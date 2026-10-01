// Package handler HTTP 接口层，负责参数解析与响应封装。
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/HandsomeNPC/EinoKit/internal/service"
)

// HealthApi 健康检查接口。
type HealthApi struct{}

// HealthApiApp 单例，供路由注册引用。
var HealthApiApp = new(HealthApi)

// Health 健康检查。
func (a *HealthApi) Health(c *gin.Context) {
	c.JSON(http.StatusOK, service.HealthSvcApp.Check())
}
