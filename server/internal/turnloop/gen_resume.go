package turnloop

import (
	"context"
	"errors"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// makeGenResume 返回 GenResume 回调。当 loop 因 interrupt 退出后,下次带着
// 新条目(中断反馈)重启,TurnLoop 走的不是 GenInput 而是 GenResume。
//
// 与 eino-examples 的 makeGenResume 一致:在 newItems 中找带中断反馈的条目,
// 用其 InterruptID + InterruptContent 组装 ResumeParams.Targets,把控制权
// 交还给中断节点。差异在于 EinoKit 的 ChatItem 直接携带中断状态
// (InterruptID + InterruptContent),无需从 session 的 pendingInterruptID 取。
//
// 本回调不持久化:中断反馈通过 ResumeParams.Targets 直接交给 ADK 的中断节点,
// 不作为消息进历史——若在其中插入 user 消息,会打断 tool_call → tool_result
// 的相邻关系,导致下一轮 Responses API 报消息顺序错误。tool_result 及后续
// assistant 文本由 OnAgentEvents 在 resume 完成后落库(走 internal/memory/thread)。
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
