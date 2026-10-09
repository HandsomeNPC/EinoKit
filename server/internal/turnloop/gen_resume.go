package turnloop

import (
	"context"
	"errors"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// makeGenResume 返回 GenResume 回调。由 OnAgentEvents 在 resume 完成后落库(走 internal/memory/thread)。
func makeGenResume(tc ThreadContext) func(ctx context.Context, loop *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], interruptedItems, unhandledItems, newItems []*ChatItem) (*adk.GenResumeResult[*ChatItem, *schema.AgenticMessage], error) {
	return func(ctx context.Context, loop *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], interruptedItems, unhandledItems, newItems []*ChatItem) (*adk.GenResumeResult[*ChatItem, *schema.AgenticMessage], error) {
		var interruptItem *ChatItem
		for _, item := range newItems {
			if item != nil && len(item.Resume) > 0 {
				interruptItem = item
				break
			}
		}
		if interruptItem == nil {
			return nil, errors.New("no interrupt item found for resume")
		}
		targets := make(map[string]any, len(interruptItem.Resume))
		for _, entry := range interruptItem.Resume {
			if entry.InterruptID == "" {
				continue
			}
			targets[entry.InterruptID] = entry
		}
		return &adk.GenResumeResult[*ChatItem, *schema.AgenticMessage]{
			ResumeParams: &adk.ResumeParams{
				Targets: targets,
			},
			Consumed:  interruptedItems,
			Remaining: unhandledItems,
		}, nil
	}
}
