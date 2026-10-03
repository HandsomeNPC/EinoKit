package handler

import (
	"errors"
	"fmt"
	"net/http"

	agui "github.com/HandsomeNPC/agui-go-sdk"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/gin-gonic/gin"

	"github.com/HandsomeNPC/EinoKit/internal/loop"
	"github.com/HandsomeNPC/EinoKit/internal/model/bo"
	"github.com/HandsomeNPC/EinoKit/internal/repository"
	"github.com/HandsomeNPC/EinoKit/internal/service/agent"
)

type SessionApi struct{}

var SessionApiApp = new(SessionApi)

// CreateSession 创建会话
func (a *SessionApi) CreateSession(c *gin.Context) {
	id, err := agent.SessionSvcApp.Create(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id})
}

// ListSessions 列出全部会话
func (a *SessionApi) ListSessions(c *gin.Context) {
	metas, err := agent.SessionSvcApp.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, metas)
}

// DeleteSession 删除指定会话
func (a *SessionApi) DeleteSession(c *gin.Context) {
	id := c.Param("id")
	if err := agent.SessionSvcApp.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, repository.ErrSessionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "会话不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// Chat 会话对话，以 AG-UI 事件流（SSE）返回
func (a *SessionApi) Chat(c *gin.Context) {
	id := c.Param("id")

	var req bo.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体解析失败: " + err.Error()})
		return
	}

	// 开流之前先把能用 JSON 拒绝的错误都拒掉：流一旦打开就只能用 RUN_ERROR 报错了。
	exists, err := agent.SessionSvcApp.Exists(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "会话不存在"})
		return
	}

	if req.Prompt == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "prompt 不能为空"})
		return
	}

	em := openStream(c, id)
	em.RunStarted()

	result, err := agent.SessionSvcApp.Chat(c, em, id, req.Prompt)
	finish(em, result, err)
}

// Abort 立即终止当前会话的运行。POST /sessions/:id/abort
func (a *SessionApi) Abort(c *gin.Context) {
	if agent.SessionSvcApp.Abort(c.Param("id")) {
		c.JSON(http.StatusOK, gin.H{"status": "aborted"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "no active loop"})
}

// Render 渲染会话历史。GET /sessions/:id/render
func (a *SessionApi) Render(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "尚未实现"})
}

// Upload 上传文档到会话工作区。POST /sessions/:id/docs
func (a *SessionApi) Upload(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "尚未实现"})
}

// openStream 发出 SSE 响应头并构造 emitter。
//
// threadID 用会话 ID：AG-UI 的 thread 概念与本项目的会话一一对应。
func openStream(c *gin.Context, sessionID string) *agui.Emitter {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no") // nginx 反代下禁用缓冲
	c.Writer.Flush()                    // 立刻把 200 + 头部刷出去

	return agui.NewEmitter(c, sessionID, events.GenerateRunID())
}

// finish 根据本轮结果发出终态事件。
//
// 三种收尾：
//   - 被更新的请求取代：发 RUN_ERROR，不发 RUN_FINISHED（这一轮不算完成）
//   - 出错：发 RUN_ERROR
//   - 正常结束：发 RUN_FINISHED + success
func finish(em *agui.Emitter, result loop.TurnResult, err error) {
	switch {
	case loop.IsSuperseded(err):
		em.RunError("本轮已被更新的请求取代")
	case err != nil:
		em.RunError(err.Error())
	case result.Err != nil:
		em.RunError(fmt.Sprintf("agent 运行出错: %v", result.Err))
	default:
		em.RunFinishedSuccess()
	}
}
