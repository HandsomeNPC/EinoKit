package turnloop

import (
	"context"
	"errors"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/HandsomeNPC/EinoKit/internal/agui"
)

// makeOnAgentEvents：接收 Agent 的事件流，负责渲染输出和持久化中间消息。
// emitter 由 NewTurnLoop 创建并注入，绑定本次 HTTP 请求的 gin.Context，
// 在此处把 Agent 事件流转成 AG-UI SSE 写回客户端。
func makeOnAgentEvents(emitter *agui.Emitter) func(ctx context.Context, tc *adk.TurnContext[*ChatItem, *schema.AgenticMessage], events *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) error {
	return func(ctx context.Context, tc *adk.TurnContext[*ChatItem, *schema.AgenticMessage], events *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) error {
		// TODO: 遍历 events,用 emitter 把 Agent 事件流转成 AG-UI SSE
		//  (RunStarted/TextMessageStart/Content/End/ToolCallStart/Args/End/
		//   ToolCallResult/RunFinished 等),并持久化中间消息;
		//  触发 interrupt 时写回 pending interrupt 与 msgIdx,返回 result.Err。
		_ = emitter
		_ = events
		return errors.New("onAgentEvents: 未实现")
	}
}
