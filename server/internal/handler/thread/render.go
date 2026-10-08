package thread

import (
	"log"
	"net/http"
	"strings"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/gin-gonic/gin"

	"github.com/HandsomeNPC/EinoKit/internal/agui"
	"github.com/HandsomeNPC/EinoKit/internal/memory/thread"
	"github.com/HandsomeNPC/EinoKit/internal/model/bo"
	"github.com/HandsomeNPC/EinoKit/internal/turnloop"
)

// Render 渲染会话历史。POST /threads/render
//
// 返回 AG-UI 的消息数组，供前端 AGUIAdapter.convertHistoryMessages 还原气泡。
// 有待确认中断时连同中断详情一起返回，前端据此把审批卡片恢复出来——
// 这样刷新页面不会丢掉待审批状态。
func Render(c *gin.Context) {
	var req bo.ThreadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误: " + err.Error()})
		return
	}
	id := strings.TrimSpace(req.ThreadID)
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "threadId 不能为空"})
		return
	}
	// 整行读出来：下面要用 info.PendingInterrupt。
	info, err := thread.SelectByID(id)
	if err != nil {
		writeThreadLookupError(c, err)
		return
	}

	msgs, err := agui.RenderHistory(id)
	if err != nil {
		log.Printf("[thread] 渲染会话历史失败 thread=%s: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "渲染会话历史失败"})
		return
	}

	interrupts, err := loadPendingInterrupts(c.Request.Context(), id, info)
	if err != nil {
		// 历史已经读到了，待审批状态读失败不该让整个接口挂掉。
		log.Printf("warn: 读取待确认中断失败 thread=%s: %v", id, err)
	}
	// 显式兜空切片，保证 JSON 是 [] 而不是 null。
	if interrupts == nil {
		interrupts = []types.Interrupt{}
	}

	c.JSON(http.StatusOK, gin.H{
		"threadId": id,
		"messages": msgs,
		// pendingApproval 保留为便捷布尔，与 interrupts 非空等价。
		"pendingApproval": len(interrupts) > 0,
		"interrupts":      interrupts,
		"running":         turnloop.IsRunning(id),
	})
}
