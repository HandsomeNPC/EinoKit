// Package bo 定义业务对象（business object），承载接口入参的 DTO。
package bo

// ChatRequest 是对话接口的请求体
type ChatRequest struct {
	TDesignChatRequestParams
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
