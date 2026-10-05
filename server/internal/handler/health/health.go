// Package health 健康检查接口。
package health

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/HandsomeNPC/EinoKit/internal/service"
)

// Health 健康检查。
func Health(c *gin.Context) {
	c.JSON(http.StatusOK, service.HealthSvcApp.Check())
}
