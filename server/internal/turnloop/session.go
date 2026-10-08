package turnloop

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/HandsomeNPC/EinoKit/internal/agui"
)

// Loop 是本包对外暴露的 TurnLoop 具体类型别名，省掉调用方写一长串泛型实参。
type Loop = adk.TurnLoop[*ChatItem, *schema.AgenticMessage]

// idleTimeout 常驻 loop 的空闲回收时限。超过这段时间没有新输入，
// loop 自行退出并被 session 丢弃，下次请求再建新的。
//
// 靠 adk.UntilIdleFor 实现：它只在轮次之间生效，不会打断正在跑的 agent，
// 每次新条目入队都会重置计时。
const idleTimeout = 10 * time.Minute

// preemptTimeout 抢占的升级时限。超过这段时间 agent 还没走到安全点
// （模型返回后 / 工具调用全部结束后），就升级为立即取消。
const preemptTimeout = 15 * time.Second

// turnSeq 给每个 Turn 发号，turnOf 据此在多个候选里挑出最新的那个。
var turnSeq atomic.Uint64

// Turn 代表一次 HTTP 对话请求。
//
// 它与 TurnLoop 的「轮」不是一一对应：一次请求可能触发两轮
// （如待审批时直接提问 —— 先取消审批，再跑新 prompt），
// 所以用 pending 计数，归零才算这次请求结束。
type Turn struct {
	seq      uint64
	runID    string
	emitter  *agui.Emitter
	done     chan struct{}
	doneOnce sync.Once

	mu sync.Mutex
	// pending 是本次请求还有几个条目没被 loop 处理完。
	pending int
	// preempted 表示本轮被后来的请求抢占，收尾时不该再发 RUN_FINISHED。
	preempted bool
}

func newTurn(runID string, emitter *agui.Emitter) *Turn {
	return &Turn{
		seq:     turnSeq.Add(1),
		runID:   runID,
		emitter: emitter,
		done:    make(chan struct{}),
	}
}

// RunID 返回本次请求的 run ID，消息落库按它分组。
func (t *Turn) RunID() string { return t.runID }

// Emitter 返回本次请求的 SSE 发射器。
func (t *Turn) Emitter() *agui.Emitter { return t.emitter }

// Done 在本次请求的全部轮次处理完后关闭，handler 据此结束 SSE。
func (t *Turn) Done() <-chan struct{} { return t.done }

// Preempted 报告本轮是否已被后来的请求抢占。
func (t *Turn) Preempted() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.preempted
}

// markPreempted 标记被抢占，并立刻放行等待中的 handler ——
// 本轮的后续输出归新请求的 SSE 流，旧连接不必再等。
func (t *Turn) markPreempted() {
	t.mu.Lock()
	t.preempted = true
	t.mu.Unlock()
	t.finish()
}

// addPending 登记本次请求又多了一个待处理条目。
func (t *Turn) addPending() {
	t.mu.Lock()
	t.pending++
	t.mu.Unlock()
}

// completeOne 标记一个条目处理完毕，并报告这是否是最后一个。
// 最后一个条目完成时关闭 done，handler 随之返回。
func (t *Turn) completeOne() (last bool) {
	t.mu.Lock()
	t.pending--
	last = t.pending <= 0
	t.mu.Unlock()
	if last {
		t.finish()
	}
	return last
}

func (t *Turn) finish() {
	t.doneOnce.Do(func() { close(t.done) })
}

// turnOf 从 loop 交给回调的条目里取出所属的请求。
//
// 取 seq 最大的那个：resume 轮的 Consumed 既包含上一轮遗留的中断条目
// （携带已结束的旧请求），也包含本次请求新压入的恢复条目，要的是后者。
func turnOf(items []*ChatItem) *Turn {
	var pick *Turn
	for _, it := range items {
		if it == nil || it.turn == nil {
			continue
		}
		if pick == nil || it.turn.seq > pick.seq {
			pick = it.turn
		}
	}
	return pick
}

