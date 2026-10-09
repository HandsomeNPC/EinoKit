package turnloop

import (
	"context"
	"errors"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/HandsomeNPC/EinoKit/internal/agui"
)

// makeGenResume 返回 GenResume 回调。由 OnAgentEvents 在 resume 完成后落库(走 internal/memory/thread)。
//
// interruptID 不取前端传入值：客户端只表达"批准还是拒绝"，ID 一律从后端自己的
// pending_interrupt 快照里取，避免过期或伪造的 ID。
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

		// 待确认中断的权威 ID 来自后端快照。快照为空说明 checkpoint 与 thread_info
		// 不一致（如 TTL 过期后又被 resume），此时无法构造 Targets。
		pending, err := loadPendingInterrupts(ctx, tc.ThreadID)
		if err != nil {
			return nil, err
		}
		if len(pending) == 0 {
			return nil, errors.New("turnloop: 没有待确认的中断，无法 resume")
		}

		targets := make(map[string]any, len(pending))
		for i, ic := range pending {
			// 按序号与前端回传的决定配对：前端只给 status/payload，顺序即对应关系。
			entry := types.ResumeEntry{Status: types.ResumeStatusResolved}
			if i < len(interruptItem.Resume) {
				entry = interruptItem.Resume[i]
			}
			entry.InterruptID = ic.ID
			targets[ic.ID] = entry
		}

		// resume 轮同样要让工具能发 STATE_*/ACTIVITY_*。
		runCtx := ctx
		if turn := turnOf(newItems); turn != nil && turn.Emitter != nil {
			runCtx = agui.WithEmitter(ctx, turn.Emitter)
		}

		// newItems 必须并入 Consumed：不在 Consumed 也不在 Remaining 的条目会被
		// 丢弃，恢复条目一旦被丢，OnAgentEvents 就拿不到本轮的 turn/emitter。
		consumed := append(append([]*ChatItem{}, interruptedItems...), newItems...)

		return &adk.GenResumeResult[*ChatItem, *schema.AgenticMessage]{
			RunCtx: runCtx,
			ResumeParams: &adk.ResumeParams{
				Targets: targets,
			},
			Consumed:  consumed,
			Remaining: unhandledItems,
		}, nil
	}
}
