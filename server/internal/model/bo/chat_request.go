// Package bo 定义业务对象（business object），承载接口入参的 DTO。
package bo

import "encoding/json"

// ChatRequest 是对话接口的请求体，字段对齐 TDesign ChatEngine 的 onRequest。
//
// 带 ToolCallMessage 即视为"审批恢复"：TDesign 的审批卡片 respond 之后，
// 前端会把结果作为 toolCallMessage 再 POST 回同一个对话端点。
type ChatRequest struct {
	UID             string          `json:"uid,omitempty"`             // 业务标识，当前不使用
	Prompt          string          `json:"prompt,omitempty"`          // 用户输入
	ToolCallMessage json.RawMessage `json:"toolCallMessage,omitempty"` // 非空 ⇒ 审批恢复
}

// IsResume 判断本次请求是否为审批恢复。
func (r ChatRequest) IsResume() bool { return len(r.ToolCallMessage) > 0 }

// ToolCallApproval 宽松解析 toolCallMessage 里的审批结论。
//
// 之所以宽松：TDesign 前端 respond 的载荷形状由业务组件自己定；
// 而中断 ID 一律以库里的 pending 状态为准，不信任客户端回传的值。
type ToolCallApproval struct {
	InterruptID string `json:"interruptId,omitempty"`
	ToolCallID  string `json:"toolCallId,omitempty"`
	Approved    *bool  `json:"approved,omitempty"`
	Action      string `json:"action,omitempty"` // approve/reject，Approved 缺失时的兜底
	Reason      string `json:"reason,omitempty"`
}

// IsApproved 归一化审批结论：显式 approved 优先，其次看 action。
func (a ToolCallApproval) IsApproved() bool {
	if a.Approved != nil {
		return *a.Approved
	}
	return a.Action == "approve" || a.Action == "approved"
}
