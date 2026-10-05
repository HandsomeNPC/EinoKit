package turnloop

import (
	"context"
	"errors"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

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
