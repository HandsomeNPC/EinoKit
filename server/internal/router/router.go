// Package router 构建 EinoKit 的 gin 引擎与路由。
package router

import (
	"github.com/gin-gonic/gin"

	"github.com/HandsomeNPC/EinoKit/internal/handler"
)

// InitRouter 构建并返回 gin 引擎(挂载全部中间件与路由)。
func InitRouter() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	RegisterRoutes(r)
	return r
}

// RegisterRoutes 挂载业务路由。路由直接挂在根组下，不带 /api 前缀(CLAUDE.md 约定)。
//
// 会话交互以 AG-UI 协议的 SSE 事件流返回。审批恢复复用 /chat(请求体带
// toolCallMessage)，/approve 仅保留为指引；render 与 docs 尚未实现。
func RegisterRoutes(r *gin.Engine) {
	// 健康检查
	r.GET("/health", handler.HealthApiApp.Health)

	// 会话管理
	r.POST("/sessions", handler.SessionApiApp.CreateSession)
	r.GET("/sessions", handler.SessionApiApp.ListSessions)
	r.DELETE("/sessions/:id", handler.SessionApiApp.DeleteSession)

	// 会话交互
	r.POST("/sessions/:id/chat", handler.SessionApiApp.Chat)
	r.GET("/sessions/:id/render", handler.SessionApiApp.Render)
	r.POST("/sessions/:id/approve", handler.SessionApiApp.Approve)
	r.POST("/sessions/:id/abort", handler.SessionApiApp.Abort)
	r.POST("/sessions/:id/docs", handler.SessionApiApp.Upload)
}
