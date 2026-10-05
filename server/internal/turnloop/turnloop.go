package turnloop

import (
	"context"
	"errors"

	"github.com/HandsomeNPC/EinoKit/internal/checkpoint"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// *schema.AgenticMessage 是本包固定的消息类型，即 EinoKit 全局使用的 agentic 消息。

// NewTurnLoop 装配一个 TurnLoop。sessionID 同时作为 checkpoint key，
func NewTurnLoop(sessionID string) *adk.TurnLoop[*ChatItem, *schema.AgenticMessage] {
	cfg := adk.TurnLoopConfig[*ChatItem, *schema.AgenticMessage]{
		GenInput:      makeGenInput(sessionID), //每轮开始时调用，决定"这一轮 Agent 看到什么"
		PrepareAgent:  makePrepareAgent(),      //每轮调用一次，返回本轮使用的 Agent
		OnAgentEvents: makeOnAgentEvents(),     //接收Agent的事件流，负责渲染输出和持久化中间消息
		//以下三个字段用于声明式 checkpoint（审批恢复）
		GenResume:    makeGenResume(),
		Store:        checkpoint.GetInstance(),
		CheckpointID: sessionID,
	}
	return adk.NewTurnLoop(cfg)
}

// GenInput：每轮开始时调用，决定"这一轮 Agent 看到什么"
// 从队列中选择 items 构建 Agent 输入，返回 Consumed（本轮处理）和 Remaining（留到后续轮次）
func makeGenInput(sessionID string) func(ctx context.Context, loop *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], items []*ChatItem) (*adk.GenInputResult[*ChatItem, *schema.AgenticMessage], error) {
	return func(ctx context.Context, loop *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], items []*ChatItem) (*adk.GenInputResult[*ChatItem, *schema.AgenticMessage], error) {
		// TODO: 从 items 选 queryItem，持久化用户消息，构建 runMessages，
		//  返回 Input/Consumed/Remaining。无 query 时 loop.Stop(WithStopCause("no query items"))。
		_ = sessionID
		_ = items
		return nil, errors.New("genInput: 未实现")
	}
}

// PrepareAgent：每轮调用一次，返回本轮使用的 Agent
// 本示例直接返回同一个 Agent，但你可以根据 items 动态选择不同 Agent
func makePrepareAgent() func(ctx context.Context, loop *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], consumed []*ChatItem) (adk.TypedAgent[*schema.AgenticMessage], error) {
	return func(ctx context.Context, loop *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], consumed []*ChatItem) (adk.TypedAgent[*schema.AgenticMessage], error) {
		// TODO: 返回注入的 Agent。
		_ = consumed
		return nil, errors.New("prepareAgent: 未实现")
	}
}

// OnAgentEvents：接收 Agent 的事件流，负责渲染输出和持久化中间消息
// 本示例通过 channel 把事件流转交给 HTTP handler 做 SSE 输出
func makeOnAgentEvents() func(ctx context.Context, tc *adk.TurnContext[*ChatItem, *schema.AgenticMessage], events *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) error {
	return func(ctx context.Context, tc *adk.TurnContext[*ChatItem, *schema.AgenticMessage], events *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) error {
		// TODO: 调用注入的 stream 消费 events，持久化中间消息，
		//  触发 interrupt 时写回 pending interrupt 与 msgIdx，返回 result.Err。
		_ = events
		return errors.New("onAgentEvents: 未实现")
	}
}

// GenResume 的职责就是从新 Push 进来的 items 中提取审批结果，构建 ResumeParams：
func makeGenResume() func(ctx context.Context, loop *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], interruptedItems, unhandledItems, newItems []*ChatItem) (*adk.GenResumeResult[*ChatItem, *schema.AgenticMessage], error) {
	return func(ctx context.Context, loop *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], interruptedItems, unhandledItems, newItems []*ChatItem) (*adk.GenResumeResult[*ChatItem, *schema.AgenticMessage], error) {
		// TODO: 在 newItems 中找 InterruptContent != nil 的条目，
		//  从 pending interrupt 取 ID，组装 ResumeParams.Targets。
		_ = interruptedItems
		_ = unhandledItems
		_ = newItems
		return nil, errors.New("genResume: 未实现")
	}
}
