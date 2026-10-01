// Package bo 定义业务对象（business object），承载接口入参的 DTO。
package bo

// ApproveRequest 是人工审批（human-in-the-loop）的请求体。
type ApproveRequest struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
}
