// Package handler HTTP 接口层，负责参数解析与响应封装。
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	agui "github.com/HandsomeNPC/agui-go-sdk"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/gin-gonic/gin"

	"github.com/HandsomeNPC/EinoKit/internal/loop"
	"github.com/HandsomeNPC/EinoKit/internal/model/bo"
	"github.com/HandsomeNPC/EinoKit/internal/repository"
	"github.com/HandsomeNPC/EinoKit/internal/service/agent"
)

// SessionApi 会话相关接口。
type SessionApi struct{}

// SessionApiApp 单例，供路由注册引用。
var SessionApiApp = new(SessionApi)

// CreateSession 创建会话。POST /sessions
func (a *SessionApi) CreateSession(c *gin.Context) {
	id, err := agent.SessionSvcApp.Create(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id})
}

// ListSessions 列出全部会话。GET /sessions
func (a *SessionApi) ListSessions(c *gin.Context) {
	metas, err := agent.SessionSvcApp.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, metas)
}

// DeleteSession 删除指定会话。DELETE /sessions/:id
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

// Chat 会话对话，以 AG-UI 事件流（SSE）返回。POST /sessions/:id/chat
//
// 请求体带 toolCallMessage 即视为审批恢复——TDesign 的审批卡片 respond 之后，
// 前端会把结论 POST 回本端点，而不是另一个 /approve 端点。
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

	if req.IsResume() {
		a.resume(c, id, req)
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

// resume 用审批结论恢复被中断的运行。
func (a *SessionApi) resume(c *gin.Context, id string, req bo.ChatRequest) {
	pending, err := agent.SessionSvcApp.PendingApproval(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if pending == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该会话没有待审批的中断"})
		return
	}

	var approval bo.ToolCallApproval
	if err := json.Unmarshal(req.ToolCallMessage, &approval); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "toolCallMessage 解析失败: " + err.Error()})
		return
	}

	result := &loop.ApprovalResult{Approved: approval.IsApproved()}
	if !result.Approved && approval.Reason != "" {
		reason := approval.Reason
		result.DisapproveReason = &reason
	}

	em := openStream(c, id)
	em.RunStarted()

	// 先把审批结论回填到原来那张审批卡片上：只发 TOOL_CALL_RESULT，
	// 绝不重发 START/ARGS，否则前端会多渲染一张卡片。
	decision, err := json.Marshal(map[string]any{
		"approved": result.Approved,
		"reason":   approval.Reason,
	})
	if err != nil {
		em.RunError("序列化审批结论失败: " + err.Error())
		return
	}
	if pending.ApprovalToolCallID != "" {
		em.ToolResult(events.GenerateMessageID(), pending.ApprovalToolCallID, string(decision))
	}

	turn, err := agent.SessionSvcApp.Resume(c, em, id, pending, result)
	finish(em, turn, err)
}

// Abort 立即终止当前会话的运行。POST /sessions/:id/abort
func (a *SessionApi) Abort(c *gin.Context) {
	if agent.SessionSvcApp.Abort(c.Param("id")) {
		c.JSON(http.StatusOK, gin.H{"status": "aborted"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "no active loop"})
}

// Approve 旧端点，已合并进 /chat。POST /sessions/:id/approve
//
// 保留路由只为给仍在调它的客户端一个明确指引，不再维护第二条代码路径：
// TDesign 的审批卡片 respond 只会 POST 回聊天端点。
func (a *SessionApi) Approve(c *gin.Context) {
	c.JSON(http.StatusGone, gin.H{
		"error": "审批已合并到 POST /sessions/:id/chat，请在请求体中携带 toolCallMessage",
	})
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
// 四种收尾：
//   - 被更新的请求取代：发 RUN_ERROR，不发 RUN_FINISHED（这一轮不算完成）
//   - 出错：发 RUN_ERROR
//   - 中断等待审批：发 RUN_FINISHED + interrupt outcome。TDesign 会把 outcome
//     静默丢弃（Zod $strip），审批 UI 实际由桥接层发的 approval_request 卡片驱动；
//     这里仍按规范发，是为了协议正确性与其他客户端。
//   - 正常结束：发 RUN_FINISHED + success
func finish(em *agui.Emitter, result loop.TurnResult, err error) {
	switch {
	case loop.IsSuperseded(err):
		em.RunError("本轮已被更新的请求取代")
	case err != nil:
		em.RunError(err.Error())
	case result.Err != nil:
		em.RunError(fmt.Sprintf("agent 运行出错: %v", result.Err))
	case result.Interrupted():
		em.RunFinishedInterrupt([]types.Interrupt{{
			ID:         result.InterruptID,
			Reason:     "tool_call",
			Message:    result.Message,
			ToolCallID: result.ApprovalToolCallID,
			Metadata: types.Metadata{
				"toolName":  result.ToolName,
				"arguments": result.Arguments,
			},
		}})
	default:
		em.RunFinishedSuccess()
	}
}
