package thread

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Upload 上传文档到会话工作区。POST /threads/docs 暂时不实现
func Upload(c *gin.Context) {
	//TODO 暂时不实现这个接口
	c.JSON(http.StatusOK, gin.H{})
}
