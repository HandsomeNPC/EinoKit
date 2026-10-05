package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type ThreadApi struct{}

var ThreadApiApp = new(ThreadApi)

// CreateThread 创建会话。POST /threads
func (a *ThreadApi) CreateThread(c *gin.Context) {

	c.JSON(http.StatusOK, gin.H{})
}

// ListThreads 列出全部会话。GET /threads
func (a *ThreadApi) ListThreads(c *gin.Context) {
	c.JSON(http.StatusOK, []any{})
}

// DeleteThread 删除指定会话。DELETE /threads/:id
func (a *ThreadApi) DeleteThread(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

// Chat 会话对话。POST /threads/:id/chat
func (a *ThreadApi) Chat(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{})
}

// Render 渲染会话历史。GET /threads/:id/render
func (a *ThreadApi) Render(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{})
}

// Abort 终止当前会话的运行。POST /threads/:id/abort
func (a *ThreadApi) Abort(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{})
}

// Upload 上传文档到会话工作区。POST /threads/:id/docs
func (a *ThreadApi) Upload(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{})
}
