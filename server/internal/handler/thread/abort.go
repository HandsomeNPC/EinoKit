package thread

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/HandsomeNPC/EinoKit/internal/turnloop"
)

// Abort 终止当前会话的运行。POST /threads/abort
func Abort(c *gin.Context) {
	id, ok := bindThreadID(c)
	if !ok {
		return
	}

	if !turnloop.Abort(id) {
		c.JSON(http.StatusOK, gin.H{"aborted": false, "message": "该会话当前没有运行中的任务"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"aborted": true})
}
