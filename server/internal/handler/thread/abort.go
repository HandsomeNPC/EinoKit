package thread

import (
	"net/http"
	"strings"

	"github.com/HandsomeNPC/EinoKit/internal/memory/thread"
	"github.com/HandsomeNPC/EinoKit/internal/model/bo"
	"github.com/cloudwego/eino/adk"
	"github.com/gin-gonic/gin"

	"github.com/HandsomeNPC/EinoKit/internal/turnloop"
)

// Abort 终止当前会话的运行。POST /threads/abort
func Abort(c *gin.Context) {
	var req bo.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误: " + err.Error()})
		return
	}
	threadID := strings.TrimSpace(req.ThreadID)
	if threadID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "threadId 不能为空"})
		return
	}
	if _, err := thread.SelectByID(threadID); err != nil {
		writeThreadLookupError(c, err)
		return
	}

	loop, ok := turnloop.TurnloopHolderInstance.Get(threadID)
	if !ok {
		return
	}
	loop.Stop(adk.WithGraceful())
	loop.Wait()
	c.JSON(http.StatusOK, gin.H{"aborted": true})
}
