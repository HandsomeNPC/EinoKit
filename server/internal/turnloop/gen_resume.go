package turnloop

import (
	"context"
	"errors"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

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
