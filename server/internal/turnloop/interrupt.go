package turnloop

import (
	"context"
	"fmt"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/cloudwego/eino/adk"

	"github.com/HandsomeNPC/EinoKit/internal/checkpoint"
	"github.com/HandsomeNPC/EinoKit/internal/memory/thread"
)

// InterruptReasonToolCall 是工具中断的 reason 取值，对齐 AG-UI 的约定。
const InterruptReasonToolCall = "tool_call"

// toolCallIDOf 从中断地址里取出触发该中断的 tool call ID。
//
// compose 在进入工具时压入的地址段是 (ID=工具名, SubID=call ID)
// （compose/tool_node.go: appendToolAddressSegment(ctx, task.name, task.callID)），
// 所以 call ID 在 SubID 而不是 ID。取最后一个 tool 段：嵌套 agent-as-tool 时
// 最内层才是真正发起调用的那个。
func toolCallIDOf(addr adk.Address) string {
	var id string
	for _, seg := range addr {
		if seg.Type == adk.AddressSegmentTool && seg.SubID != "" {
			id = seg.SubID
		}
	}
	return id
}

// interruptsOf 把 ADK 的中断上下文转成 AG-UI 的 []types.Interrupt。
//
// 一次中断可能有多个根因（并行工具调用各自中断），所以返回切片。
func interruptsOf(ictxs []*adk.InterruptCtx) []types.Interrupt {
	out := make([]types.Interrupt, 0, len(ictxs))
	for _, ic := range ictxs {
		if ic == nil {
			continue
		}
		out = append(out, types.Interrupt{
			ID:         ic.ID,
			Reason:     InterruptReasonToolCall,
			Message:    interruptMessage(ic.Info),
			ToolCallID: toolCallIDOf(ic.Address),
		})
	}
	return out
}

// interruptMessage 把工具传给 StatefulInterrupt 的 info 渲染成给用户看的文案。
func interruptMessage(info any) string {
	switch v := info.(type) {
	case nil:
		return ""
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// PendingInterrupts 返回会话当前待确认的中断，供 /threads/render 回填审批卡片。
//
// 语义同 loadPendingInterrupts：checkpoint 不在就返回空，保证返回的中断一定可以 resume。
func PendingInterrupts(ctx context.Context, threadID string) ([]types.Interrupt, error) {
	return loadPendingInterrupts(ctx, threadID)
}

// IsRunning 报告会话是否有正在运行的 loop。
func IsRunning(threadID string) bool {
	_, ok := TurnloopHolderInstance.Get(threadID)
	return ok
}

// loadPendingInterrupts 读出会话的待确认中断。
//
// 先查 checkpoint：它才是能让 ADK 真的 resume 的唯一权威，thread_info 里的那份
// 只是展示/回填副本。checkpoint 不在（TTL 过期、Redis 被清）就一律当作无待确认
// 中断——否则前端会显示一张点下去必然报错的审批卡片。
func loadPendingInterrupts(ctx context.Context, threadID string) ([]types.Interrupt, error) {
	if threadID == "" {
		return nil, nil
	}

	store := checkpoint.GetInstance()
	_, existed, err := store.Get(ctx, threadID)
	if err != nil {
		return nil, fmt.Errorf("turnloop: 查询 checkpoint thread=%s 失败: %w", threadID, err)
	}
	if !existed {
		return nil, nil
	}

	info, err := thread.SelectByID(threadID)
	if err != nil {
		return nil, err
	}
	return info.PendingInterrupt, nil
}
