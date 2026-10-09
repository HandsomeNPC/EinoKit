package thread

import (
	"context"
	"net/http"
	"strings"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/cloudwego/eino/adk"
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
	chatItem := turnloop.NewChatItem(&req)
	if req.IsResume() {
		handleResume(tc, chatItem)
	} else {
		handlePrompt(tc, chatItem)
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
func handleResume(tc turnloop.ThreadContext, item *turnloop.ChatItem) {
	loop, ok := turnloop.TurnloopHolderInstance.Get(tc.ThreadID)
	if !ok {
		//获取不到TurnLoop则创建一个再继续
		loop = turnloop.NewTurnLoop(tc)
		turnloop.TurnloopHolderInstance.Put(tc.ThreadID, loop)
	}
	loop.Push(item)
	tc.Emitter.RunStarted()
	loop.Run(context.Background())
}

// 处理非中断请求
func handlePrompt(tc turnloop.ThreadContext, item *turnloop.ChatItem) {
	loop, ok := turnloop.TurnloopHolderInstance.Get(tc.ThreadID)
	if !ok {
		//获取不到TurnLoop则创建一个再继续
		loop = turnloop.NewTurnLoop(tc)
		turnloop.TurnloopHolderInstance.Put(tc.ThreadID, loop)
		loop.Push(item)
		tc.Emitter.RunStarted()
		loop.Run(context.Background())
		return
	}
	//如果当前存在Loop,则先Preempt失败则run
	ok, _ = loop.Push(item, adk.WithPreempt[*turnloop.ChatItem, adk.AgenticMessage](adk.AfterToolCalls))
	if !ok {
		loop.Push(item)
	}
	tc.Emitter.RunStarted()
	loop.Run(context.Background())

}
