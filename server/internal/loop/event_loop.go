package loop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	agui "github.com/HandsomeNPC/agui-go-sdk"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"

	"github.com/HandsomeNPC/EinoKit/internal/aguibridge"
	"github.com/HandsomeNPC/EinoKit/internal/repository"
)

// errInterrupted 在 Agent 因审批被中断时由 OnAgentEvents 返回，
// TurnLoop 以它作为 ExitReason 退出（并保存 checkpoint）。
var errInterrupted = errors.New("loop: agent 因审批中断")

// errSuperseded 表示本轮已被更新的请求取代。
var errSuperseded = errors.New("loop: 本轮已被更新的请求取代")

// IsSuperseded 判断错误是否为"本轮被取代"。
func IsSuperseded(err error) bool { return errors.Is(err, errSuperseded) }

// agentStartTimeout 等待 Agent 投递事件迭代器的上限。
// 抢占场景下上一轮可能要排空几十秒，所以给得比较宽。
const agentStartTimeout = 60 * time.Second

// keepaliveInterval SSE 保活注释帧的间隔。
const keepaliveInterval = 5 * time.Second

// Deps 是 loop 层运行一轮所需的全部外部依赖。
type Deps struct {
	Agent adk.TypedAgent[*schema.AgenticMessage]
	Store adk.CheckPointStore // checkpoint.Default() 可直接传入：两者是同一个接口别名
	Repo  *repository.SessionRepository
}

// RunChat 处理一条新的用户消息：为会话创建或抢占 TurnLoop，
// 把本轮事件翻译成 AG-UI 事件写进 SSE 流，返回本轮结果。
func RunChat(c *gin.Context, em *agui.Emitter, d Deps, sessionID, query string) (iterResult, error) {
	item := &ChatItem{Query: query}
	ts := getTurnState(sessionID)

	ts.mu.Lock()
	if ts.loop != nil {
		// 已有 loop：尝试抢占（在工具调用边界打断当前轮）。
		existing := ts.loop
		log.Printf("[chat] session=%s 抢占当前轮次", sessionID)
		ready, done := ts.rearmLocked()
		ts.mu.Unlock()

		if ok, _ := existing.Push(item, adk.WithPreempt[*ChatItem, *schema.AgenticMessage](adk.AfterToolCalls)); ok {
			return streamOneTurn(c, em, sessionID, ready, done, nil)
		}

		// 推不进去说明 loop 已经死了（例如上一轮出错），建新的。
		log.Printf("[chat] session=%s 旧 loop 已停止，新建 loop", sessionID)
		ts.mu.Lock()
	}

	ready, done := ts.startLoopLocked(d, sessionID, item)
	ts.mu.Unlock()
	return streamOneTurn(c, em, sessionID, ready, done, nil)
}

// RunResume 用用户的审批决定恢复被中断的运行。
//
// 它会新建一个带 checkpoint 的 TurnLoop：Store + CheckpointID 都配好时，
// TurnLoop 在 Run 时发现快照里有 runner 状态，就走 GenResume 而非 GenInput。
func RunResume(c *gin.Context, em *agui.Emitter, d Deps, sessionID string,
	pending *PendingApproval, result *ApprovalResult,
) (iterResult, error) {
	item := &ChatItem{
		ApprovalResult: result,
		InterruptID:    pending.InterruptID,
	}

	// 恢复时让本轮到达的工具结果回填到原来那张卡片上。
	var seed map[string]string
	if pending.ModelCallID != "" && pending.CallToolCallID != "" {
		seed = map[string]string{pending.ModelCallID: pending.CallToolCallID}
	}

	ts := getTurnState(sessionID)
	ts.mu.Lock()
	// 清掉任何残留的 loop：审批恢复必须从 checkpoint 起跑。
	if ts.loop != nil {
		ts.loop.Stop(adk.WithImmediate())
		ts.loop = nil
	}
	if ts.cancelLoop != nil {
		ts.cancelLoop()
		ts.cancelLoop = nil
	}
	ready, done := ts.startLoopLocked(d, sessionID, item)
	ts.mu.Unlock()

	log.Printf("[approve] session=%s interruptID=%s approved=%v",
		sessionID, pending.InterruptID, result.Approved)
	return streamOneTurn(c, em, sessionID, ready, done, seed)
}

