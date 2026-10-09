package thread

import (
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/HandsomeNPC/EinoKit/internal/agents"
	"github.com/HandsomeNPC/EinoKit/internal/memory/thread"
	"github.com/HandsomeNPC/EinoKit/internal/model/bo"
)

// CreateThread 创建会话。POST /threads
func CreateThread(c *gin.Context) {
	var req bo.CreateThreadRequest
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
