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
func RegisterRoutes(r *gin.Engine) {
	r.GET("/health", handler.HealthApiApp.Health)
}