// rearmLocked 换上一对新的桥接通道，并关闭旧的 handlerDone 通知前一个 handler 退出。
// 调用方必须持有 ts.mu。返回的是局部引用——后续等待只认它们，
// 这样并发抢占替换了 ts 上的通道也不会让本 handler 停在没人投递的旧通道上。
func (ts *sessionTurnState) rearmLocked() (chan iterEnvelope, chan struct{}) {
	if ts.handlerDone != nil {
		close(ts.handlerDone)
	}
	ts.iterReady = make(chan iterEnvelope, 1)
	ts.handlerDone = make(chan struct{})
	return ts.iterReady, ts.handlerDone
}

// startLoopLocked 新建 loop、推入首个条目并启动。调用方必须持有 ts.mu。
func (ts *sessionTurnState) startLoopLocked(d Deps, sessionID string, item *ChatItem) (chan iterEnvelope, chan struct{}) {
	ready, done := ts.rearmLocked()

	l := newLoop(d, sessionID)
	ts.loop = l

	// loop 的寿命长于 HTTP 请求，所以 context 必须来自 Background；
	// 若用 c.Request.Context()，客户端一断开就会把 loop 杀在工具调用中途。
	loopCtx, cancel := context.WithCancel(context.Background())
	ts.cancelLoop = cancel

	l.Push(item)
	l.Run(loopCtx)
	startLoopCleanup(ts, l, sessionID)
	return ready, done
}

// startLoopCleanup 等 loop 退出后把 ts.loop 置空并释放其 context，
// 这样下一次请求会建一个全新的 loop，而不是去抢占一个已经死掉的。
func startLoopCleanup(ts *sessionTurnState, l *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], sessionID string) {
	go func() {
		state := l.Wait()

		ts.mu.Lock()
		if ts.loop == l {
			ts.loop = nil
		}
		if ts.cancelLoop != nil {
			ts.cancelLoop()
			ts.cancelLoop = nil
		}
		ts.mu.Unlock()

		switch {
		case state.ExitReason == nil:
			log.Printf("[loop] session=%s 正常退出", sessionID)
		case errors.Is(state.ExitReason, errInterrupted):
			log.Printf("[loop] session=%s 因审批中断退出", sessionID)
		default:
			log.Printf("[loop] session=%s 退出: %v", sessionID, state.ExitReason)
		}
		if state.CheckpointErr != nil {
			log.Printf("[loop] session=%s checkpoint 保存失败: %v", sessionID, state.CheckpointErr)
		}
	}()
}

// streamOneTurn 等 OnAgentEvents 投递事件迭代器，消费它并把结果回传。
//
// 在等待期间开一个保活 goroutine：抢占场景下上一轮可能要排空几十秒，
// 期间什么都不写可能让浏览器/中间代理认为连接已失活。
func streamOneTurn(c *gin.Context, em *agui.Emitter, sessionID string,
	ready chan iterEnvelope, handlerDone chan struct{}, seed map[string]string,
) (iterResult, error) {
	kaStop, kaDone := startKeepalive(c)

	var envelope iterEnvelope
	select {
	case envelope = <-ready:
		stopKeepalive(kaStop, kaDone)
	case <-handlerDone:
		// 另一次抢占接管了本会话，本轮已被取代。
		stopKeepalive(kaStop, kaDone)
		log.Printf("[chat] session=%s 本轮被更新的请求取代", sessionID)
		return iterResult{}, errSuperseded
	case <-time.After(agentStartTimeout):
		stopKeepalive(kaStop, kaDone)
		return iterResult{}, fmt.Errorf("loop: agent 未在 %s 内启动", agentStartTimeout)
	}

	result := aguibridge.StreamTurn(em, seed, envelope.events)

	// 回传给给我们投递这个信封的那次 OnAgentEvents（而非 ts 上的通道）。
	envelope.done <- result

	switch {
	case result.Err != nil:
		log.Printf("[chat] session=%s 流式出错: %v", sessionID, result.Err)
	case result.Interrupted():
		log.Printf("[chat] session=%s 中断等待审批: id=%s", sessionID, result.InterruptID)
	default:
		log.Printf("[chat] session=%s 完成，回复 %d 字", sessionID, len([]rune(result.LastContent)))
	}
	return result, nil
}

