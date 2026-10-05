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

// RegisterRoutes 挂载业务路由。路由直接挂在根组下，不带 /api 前缀(CLAUDE.md 约定)。
//
// 会话相关接口当前为空返回占位，待 loop 包重写后接入实际逻辑。
func RegisterRoutes(r *gin.Engine) {
	// 健康检查
	r.GET("/health", healthHandler.Health)

	// 会话管理
	r.POST("/threads", threadHandler.CreateThread)
	r.GET("/threads", threadHandler.ListThreads)
	r.DELETE("/threads/:id", threadHandler.DeleteThread)

	// 会话交互
	r.POST("/threads/:id/chat", threadHandler.Chat)
	r.GET("/threads/:id/render", threadHandler.Render)
	r.POST("/threads/:id/abort", threadHandler.Abort)
	r.POST("/threads/:id/docs", threadHandler.Upload)
}
