// Package bo 定义业务对象（business object），承载接口入参的 DTO。
package bo

// ChatRequest 是用户发送对话消息的请求体。
type ChatRequest struct {
	Message string `json:"message"`
}
