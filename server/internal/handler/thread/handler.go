// Package thread 会话相关接口。
package thread

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/HandsomeNPC/EinoKit/internal/memory/thread"
	"github.com/HandsomeNPC/EinoKit/internal/model/bo"
)

// bindThreadID 从请求体读出 threadId 并校验会话存在。
//
// 会话级接口的公共前置：失败时已写好响应（400/404/500），调用方直接 return。
func bindThreadID(c *gin.Context) (string, bool) {
	var req bo.ThreadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误: " + err.Error()})
		return "", false
	}
	id := strings.TrimSpace(req.ThreadID)
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "threadId 不能为空"})
		return "", false
	}
	if _, err := thread.SelectByID(id); err != nil {
		writeThreadLookupError(c, err)
		return "", false
	}
	return id, true
}

// writeThreadLookupError 把查会话的错误翻译成 404 / 500。
func writeThreadLookupError(c *gin.Context, err error) {
	if errors.Is(err, thread.ErrThreadNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "会话不存在"})
		return
	}
	log.Printf("[thread] 查询会话失败: %v", err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "查询会话失败"})
}
