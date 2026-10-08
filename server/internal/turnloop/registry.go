package turnloop

import (
	"errors"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// Loop 是本包对外暴露的 TurnLoop 具体类型别名，省掉调用方写一长串泛型实参。
type Loop = adk.TurnLoop[*ChatItem, *schema.AgenticMessage]

// running 记录每个会话当前正在跑的 loop，供 POST /threads/:id/abort 终止。
// 一个会话同一时刻只应有一个活跃 run：后开的 run 覆盖登记，Deregister 用
// 身份比对避免把后来者误删（ABA 问题）。
var (
	runningMu sync.Mutex
	running   = map[string]*Loop{}
)

// ErrAlreadyRunning 表示该会话已有活跃 run，不允许并发。
var ErrAlreadyRunning = errors.New("turnloop: 该会话正在处理上一条消息")

// TryRegister 原子地"检查无活跃 run 并登记"。已有活跃 run 时返回 ErrAlreadyRunning。
//
// 必须是单个临界区：先 IsRunning 再 Register 的两步写法存在竞态——两个并发
// 请求可能都通过检查，然后共用同一个 CheckpointID 互相覆盖快照。
// 返回的 deregister 须在 run 结束时调用（defer）。
func TryRegister(threadID string, loop *Loop) (deregister func(), err error) {
	runningMu.Lock()
	if running[threadID] != nil {
		runningMu.Unlock()
		return nil, ErrAlreadyRunning
	}
	running[threadID] = loop
	runningMu.Unlock()

	return func() {
		runningMu.Lock()
		if running[threadID] == loop {
			delete(running, threadID)
		}
		runningMu.Unlock()
	}, nil
}

// Abort 终止会话正在跑的 loop，没有活跃 run 时返回 false。
//
// 用 WithImmediate 立即打断模型/工具调用，WithSkipCheckpoint 跳过快照保存——
// 用户主动终止意味着不打算恢复这一轮，留下 runner 快照会让下一次 /chat 误入
// resume 分支。跳过保存时 TurnLoop 还会顺手删掉本次加载的旧快照，
// 这正是终止后期望的干净状态（见 eino adk/turn_loop.go cleanup）。
func Abort(threadID string) bool {
	runningMu.Lock()
	loop := running[threadID]
	runningMu.Unlock()

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

// IsRunning 报告会话当前是否有活跃 run，供 /render 回报状态。
// 不要用它做并发准入判断——"检查后登记"有竞态，用 TryRegister。
func IsRunning(threadID string) bool {
	runningMu.Lock()
	defer runningMu.Unlock()
	return running[threadID] != nil
}
