// Package router 构建 EinoKit 的 gin 引擎与路由。
package router

import (
	"github.com/gin-gonic/gin"

	healthHandler "github.com/HandsomeNPC/EinoKit/internal/handler/health"
	threadHandler "github.com/HandsomeNPC/EinoKit/internal/handler/thread"
)

// InitRouter 构建并返回 gin 引擎(挂载全部中间件与路由)。
func InitRouter() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	RegisterRoutes(r)
	return r
}

// RegisterRoutes 挂载业务路由。
//
// 路由不带 /api 前缀：开发期由 vite 代理 rewrite 掉（见 web/vite.config.ts），
// 生产由网关负责，后端只认裸路径。
//
// 会话级接口统一用 POST + 请求体里的 threadId，不走 URL 路径参数——
// 前端切会话时只改 body，SSE endpoint 保持静态。
func RegisterRoutes(r *gin.Engine) {
	// 健康检查
	r.GET("/health", healthHandler.Health)

	// 会话管理
	r.POST("/threads", threadHandler.CreateThread)
	r.GET("/threads", threadHandler.ListThreads)
	r.POST("/threads/delete", threadHandler.DeleteThread)

	// 会话交互
	r.POST("/threads/chat", threadHandler.Chat)
	r.POST("/threads/render", threadHandler.Render)
	r.POST("/threads/abort", threadHandler.Abort)
	r.POST("/threads/docs", threadHandler.Upload)
}