// startKeepalive 起一个 goroutine 周期性写 SSE 注释帧。
//
// 只能写注释（": ...\n\n"）而不能发空事件：ag-ui 的 SSE writer 只产出
// data: <json> 帧且会校验事件，不存在"空事件"。注释行任何符合规范的解析器都会丢弃。
//
// 并且只在 emitter 可证明空闲的窗口里开：两个 goroutine 并发写 c.Writer 是数据竞争。
func startKeepalive(c *gin.Context) (stop chan struct{}, done chan struct{}) {
	stop, done = make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(keepaliveInterval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if _, err := c.Writer.WriteString(": keepalive\n\n"); err != nil {
					return
				}
				c.Writer.Flush()
			}
		}
	}()
	return stop, done
}

// stopKeepalive 停掉保活并等它真正退出。
// 这个 join 是必须的：否则 ticker 可能在 emitter 已开始写事件之后再触发一次。
func stopKeepalive(stop, done chan struct{}) {
	close(stop)
	<-done
}

// newLoop 为会话创建一个 TurnLoop。
//
// 一轮的生命周期：
//  1. loop.Run(ctx)
//  2. → GenInput(items)        把队列里的 ChatItem 变成这一轮的 Agent 输入
//     （或 GenResume，当 checkpoint 里存着被中断的 runner 状态）
//  3. → PrepareAgent(consumed)  决定这一轮用哪个 Agent
//  4. → Agent.Run(input)        拿到事件迭代器
//  5. → OnAgentEvents(...)      ★ 在这里把迭代器交给 HTTP handler，只调一次
//  6. 根据 OnAgentEvents 返回的 error 决定 ExitReason
//
// Store 与 CheckpointID 始终配置：首轮对话的中断必须落盘，之后的恢复才找得到它。
func newLoop(d Deps, sessionID string) *adk.TurnLoop[*ChatItem, *schema.AgenticMessage] {
	return adk.NewTurnLoop(adk.TurnLoopConfig[*ChatItem, *schema.AgenticMessage]{
		GenInput:      makeGenInput(d, sessionID),
		GenResume:     makeGenResume(d, sessionID),
		PrepareAgent:  makePrepareAgent(d),
		OnAgentEvents: makeOnAgentEvents(d, sessionID),
		Store:         d.Store,
		CheckpointID:  sessionID, // 一个会话一份 checkpoint
	})
}

