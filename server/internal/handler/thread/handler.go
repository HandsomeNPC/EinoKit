// Package thread 会话相关接口。
package thread

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CreateThread 创建会话。POST /threads
func CreateThread(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{})
}

// ListThreads 列出全部会话。GET /threads
func ListThreads(c *gin.Context) {
	c.JSON(http.StatusOK, []any{})
}

// DeleteThread 删除指定会话。DELETE /threads/:id
func DeleteThread(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

// Chat 会话对话。POST /threads/:id/chat
func Chat(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{})
}

// Render 渲染会话历史。GET /threads/:id/render
func Render(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{})
}

// Abort 终止当前会话的运行。POST /threads/:id/abort
func Abort(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{})
}

// Upload 上传文档到会话工作区。POST /threads/:id/docs
func Upload(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{})
}
