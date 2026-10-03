package loop

import (
	"sync"

	"github.com/cloudwego/eino/adk"
)

// turnStates 按 sessionID 维护每个会话的 sessionTurnState。
var turnStates = sync.Map{}

type sessionTurnState[M adk.MessageType] struct {
	mu          sync.Mutex                  //保证并发安全
	loop        *adk.TurnLoop[*ChatItem, M] //该会话当前的TurnLoop，nil表示没有进行中的轮次
	iterReady   chan iterEnvelope[M]        // OnAgentEvents → HTTP handler,Agent产出事件的时候,onAgentEvents把事件迭代器+history打包成iterEnvelop发到这里
	handlerDone chan struct{}               //通过close(ts.handlerDone)关闭消息
}

// ChatItem 用户消息
type ChatItem struct {
	Query          string          // 用户消息文本(审批条目时为空)
	ApprovalResult *ApprovalResult // 当本条目携带审批决定时非 nil
	InterruptID    string          // 本审批所解决的 interrupt 的 ID
}

type ApprovalResult struct {
	Approved         bool
	DisapproveReason *string
}

type iterEnvelope[M adk.MessageType] struct {
	events  *adk.AsyncIterator[*adk.TypedAgentEvent[M]] //Agent生的事件
	history []M                                         //本轮之前产生的消息
	done    chan iterResult[M]
}

// iterResult 把结果从 HTTP handler 回传给 OnAgentEvents。
type iterResult[M adk.MessageType] struct {
	lastContent   string //这一轮 Agent 最后一条 assistant 文本回复的内容
	intermediates []M    // 待持久化的消息(Assistant消息以及工具调用消息)
	interruptID   string // 这一轮因审批被中断,值是触发中断的 interrupt 上下文 ID
	msgIdx        int    //A2UI 组件树里"这一轮已经写到第几个 msg 卡片"的游标。中断时它告诉 OnAgentEvents已经落库到哪了
	err           error
}

// getTurnState 根据SessionID拿取对应的 turn state。
func getTurnState[M adk.MessageType](sessionID string) *sessionTurnState[M] {
	val, _ := turnStates.LoadOrStore(sessionID, &sessionTurnState[M]{})
	return val.(*sessionTurnState[M])
}

// deleteTurnState 移除 sessionID 对应的 turn state。
func deleteTurnState(sessionID string) {
	turnStates.Delete(sessionID)
}