// makeGenInput 从会话历史构建这一轮喂给 Agent 的消息。
//
// 用户消息在这里落库而不是在 handler 里：GenInput 只会在上一轮的 OnAgentEvents
// 把中间消息落完之后才触发，因此即便发生抢占，消息的入库顺序也与对话顺序一致。
func makeGenInput(d Deps, sessionID string) func(context.Context, *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], []*ChatItem) (*adk.GenInputResult[*ChatItem, *schema.AgenticMessage], error) {
	return func(ctx context.Context, l *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], items []*ChatItem) (*adk.GenInputResult[*ChatItem, *schema.AgenticMessage], error) {
		// 取第一个带 query 的条目，其余留到后续轮次。
		var consumed, remaining []*ChatItem
		var queryItem *ChatItem
		for _, item := range items {
			if queryItem == nil && item.Query != "" {
				queryItem = item
				consumed = append(consumed, item)
			} else {
				remaining = append(remaining, item)
			}
		}
		if queryItem == nil {
			// 没有可处理的消息条目，停掉 loop。
			l.Stop(adk.WithStopCause("没有待处理的消息条目"))
			return &adk.GenInputResult[*ChatItem, *schema.AgenticMessage]{
				Input:     &adk.TypedAgentInput[*schema.AgenticMessage]{Messages: []*schema.AgenticMessage{schema.UserAgenticMessage("done")}},
				Remaining: items,
			}, nil
		}

		userMsg := schema.UserAgenticMessage(queryItem.Query)
		if payload, err := marshalMsg(userMsg); err != nil {
			log.Printf("[genInput] session=%s 序列化用户消息失败: %v", sessionID, err)
		} else if err := d.Repo.AppendMessage(ctx, sessionID, payload); err != nil {
			log.Printf("[genInput] session=%s 落库用户消息失败: %v", sessionID, err)
		}

		history, err := loadHistory(ctx, d.Repo, sessionID)
		if err != nil {
			return nil, fmt.Errorf("loop: 读取会话历史失败: %w", err)
		}

		log.Printf("[genInput] session=%s query=%q 历史消息=%d", sessionID, queryItem.Query, len(history))
		return &adk.GenInputResult[*ChatItem, *schema.AgenticMessage]{
			Input: &adk.TypedAgentInput[*schema.AgenticMessage]{
				Messages:        normalizeMessagesForModelInput(history),
				EnableStreaming: true,
			},
			Consumed:  consumed,
			Remaining: remaining,
		}, nil
	}
}

// makeGenResume 用审批结果恢复被中断的那一轮。
func makeGenResume(d Deps, sessionID string) func(context.Context, *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], []*ChatItem, []*ChatItem, []*ChatItem) (*adk.GenResumeResult[*ChatItem, *schema.AgenticMessage], error) {
	return func(ctx context.Context, _ *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], interruptedItems, unhandledItems, newItems []*ChatItem) (*adk.GenResumeResult[*ChatItem, *schema.AgenticMessage], error) {
		for _, item := range newItems {
			if item.ApprovalResult != nil {
				return &adk.GenResumeResult[*ChatItem, *schema.AgenticMessage]{
					ResumeParams: &adk.ResumeParams{
						Targets: map[string]any{item.InterruptID: item.ApprovalResult},
					},
					Consumed:  interruptedItems,
					Remaining: unhandledItems,
				}, nil
			}
		}

		// 没有审批条目：用户无视审批卡片直接发了新消息。
		// 此时不能报错——报错会让会话永久卡在 resume 模式。自动把原审批当作取消，
		// 新消息留到下一轮由 GenInput 处理。
		sess, err := d.Repo.SelectByID(ctx, sessionID)
		if err != nil || sess == nil || sess.PendingInterruptID == "" {
			return nil, errors.New("loop: 没有可恢复的审批")
		}
		reason := "用户发送了新消息，本次审批视为取消"
		log.Printf("[genResume] session=%s 无审批条目，自动取消中断 %s", sessionID, sess.PendingInterruptID)
		return &adk.GenResumeResult[*ChatItem, *schema.AgenticMessage]{
			ResumeParams: &adk.ResumeParams{
				Targets: map[string]any{
					sess.PendingInterruptID: &ApprovalResult{Approved: false, DisapproveReason: &reason},
				},
			},
			Consumed:  interruptedItems,
			Remaining: append(unhandledItems, newItems...),
		}, nil
	}
}

// makePrepareAgent 每轮都返回同一个 Agent。
func makePrepareAgent(d Deps) func(context.Context, *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], []*ChatItem) (adk.TypedAgent[*schema.AgenticMessage], error) {
	return func(context.Context, *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], []*ChatItem) (adk.TypedAgent[*schema.AgenticMessage], error) {
		return d.Agent, nil
	}
}

