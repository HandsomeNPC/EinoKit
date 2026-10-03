package agent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/HandsomeNPC/EinoKit/internal/loop"
)

// approvalMiddleware 在 order_milk_tea 真正执行前插入一道人工审批闸门：
// 首次调用抛 interrupt 暂停 Agent，拿到审批结果后再决定执行还是回拒绝文案。
//
// 依赖 TurnLoop 配了 CheckPointStore——中断状态要存得下来才恢复得回去。
type approvalMiddleware struct {
	*adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
}

func newApprovalMiddleware() adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage] {
	return &approvalMiddleware{
		TypedBaseChatModelAgentMiddleware: &adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]{},
	}
}

// WrapInvokableToolCall 只拦 order_milk_tea，其余工具原样放行。
func (m *approvalMiddleware) WrapInvokableToolCall(
	_ context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	if tCtx.Name != ToolNameOrderMilkTea {
		return endpoint, nil
	}
	return func(ctx context.Context, args string, opts ...tool.Option) (string, error) {
		// 第一次进来：还没中断过，抛 interrupt 并把参数存进中断状态。
		wasInterrupted, _, stored := tool.GetInterruptState[string](ctx)
		if !wasInterrupted {
			return "", tool.StatefulInterrupt(ctx, &loop.ApprovalInfo{
				ToolName:        tCtx.Name,
				ArgumentsInJSON: args,
			}, args)
		}

		// 恢复且本工具就是恢复目标：按审批结果决定执行还是拒绝。
		//
		// 这里取 any 再用 ResumedApproval 归一化，而不是直接断言
		// *loop.ApprovalResult：审批结论若过了 gob（checkpoint 往返），指针会被
		// 摊平成值，只断言指针会漏——而漏了不会报错，只会掉进下面"重新中断"
		// 那一支，让审批永远批不下去。
		isTarget, hasData, raw := tool.GetResumeContext[any](ctx)
		if isTarget && hasData {
			if data, ok := loop.ResumedApproval(raw); ok {
				if data.Approved {
					return endpoint(ctx, stored, opts...)
				}
				if data.DisapproveReason != nil {
					return fmt.Sprintf("用户拒绝了 %s：%s", tCtx.Name, *data.DisapproveReason), nil
				}
				return fmt.Sprintf("用户拒绝了 %s", tCtx.Name), nil
			}
		}

		// 恢复的是别的 interrupt：本次审批还没结论，重新中断并保留自己的状态。
		// 少了这一支，多中断的一轮会静默执行未经批准的工具。
		if !isTarget {
			return "", tool.StatefulInterrupt(ctx, &loop.ApprovalInfo{
				ToolName:        tCtx.Name,
				ArgumentsInJSON: stored,
			}, stored)
		}

		return endpoint(ctx, stored, opts...)
	}, nil
}
