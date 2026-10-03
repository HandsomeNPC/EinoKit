// Package bo 定义业务对象（business object），承载接口入参的 DTO。
package bo

import "encoding/json"

// ChatRequest 是对话接口的请求体
type ChatRequest struct {
	TDesignChatRequestParams
	UID             string           `json:"uid,omitempty"`             // 业务标识，当前不使用
	ToolCallMessage *ToolCallMessage `json:"toolCallMessage,omitempty"` // 非空 ⇒ 审批恢复
	Action          *A2UIAction      `json:"action,omitempty"`          // A2UI 卡片回传的动作
}

// TDesignChatRequestParams 对齐 TDesign ChatEngine 的 ChatRequestParams 固有部分，
type TDesignChatRequestParams struct {
	Prompt      string           `json:"prompt,omitempty"`      // 用户输入
	MessageID   string           `json:"messageID,omitempty"`   // 前端生成的消息 ID，用于流式定位/补全
	Attachments []AttachmentItem `json:"attachments,omitempty"` // 附件快照，由前端 createChatRequestParams 拍平
}

// AttachmentItem 对齐 TDesign ChatEngine 的 AttachmentItem
type AttachmentItem struct {
	FileType    string         `json:"fileType"` // image/video/audio/pdf/doc/ppt/txt
	Size        int64          `json:"size,omitempty"`
	Name        string         `json:"name,omitempty"`
	URL         string         `json:"url,omitempty"`
	IsReference bool           `json:"isReference,omitempty"` // 是否作为引用附件
	Width       int            `json:"width,omitempty"`
	Height      int            `json:"height,omitempty"`
	Extension   string         `json:"extension,omitempty"` // 不带点，如 png/pdf
	Metadata    map[string]any `json:"metadata,omitempty"`  // 业务自定义元数据
}

// ToolCallMessage 是前端Tool回传的固定结构，
type ToolCallMessage struct {
	ToolCallID   string          `json:"toolCallId,omitempty"`   // 对应工具调用 ID
	ToolCallName string          `json:"toolCallName,omitempty"` // 对应工具调用名称
	Result       json.RawMessage `json:"result,omitempty"`       // 审批响应原始 JSON（对象或数组）
}

// A2UIAction 描述 A2UI 卡片回传的固定结构。
type A2UIAction struct {
	Name      string         `json:"name,omitempty"`      // 动作名，如 approve/reject/submit
	Context   map[string]any `json:"context,omitempty"`   // 业务上下文，形状由卡片自定
	Timestamp string         `json:"timestamp,omitempty"` // 前端回传时间戳
}

// IsResume 判断本次请求是否为继续（审批恢复或 A2UI 卡片动作回传）。
func (r ChatRequest) IsResume() bool {
	return r.ToolCallMessage != nil || r.Action != nil
}

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