// makeOnAgentEvents 是 TurnLoop 与 HTTP handler 之间的桥。
//
// 它把事件迭代器递给 handler，等 handler 消费完，然后负责落库与中断状态维护。
func makeOnAgentEvents(d Deps, sessionID string) func(context.Context, *adk.TurnContext[*ChatItem, *schema.AgenticMessage], *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) error {
	return func(ctx context.Context, _ *adk.TurnContext[*ChatItem, *schema.AgenticMessage], evs *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) error {
		ts := getTurnState(sessionID)

		// 在锁内快照通道，避免与正在为抢占重建通道的 handler 竞态。
		ts.mu.Lock()
		ready := ts.iterReady
		ts.mu.Unlock()
		if ready == nil {
			return errors.New("loop: 没有等待中的 handler")
		}

		// done 由本次调用自己创建并随信封传出，确保 handler 回复的是本次调用。
		done := make(chan iterResult, 1)
		select {
		case ready <- iterEnvelope{events: evs, done: done}:
		case <-ctx.Done():
			return ctx.Err()
		}

		// 等 handler 排空。同时 select ctx.Done()：抢占取代了 handler 时，
		// 旧 handler 会经 handlerDone 退出，没人会往 done 里写。
		var result iterResult
		select {
		case result = <-done:
		case <-ctx.Done():
			return ctx.Err()
		}

		// 落库本轮产生的全部消息（assistant 文本/工具调用、工具结果）。
		for _, msg := range result.Intermediates {
			payload, err := marshalMsg(msg)
			if err != nil {
				log.Printf("[onEvents] session=%s 序列化消息失败: %v", sessionID, err)
				continue
			}
			if err := d.Repo.AppendMessage(ctx, sessionID, payload); err != nil {
				log.Printf("[onEvents] session=%s 落库消息失败: %v", sessionID, err)
			}
		}

		if result.Interrupted() {
			pending := PendingApproval{
				InterruptID:        result.InterruptID,
				ApprovalToolCallID: result.ApprovalToolCallID,
				CallToolCallID:     result.CallToolCallID,
				ModelCallID:        result.ModelCallID,
				ToolName:           result.ToolName,
				Arguments:          result.Arguments,
			}
			blob, err := json.Marshal(pending)
			if err != nil {
				return fmt.Errorf("loop: 序列化待审批上下文失败: %w", err)
			}
			if err := d.Repo.UpdatePendingApproval(ctx, sessionID, result.InterruptID, string(blob)); err != nil {
				return fmt.Errorf("loop: 保存待审批状态失败: %w", err)
			}
			return errInterrupted
		}

		if err := d.Repo.ClearPendingApproval(ctx, sessionID); err != nil {
			log.Printf("[onEvents] session=%s 清空待审批状态失败: %v", sessionID, err)
		}
		return result.Err
	}
}

// StopSession 立即停掉某个会话正在运行的 TurnLoop。
// 没有进行中的轮次时返回 false。
func StopSession(sessionID string) bool {
	ts := getTurnState(sessionID)

	ts.mu.Lock()
	l := ts.loop
	ts.loop = nil
	cancel := ts.cancelLoop
	ts.cancelLoop = nil
	ts.mu.Unlock()

	if l == nil {
		if cancel != nil {
			cancel()
		}
		return false
	}

	log.Printf("[abort] session=%s 立即停止 loop", sessionID)
	l.Stop(adk.WithImmediate())
	l.Wait()
	if cancel != nil {
		cancel()
	}
	log.Printf("[abort] session=%s loop 已停止", sessionID)
	return true
}

// DropSession 停掉 loop 并移除会话的内存状态，供删除会话时调用。
func DropSession(sessionID string) {
	StopSession(sessionID)
	deleteTurnState(sessionID)
}
