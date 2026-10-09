package thread

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"

	"github.com/HandsomeNPC/EinoKit/internal/memory/thread"
	"github.com/HandsomeNPC/EinoKit/internal/model/bo"
	"github.com/HandsomeNPC/EinoKit/internal/msgops"
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

	msgs, err := RenderHistory(id)
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

// RenderHistory 读出会话历史并转成 AG-UI 消息数组，供前端
func RenderHistory(threadID string) ([]types.Message, error) {
	rows, err := thread.SelectMessages(threadID)
	if err != nil {
		return nil, err
	}

	out := make([]types.Message, 0, len(rows))
	for _, row := range rows {
		var batch []*schema.AgenticMessage
		if uErr := json.Unmarshal(row.Payload, &batch); uErr != nil {
			return nil, fmt.Errorf("agui: 解析会话 %s 第 %d 行消息失败: %w", threadID, row.ID, uErr)
		}
		for i, msg := range batch {
			if msg == nil {
				continue
			}
			base := strconv.FormatInt(row.ID, 10) + "-" + strconv.Itoa(i)
			out = append(out, convertMessage(base, msg)...)
		}
	}
	return out, nil
}

// convertMessage 把一条 AgenticMessage 拆成若干 AG-UI 消息。
func convertMessage(baseID string, msg *schema.AgenticMessage) []types.Message {
	toolResults := msgops.ToolResults(msg)

	switch msg.Role {
	case schema.AgenticRoleTypeSystem:
		if text := msgops.Text(msg); text != "" {
			return []types.Message{{ID: baseID, Role: types.RoleSystem, Content: text}}
		}
		return nil

	case schema.AgenticRoleTypeUser:
		// 工具结果在 agentic 表示里也挂 user 角色，必须先分流，
		// 否则工具输出会被渲染成用户气泡。
		if len(toolResults) > 0 {
			return convertToolResults(baseID, toolResults)
		}
		if text := msgops.UserText(msg); text != "" {
			return []types.Message{{ID: baseID, Role: types.RoleUser, Content: text}}
		}
		return nil

	default:
		return convertAssistant(baseID, msg, toolResults)
	}
}

// convertAssistant 处理 assistant 消息：reasoning 单独成条，文本与工具调用
// 合成一条 assistant 消息（AG-UI 的 toolCalls 挂在 assistant 上）。
func convertAssistant(baseID string, msg *schema.AgenticMessage, toolResults []msgops.ToolResult) []types.Message {
	var out []types.Message

	if reasoning := reasoningText(msg); reasoning != "" {
		out = append(out, types.Message{
			ID:      baseID + "-reasoning",
			Role:    types.RoleReasoning,
			Content: reasoning,
		})
	}

	text := msgops.AssistantText(msg)
	calls := msgops.ToolCalls(msg)
	if text != "" || len(calls) > 0 {
		m := types.Message{ID: baseID, Role: types.RoleAssistant}
		if text != "" {
			m.Content = text
		}
		for _, tc := range calls {
			m.ToolCalls = append(m.ToolCalls, types.ToolCall{
				ID:   tc.ID,
				Type: types.ToolCallTypeFunction,
				Function: types.FunctionCall{
					Name:      tc.Name,
					Arguments: tc.Args,
				},
			})
		}
		out = append(out, m)
	}

	// assistant 消息里一般不带工具结果，但带了也不能丢。
	out = append(out, convertToolResults(baseID, toolResults)...)
	return out
}

// convertToolResults 把工具结果转成 AG-UI 的 tool 消息。
func convertToolResults(baseID string, results []msgops.ToolResult) []types.Message {
	out := make([]types.Message, 0, len(results))
	for i, tr := range results {
		out = append(out, types.Message{
			ID:         baseID + "-tool-" + strconv.Itoa(i),
			Role:       types.RoleTool,
			ToolCallID: tr.ID,
			Content:    tr.Content,
		})
	}
	return out
}

// reasoningText 取出 reasoning block 的明文。加密的 reasoning blob 不外传
// （与 Emitter.MessagesSnapshot 的 scrubEncryptedValues 口径一致）。
func reasoningText(msg *schema.AgenticMessage) string {
	var b []byte
	for _, block := range msg.ContentBlocks {
		if block == nil || block.Type != schema.ContentBlockTypeReasoning || block.Reasoning == nil {
			continue
		}
		b = append(b, block.Reasoning.Text...)
	}
	return string(b)
}
