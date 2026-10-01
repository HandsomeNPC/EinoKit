// Package handler HTTP 接口层，负责参数解析与响应封装。
package handler

import "github.com/gin-gonic/gin"

// SessionApi 会话相关接口（对标 eino-examples/quickstart/chatwitheino/server）。
// 当前仅返回 OK，具体实现待后续接入 Eino ADK。
type SessionApi struct{}

// SessionApiApp 单例，供路由注册引用。
var SessionApiApp = new(SessionApi)

// CreateSession 创建会话。POST /sessions
func (a *SessionApi) CreateSession(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok"})
}

// ListSessions 列出全部会话。GET /sessions
func (a *SessionApi) ListSessions(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok"})
}

// DeleteSession 删除指定会话。DELETE /sessions/:id
func (a *SessionApi) DeleteSession(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok"})
}

// Chat 会话对话（SSE 流式，对标 /sessions/:id/chat）。POST /sessions/:id/chat
func (a *SessionApi) Chat(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok"})
}

// Render 渲染会话历史为 ndjson。GET /sessions/:id/render
func (a *SessionApi) Render(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok"})
}

// Approve 恢复被中断的 agent 运行。POST /sessions/:id/approve
func (a *SessionApi) Approve(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok"})
}

// Abort 立即终止当前会话的运行。POST /sessions/:id/abort
func (a *SessionApi) Abort(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok"})
}

// Upload 上传文档到会话工作区。POST /sessions/:id/docs
func (a *SessionApi) Upload(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok"})
}
