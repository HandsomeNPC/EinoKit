package thread

import (
	"errors"
	"log"
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
		if errors.Is(err, thread.ErrThreadNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "会话不存在"})
			return
		}
		log.Printf("[thread] 查询会话失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询会话失败"})
		return
	}

	loop, ok := turnloop.TurnloopHolderInstance.Get(threadID)
	if !ok {
		// 没有在跑的 loop 也要清掉待确认中断：用户点"终止"的语义是放弃本轮，
		// 残留的副本会让 /render 回填出一张点下去必然报错的审批卡片。
		clearPending(threadID)
		c.JSON(http.StatusOK, gin.H{"aborted": false, "message": "当前没有运行中的会话"})
		return
	}

	// Graceful：在安全点停，让 ADK 有机会落快照。
	loop.Stop(adk.WithGraceful())
	loop.Wait()

	// 摘掉登记，否则下次请求会对着这个已停止的 loop Push 而拿不到响应。
	turnloop.TurnloopHolderInstance.DeleteIf(threadID, loop)
	clearPending(threadID)

	log.Printf("[thread] 已终止会话 thread=%s", threadID)
	c.JSON(http.StatusOK, gin.H{"aborted": true})
}

// clearPending 清掉待确认中断副本，失败只记日志：终止是幂等操作，
// 调用方只关心"最终没有残留"。
func clearPending(threadID string) {
	if err := thread.ClearPendingInterrupt(threadID); err != nil {
		log.Printf("[thread] 清除待确认中断失败 thread=%s: %v", threadID, err)
	}
}
