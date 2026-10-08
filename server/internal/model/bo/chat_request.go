// Package bo 定义业务对象（business object），承载接口入参的 DTO。
package bo

import "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"

// ThreadRequest 是所有会话级接口的公共请求体。
//
// threadId 走请求体而非 URL 路径：前端切换会话时只换 body 字段，
// 不必重建 SSE endpoint（ChatEngine 的 endpoint 在 init 时求值，
// 改路径就得 updateEndpoint，还要处理竞态）。
type ThreadRequest struct {
	ThreadID string `json:"threadId"`
}

// ChatRequest 是对话接口的请求体。
//
// 一次请求只做一件事，二者互斥：
//   - 新一轮提问：填 Prompt（可带 Attachments）；
//   - 恢复中断：填 Resume，Prompt 留空。
//
// Resume 直接复用 AG-UI 协议的 ResumeEntry，与前端 AGUIAdapter 同源，
// 批准为 {"interruptId":"...","status":"resolved","payload":true}，
// 拒绝为 {"interruptId":"...","status":"cancelled"}。
// 这是本项目唯一的中断恢复通道——任何工具的任何中断都走它，
// 工具自身的业务语义只体现在 Payload 里，不影响传输格式。
type ChatRequest struct {
	ThreadRequest
	TDesignChatRequestParams
	Resume []types.ResumeEntry `json:"resume,omitempty"`
}

// IsResume 判断本次请求是否为中断恢复。
func (r *ChatRequest) IsResume() bool {
	return len(r.Resume) > 0
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
