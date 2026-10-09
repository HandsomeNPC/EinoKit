package thread

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/HandsomeNPC/EinoKit/internal/memory/thread"
)

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
