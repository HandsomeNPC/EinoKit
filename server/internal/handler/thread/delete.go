package thread

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/gin-gonic/gin"

	"github.com/HandsomeNPC/EinoKit/internal/checkpoint"
	"github.com/HandsomeNPC/EinoKit/internal/memory/thread"
	"github.com/HandsomeNPC/EinoKit/internal/model/bo"
	"github.com/HandsomeNPC/EinoKit/internal/turnloop"
)

// DeleteThread 删除指定会话。POST /threads/delete
func DeleteThread(c *gin.Context) {
	var req bo.ThreadRequest
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
	if ok {
		loop.Stop(adk.WithGraceful())
		loop.Wait()
	}

	if err := thread.DeleteByID(threadID); err != nil {
		log.Printf("[thread] 删除会话失败 thread=%s: %v", threadID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除会话失败"})
		return
	}
	// 清掉可能残留的审批快照，避免同 ID 复用时读到过期状态。
	if deleter, ok := checkpoint.GetInstance().(interface {
		Delete(ctx context.Context, key string) error
	}); ok {
		if err := deleter.Delete(c.Request.Context(), threadID); err != nil {
			log.Printf("warn: 删除 checkpoint 失败 thread=%s: %v", threadID, err)
		}
	}
	c.Status(http.StatusNoContent)
}
