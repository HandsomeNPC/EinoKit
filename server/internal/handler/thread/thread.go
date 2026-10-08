package thread

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/HandsomeNPC/EinoKit/internal/agents"
	"github.com/HandsomeNPC/EinoKit/internal/checkpoint"
	"github.com/HandsomeNPC/EinoKit/internal/memory/thread"
	"github.com/HandsomeNPC/EinoKit/internal/turnloop"
)

// createThreadRequest 是创建会话的请求体。两个字段都可省略：
// Title 留空由首轮对话内容回填，AgentType 留空用 demo agent。
type createThreadRequest struct {
	Title     string `json:"title"`
	AgentType string `json:"agentType"`
}

// CreateThread 创建会话。POST /threads
func CreateThread(c *gin.Context) {
	var req createThreadRequest
	// 请求体允许为空（全部走默认值），只有非空且格式错误才拒绝。
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误: " + err.Error()})
		return
	}

	agentType := strings.TrimSpace(req.AgentType)
	if agentType == "" {
		agentType = agents.AgentNameDemo
	}
	// 提前校验 agent 是否注册，避免建完会话后每次 /chat 都失败。
	if !agents.IsRegistered(agentType) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未注册的 agentType: " + agentType})
		return
	}

	info := &thread.ThreadInfo{
		ThreadID:  uuid.New().String(),
		Title:     strings.TrimSpace(req.Title),
		AgentType: agentType,
	}
	if err := thread.Insert(info); err != nil {
		log.Printf("[thread] 创建会话失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建会话失败"})
		return
	}
	c.JSON(http.StatusOK, info)
}

// ListThreads 列出全部会话。GET /threads
func ListThreads(c *gin.Context) {
	rows, err := thread.SelectList()
	if err != nil {
		log.Printf("[thread] 查询会话列表失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询会话列表失败"})
		return
	}
	// 显式兜空切片，保证 JSON 是 [] 而不是 null。
	if rows == nil {
		rows = []*thread.ThreadInfo{}
	}
	c.JSON(http.StatusOK, rows)
}

// DeleteThread 删除指定会话。POST /threads/delete
func DeleteThread(c *gin.Context) {
	id, ok := bindThreadID(c)
	if !ok {
		return
	}

	// 会话上还有常驻 loop 时先停掉并丢弃登记，否则它会继续往已删除的会话里
	// 写消息，还会在 sessions 表里留一个指向不存在会话的对象。
	turnloop.Forget(id)

	if err := thread.DeleteByID(id); err != nil {
		log.Printf("[thread] 删除会话失败 thread=%s: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除会话失败"})
		return
	}
	// 清掉可能残留的审批快照，避免同 ID 复用时读到过期状态。
	if deleter, ok := checkpoint.GetInstance().(interface {
		Delete(ctx context.Context, key string) error
	}); ok {
		if err := deleter.Delete(c.Request.Context(), id); err != nil {
			log.Printf("warn: 删除 checkpoint 失败 thread=%s: %v", id, err)
		}
	}
	c.Status(http.StatusNoContent)
}
