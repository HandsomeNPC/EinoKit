package loop

import (
	"context"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/HandsomeNPC/EinoKit/internal/aguibridge"
)

// turnStateHolder 按 sessionID 维护每个会话的 sessionTurnState。
var turnStateHolder = sync.Map{}

// sessionTurnState 为一个会话持有 TurnLoop 与事件桥接通道。
//
// 注意 loop 的寿命长于单个 HTTP 请求：loop.Run 拿的是 background 派生的 context，
// handler 只是某一轮事件迭代器的临时消费者。
type sessionTurnState struct {
	mu   sync.Mutex                                       // 保证并发安全
	loop *adk.TurnLoop[*ChatItem, *schema.AgenticMessage] // 该会话当前的 TurnLoop，nil 表示没有进行中的轮次
	// cancelLoop 取消 loop 自己的 context。handler 超时放弃后，OnAgentEvents 可能
	// 停在 <-done 上无人读，只有 ctx.Done() 能救它，否则 goroutine 与快照双泄漏。
	cancelLoop  context.CancelFunc
	iterReady   chan iterEnvelope // OnAgentEvents → handler：Agent 产出事件时把迭代器打包发到这里
	handlerDone chan struct{}     // 关闭以通知前一个 handler 在被抢占时退出
	// Preempt = 抢占：用户在 Agent 还没答完当前轮时又发新消息，系统打断当前轮次优先处理新消息。
}

// ChatItem 是 TurnLoop 的条目类型：用户每一次"往对话里塞东西"——无论是发新消息，
// 还是对一个待审批的工具调用做决定——都打包成一个 ChatItem 推进循环。
//
// 两种条目靠字段是否为空区分：
//   - 普通消息条目：Query != ""，ApprovalResult == nil
//   - 审批条目：Query == ""，ApprovalResult != nil，InterruptID 指明要解除哪个中断
type ChatItem struct {
	Query          string          // 用户消息文本(审批条目时为空)
	ApprovalResult *ApprovalResult // 当本条目携带审批决定时非 nil
	InterruptID    string          // 本审批所解决的 interrupt 的 ID
}

// ApprovalResult 是用户对一个被中断等待审批的工具调用给出的结论。
type ApprovalResult struct {
	Approved         bool    // true 批准执行，false 拒绝
	DisapproveReason *string // 拒绝原因，批准时为 nil
}

// iterEnvelope 把事件迭代器从 OnAgentEvents 传给 HTTP handler。
//
// done 通道随信封一起传递，而不是挂在 sessionTurnState 上：这样 handler 回复的
// 永远是"给自己投递信封的那次 OnAgentEvents 调用"，即便并发抢占已经把会话级
// 通道换掉也不会错投。
//
// 这里有意偏离参考实现（它在 sessionTurnState 上还有一个 iterDone 字段）：
// 那个字段由 handler 写、仅被 OnAgentEvents 读，是纯交接槽；既然信封由
// OnAgentEvents 构造，它自己 make 这个通道即可，字段可以消失。
//
// 也没有 history 字段：AG-UI 是追加式事件协议，客户端自己持有 transcript，
// 不需要回放历史来重建组件树（那是 A2UI 的需求）。
type iterEnvelope struct {
	events *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]] // Agent 产出的事件
	done   chan iterResult                                                  // handler → OnAgentEvents：本轮的消费结果
}

// iterResult 把一轮的消费结果从 HTTP handler 回传给 OnAgentEvents。
//
// 直接复用桥接层的 TurnResult：它已经携带待落库消息、中断 ID 与审批卡片 ID。
// 参考实现里的 msgIdx 字段在 AG-UI 下无意义（那是 A2UI 的组件槽位游标），已去掉。
type iterResult = aguibridge.TurnResult

// TurnResult 是一轮对话的结果，供 handler 判断该发什么终态事件。
// 与 iterResult 同一类型，只是对外导出一个名字，免得上层去 import 桥接层。
type TurnResult = aguibridge.TurnResult

// getTurnState 根据 sessionID 拿取对应的 turn state，不存在则创建。
func getTurnState(sessionID string) *sessionTurnState {
	val, _ := turnStateHolder.LoadOrStore(sessionID, &sessionTurnState{})
	return val.(*sessionTurnState)
}

// deleteTurnState 移除 sessionID 对应的 turn state。
func deleteTurnState(sessionID string) {
	turnStateHolder.Delete(sessionID)
}
