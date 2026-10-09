package thread

import (
	"net/http"
	"strings"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/gin-gonic/gin"

	"github.com/HandsomeNPC/EinoKit/internal/agui"
	"github.com/HandsomeNPC/EinoKit/internal/memory/thread"
	"github.com/HandsomeNPC/EinoKit/internal/model/bo"
	"github.com/HandsomeNPC/EinoKit/internal/turnloop"
)

// Chat 会话对话。POST /threads/chat
func Chat(c *gin.Context) {
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
	runID := events.GenerateRunID()
	emitter := agui.NewEmitter(c, threadID, runID)
	tc := turnloop.ThreadContext{ThreadID: threadID, Emitter: emitter}
	writeSSEHeaders(c)
	//TODO 把请求转换成ChatItem
	if req.IsResume() {
		handleResume()
	} else {
		handlePrompt()
	}
}

// writeSSEHeaders 写 SSE 响应头。关掉 Nginx 缓冲，否则事件会被攒着不下发。
func writeSSEHeaders(c *gin.Context) {
	h := c.Writer.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Flush()
}

// handleResume 处理中断继续请求
func handleResume() {
	//TODO 从TurnLoopHolder获取TurnLoop继续回复,获取不到则新建一个
}

// 处理非中断请求
func handlePrompt() {
	//TODO 从TurnLoopHolder获取TurnLoop,获取到了先调用Preempt，
	//     Preempt调用失败则调用普通的对话,如果获取不到TurnLoop则新建一个再对话
}
