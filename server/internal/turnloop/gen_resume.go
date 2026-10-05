package turnloop

import (
	"context"
	"errors"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// makeGenResume 返回 GenResume 回调。由 OnAgentEvents 在 resume 完成后落库(走 internal/memory/thread)。
func makeGenResume() func(ctx context.Context, loop *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], interruptedItems, unhandledItems, newItems []*ChatItem) (*adk.GenResumeResult[*ChatItem, *schema.AgenticMessage], error) {
	return func(ctx context.Context, loop *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], interruptedItems, unhandledItems, newItems []*ChatItem) (*adk.GenResumeResult[*ChatItem, *schema.AgenticMessage], error) {
		// 在 newItems 中找 InterruptContent != nil 的条目(对标 example 找 ApprovalResult != nil)。
		var interruptItem *ChatItem
		for _, item := range newItems {
			if item.InterruptContent != nil {
				interruptItem = item
				break
			}
		}
		if interruptItem == nil {
			return nil, errors.New("no interrupt item found for resume")
		}

		return &adk.GenResumeResult[*ChatItem, *schema.AgenticMessage]{
			ResumeParams: &adk.ResumeParams{
				// Targets: interruptID → 中断节点收到的反馈值,ADK 据此恢复执行。
				Targets: map[string]any{interruptItem.InterruptID: interruptItem.InterruptContent},
			},
			Consumed:  interruptedItems,
			Remaining: unhandledItems,
		}, nil
	}
}
