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
// 会话相关接口当前为空返回占位，待 loop 包重写后接入实际逻辑。
func RegisterRoutes(r *gin.Engine) {
	// 健康检查
	r.GET("/health", handler.HealthApiApp.Health)

	// 会话管理
	r.POST("/threads", handler.ThreadApiApp.CreateThread)
	r.GET("/threads", handler.ThreadApiApp.ListThreads)
	r.DELETE("/threads/:id", handler.ThreadApiApp.DeleteThread)

	// 会话交互
	r.POST("/threads/:id/chat", handler.ThreadApiApp.Chat)
	r.GET("/threads/:id/render", handler.ThreadApiApp.Render)
	r.POST("/threads/:id/abort", handler.ThreadApiApp.Abort)
	r.POST("/threads/:id/docs", handler.ThreadApiApp.Upload)
}
