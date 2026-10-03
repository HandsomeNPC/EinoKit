package loop

import (
	"fmt"

	"github.com/cloudwego/eino/schema"
)

// checkpoint 里的状态用 gob 编码，接口字段里的具体类型必须显式注册。
//
// 漏注册的症状是 "gob: type not registered for interface" —— 且只在**恢复时**才炸，
// 此时首轮对话早已看起来完全正常，极难排查。故三者集中在此一处注册：
//   - ChatItem：TurnLoop 的条目类型 T，连同 buffer 一起进快照
//   - ApprovalResult：作为 adk.ResumeParams.Targets 的 any 值
//   - ApprovalInfo：作为 adk.InterruptCtx.Info 的 any 值
func init() {
	schema.RegisterName[ChatItem]("einokit_chat_item")
	schema.RegisterName[ApprovalResult]("einokit_approval_result")
	schema.RegisterName[ApprovalInfo]("einokit_approval_info")
}

// ApprovalInfo 是中断时传给人类的上下文：哪个工具、带什么参数，等你批。
// 它由审批中间件通过 tool.StatefulInterrupt 抛出，最终落在
// adk.InterruptCtx.Info 里，由桥接层取出渲染成审批卡片。
type ApprovalInfo struct {
	ToolName        string // 待审批的工具名
	ArgumentsInJSON string // 工具调用参数(JSON)
}

// String 给出人类可读的审批提示，兼作 Info 的兜底展示文本。
//
// 以下方法一律用**值接收者**：gob 在编码接口值时会把指针摊平，解码出来可能是
// ApprovalInfo 而不是 *ApprovalInfo。值接收者让两种形式都满足接口
// （Go 自动把值方法并入指针的方法集），指针接收者则只有指针满足。
func (ai ApprovalInfo) String() string {
	return fmt.Sprintf("工具 %s 请求执行，参数 %s，等待你的批准", ai.ToolName, ai.ArgumentsInJSON)
}

// GetToolName / GetArgumentsInJSON 满足 aguibridge 的 approvalInfo 接口。
// 桥接层不能直接 import 本包（会成环：loop → aguibridge），故以接口取值。
func (ai ApprovalInfo) GetToolName() string        { return ai.ToolName }
func (ai ApprovalInfo) GetArgumentsInJSON() string { return ai.ArgumentsInJSON }

// PendingApproval 是一次待审批中断的完整上下文，JSON 序列化后存进
// session.pending_approval 列，跨请求存活。
//
// 为什么需要它：恢复时必须对着**原来那张**审批卡片发 TOOL_CALL_RESULT，
// 所以 AG-UI 侧分配的 toolCallID 必须跨请求保存；而原有的 session.msg_idx 是
// int，装不下这些字符串。用一个 JSON 列而非三个字符串列，便于后续扩展。
type PendingApproval struct {
	InterruptID        string `json:"interruptId"`        // adk 的 InterruptCtx.ID，恢复时作为 ResumeParams.Targets 的 key
	ApprovalToolCallID string `json:"approvalToolCallId"` // approval_request 卡片的 AG-UI toolCallID
	CallToolCallID     string `json:"callToolCallId"`     // 被审批的真实工具调用卡片的 AG-UI toolCallID
	ModelCallID        string `json:"modelCallId"`        // 模型给的 FunctionToolCall.CallID，恢复时做 seed 映射的 key
	ToolName           string `json:"toolName"`
	Arguments          string `json:"arguments"`
}

// ResumedApproval 从 resume 上下文里取出审批结论，兼容指针与值两种形式。
//
// 必须兼容两种：审批结论是放在 adk.ResumeParams.Targets（map[string]any）里传下去的，
// 若中途过了 gob（checkpoint 往返），指针会被摊平成值，只断言 *ApprovalResult 会漏。
// 漏了的后果不是报错而是更糟——中间件会走到"重新中断"那一支，审批永远批不下去。
func ResumedApproval(data any) (*ApprovalResult, bool) {
	switch v := data.(type) {
	case *ApprovalResult:
		return v, v != nil
	case ApprovalResult:
		return &v, true
	default:
		return nil, false
	}
}