// session 是一个会话的常驻 loop。
//
// 不再「一次请求一个 loop」：那样第二个请求只能被拒（409），用不上框架的
// 抢占能力。改成 loop 常驻、请求只是往里压条目后，新消息打断旧回答就是
// 框架原生的 Push + WithPreempt。
//
// loop 并非永生 —— 业务中断（等待审批）和错误都会让它退出以落快照，
// 空闲超时也会回收。loop 不在时下一次请求会新建一个，若存在 checkpoint
// 则自动走 resume 路径。
type session struct {
	threadID string

	mu   sync.Mutex
	loop *Loop
	// gen 随每次新建 loop 递增，用于让退出回调只清理自己那一代，
	// 避免误删已经换上的新 loop（ABA）。
	gen uint64
}

var (
	sessionsMu sync.Mutex
	sessions   = map[string]*session{}
)

// getSession 取出（或创建）会话的常驻 session。
func getSession(threadID string) *session {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	s := sessions[threadID]
	if s == nil {
		s = &session{threadID: threadID}
		sessions[threadID] = s
	}
	return s
}

// Submit 把本次请求的条目压入会话的常驻 loop，并返回代表这次请求的 Turn。
//
// items 按序入队。loop 正在跑别的轮次时第一个条目带抢占：先在安全点取消
// 当前 agent，再开新一轮 —— 新一轮的 GenInput 能看到包括新条目在内的
// 全部缓冲条目，所以上下文不丢。
//
// 调用方拿到 Turn 后等待 Turn.Done()，其间 OnAgentEvents 会把事件写进
// turn.Emitter()。
func Submit(threadID, runID string, emitter *agui.Emitter, items ...*ChatItem) (*Turn, error) {
	if len(items) == 0 {
		return nil, nil
	}
	turn := newTurn(runID, emitter)
	for _, it := range items {
		it.turn = turn
		turn.addPending()
	}
	if err := getSession(threadID).submit(items); err != nil {
		return nil, err
	}
	return turn, nil
}

func (s *session) submit(items []*ChatItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 最多重试一次：Push 失败只可能是撞上 loop 正在退出，
	// 换一个新 loop 必定能收下。
	for attempt := 0; attempt < 2; attempt++ {
		loop, fresh := s.ensureLoopLocked()

		// 新建的 loop 还没在跑任何轮次，不需要抢占；
		// 复用已有 loop 时第一个条目负责打断当前轮。
		ok := true
		for i, it := range items {
			var opts []adk.PushOption[*ChatItem, *schema.AgenticMessage]
			if i == 0 && !fresh {
				opts = append(opts, adk.WithPreemptTimeout[*ChatItem, *schema.AgenticMessage](
					adk.AnySafePoint, preemptTimeout))
			}
			if pushed, _ := loop.Push(it, opts...); !pushed {
				ok = false
				break
			}
		}
		if ok {
			if fresh {
				s.startLocked(loop)
			}
			return nil
		}

		// loop 已停，丢弃后重来。
		s.dropLocked(loop)
	}
	return ErrSubmitFailed
}

// ensureLoopLocked 返回可用的 loop，必要时新建。fresh 表示这是新建的、
// 还未 Run 的 loop，调用方压完条目后须调 startLocked。
func (s *session) ensureLoopLocked() (loop *Loop, fresh bool) {
	if s.loop != nil {
		return s.loop, false
	}
	s.gen++
	s.loop = NewTurnLoop(s.threadID)
	// 空闲回收：只在轮次之间生效，不会打断正在跑的 agent。
	s.loop.Stop(adk.UntilIdleFor(idleTimeout), adk.WithStopCause("idle"))
	return s.loop, true
}

// startLocked 启动 loop 并挂上退出回收。
func (s *session) startLocked(loop *Loop) {
	gen := s.gen
	// 用脱离请求的 context：loop 要跨多次 HTTP 请求存活，
	// 不能被某一次请求的取消带走。
	loop.Run(context.Background())
	go func() {
		state := loop.Wait()
		logExit(s.threadID, state)
		s.mu.Lock()
		if s.gen == gen {
			s.loop = nil
		}
		s.mu.Unlock()
		// loop 带着未处理完的条目退出时，对应请求不能一直挂着。
		releaseTurns(state)
	}()
}

