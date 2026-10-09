package turnloop

import (
	"context"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// makeOnAgentEvents 返回 OnAgentEvents 回调。
func makeOnAgentEvents(threadCtx ThreadContext) func(ctx context.Context, tc *adk.TurnContext[*ChatItem, *schema.AgenticMessage], evIter *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) error {
	return func(ctx context.Context, tc *adk.TurnContext[*ChatItem, *schema.AgenticMessage], evIter *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) error {
		threadID := threadCtx.ThreadID
		runID := threadCtx.Emitter.RunID
		emitter := threadCtx.Emitter

		//消费消息
		for {
			event, ok := evIter.Next()
			if !ok {
				break
			}
			//TODO 消费消息
		}
	}
	return nil
}
