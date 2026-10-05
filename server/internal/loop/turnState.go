package loop

import (
	"context"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// init 把进 checkpoint 接口字段的类型显式注册给 gob。
//
// TurnLoop 落快照时用 gob 编码，ChatItem 作为条目类型 T 会随 buffer 进快照。
// 漏注册的症状是 "gob: type not registered for interface"，且只在解码时才炸。
// 这里注册 ChatItem，使快照在编码/解码两端都稳定可用。
func init() {
	schema.RegisterName[ChatItem]("einokit_chat_item")
}

// turnStateHolder 按 threadID 维护每个会话的 threadTurnState。
var turnStateHolder = sync.Map{}

// threadTurnState 为一个会话持有 TurnLoop 与事件桥接通道。
// loop的寿命长于单个 HTTP 请求: loop.Run 拿的是 background 派生的 context，
// handler 只是某一轮事件迭代器的临时消费者。
type threadTurnState struct {
	mu   sync.Mutex                                       // 保证并发安全
	loop *adk.TurnLoop[*ChatItem, *schema.AgenticMessage] // 该会话当前的 TurnLoop，nil 表示没有进行中的轮次
	// cancelLoop 取消 loop 自己的 context。handler 超时放弃后，OnAgentEvents 可能
	// 停在 <-done 上无人读，只有 ctx.Done() 能救它，否则 goroutine 与快照双泄漏。
	cancelLoop  context.CancelFunc
	iterReady   chan iterEnvelope // OnAgentEvents → handler：Agent 产出事件时把迭代器打包发到这里
	handlerDone chan struct{}     // 关闭以通知前一个 handler 在被抢占时退出
	// Preempt = 抢占：用户在 Agent 还没答完当前轮时又发新消息，系统打断当前轮次优先处理新消息。
}

// ChatItem 是 TurnLoop 的条目类型：用户每一次往对话里塞东西都打包成一个 ChatItem。
type ChatItem struct {
	Query string // 用户消息文本
}

// iterEnvelope 把事件迭代器从 OnAgentEvents 传给 HTTP handler。
type iterEnvelope struct {
	events *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]] // Agent 产出的事件
	done   chan iterResult                                                  // handler → OnAgentEvents：本轮的消费结果
}

// iterResult 把一轮的消费结果从 HTTP handler 回传给 OnAgentEvents。
// 与 TurnResult 同一类型，只是 handler/OnAgentEvents 内部用一个短名。
type iterResult = TurnResult

// TurnResult 是一轮事件消费完后交回给 loop 层的结果。
//
// 参考实现里的 msgIdx 字段在 AG-UI 下无意义（那是 A2UI 的组件槽位游标），未引入。
type TurnResult struct {
	LastContent   string                   // 本轮最后一段 assistant 正文（日志用）
	Intermediates []*schema.AgenticMessage // 待落库的消息（assistant 文本/工具调用、工具结果）
	Err           error                    // 事件流自身的错误
}

// getTurnState 根据 threadID 拿取对应的 turn state，不存在则创建。
func getTurnState(threadID string) *threadTurnState {
	val, _ := turnStateHolder.LoadOrStore(threadID, &threadTurnState{})
	return val.(*threadTurnState)
}

// deleteTurnState 移除 threadID 对应的 turn state。
func deleteTurnState(threadID string) {
	turnStateHolder.Delete(threadID)
}