// dropLocked 丢弃一个已停的 loop，使下次请求新建。
func (s *session) dropLocked(loop *Loop) {
	if s.loop == loop {
		s.loop = nil
	}
}

// releaseTurns 放行那些条目还没被处理就随 loop 退出的请求，
// 否则 handler 会一直等在 Turn.Done() 上。
func releaseTurns(state *adk.TurnLoopExitState[*ChatItem, *schema.AgenticMessage]) {
	if state == nil {
		return
	}
	groups := [][]*ChatItem{state.UnhandledItems, state.InterruptedItems}
	if state.TakeLateItems != nil {
		groups = append(groups, state.TakeLateItems())
	}
	for _, items := range groups {
		for _, it := range items {
			if it != nil && it.turn != nil {
				it.turn.completeOne()
			}
		}
	}
}

// Abort 终止会话正在跑的 loop，没有活跃 loop 时返回 false。
//
// 用 WithImmediate 立即打断模型/工具调用，WithSkipCheckpoint 跳过快照保存——
// 用户主动终止意味着不打算恢复这一轮，留下 runner 快照会让下一次 /chat 误入
// resume 分支。跳过保存时 TurnLoop 还会顺手删掉本次加载的旧快照，
// 这正是终止后期望的干净状态（见 eino adk/turn_loop.go cleanup）。
func Abort(threadID string) bool {
	sessionsMu.Lock()
	s := sessions[threadID]
	sessionsMu.Unlock()
	if s == nil {
		return false
	}

	s.mu.Lock()
	loop := s.loop
	s.mu.Unlock()
	if loop == nil {
		return false
	}

	loop.Stop(
		adk.WithImmediate(),
		adk.WithSkipCheckpoint(),
		adk.WithStopCause("aborted by user"),
	)
	// 快照跟着 checkpoint 一起作废：WithSkipCheckpoint 会连带删掉 runner 快照，
	// 中断详情若留着就会让前端显示一张永远无法恢复的审批卡片。
	clearPendingInterrupt(threadID)
	return true
}

// IsRunning 报告会话当前是否有活跃 loop，供 /render 回报状态。
func IsRunning(threadID string) bool {
	sessionsMu.Lock()
	s := sessions[threadID]
	sessionsMu.Unlock()
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loop != nil
}

// Forget 丢掉会话的常驻 loop 与登记，用于删除会话后不留悬挂对象。
func Forget(threadID string) {
	sessionsMu.Lock()
	s := sessions[threadID]
	delete(sessions, threadID)
	sessionsMu.Unlock()
	if s == nil {
		return
	}
	s.mu.Lock()
	loop := s.loop
	s.loop = nil
	s.mu.Unlock()
	if loop != nil {
		loop.Stop(adk.WithImmediate(), adk.WithSkipCheckpoint(), adk.WithStopCause("thread deleted"))
	}
}

// logExit 把 loop 退出原因落日志。中断退出是预期路径，不记为错误。
func logExit(threadID string, state *adk.TurnLoopExitState[*ChatItem, *schema.AgenticMessage]) {
	if state == nil {
		return
	}
	if state.CheckpointErr != nil {
		log.Printf("[turnloop] 保存 checkpoint 失败 thread=%s: %v", threadID, state.CheckpointErr)
	}
	if state.ExitReason != nil {
		log.Printf("[turnloop] loop 退出 thread=%s reason=%v cause=%q interrupted=%d unhandled=%d",
			threadID, state.ExitReason, state.StopCause,
			len(state.InterruptedItems), len(state.UnhandledItems))
		return
	}
	log.Printf("[turnloop] loop 正常退出 thread=%s cause=%q", threadID, state.StopCause)
}
