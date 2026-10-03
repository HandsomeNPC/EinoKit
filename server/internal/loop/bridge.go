package loop

import (
	"log"
	"strings"

	agui "github.com/HandsomeNPC/agui-go-sdk"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// streamTurn 消费一轮 Agent 事件并翻译成 AG-UI 事件。
//
// 不发 RUN_STARTED / RUN_FINISHED —— 生命周期事件由 handler 负责。
func streamTurn(em *agui.Emitter, evs *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) TurnResult {
	var out TurnResult

	bc := &blockCtx{toolCallIDs: map[string]string{}}

	for {
		ev, ok := evs.Next()
		if !ok {
			break
		}

		if ev.Err != nil {
			log.Printf("[bridge] 事件错误: %v", ev.Err)
			bc.close(em)
			out.Err = ev.Err
			return out
		}

		hasExit := ev.Action != nil && ev.Action.Exit
		if ev.Output == nil || ev.Output.MessageOutput == nil {
			if hasExit {
				break
			}
			continue
		}

		// GetMessage 会把流式变体拼成整条消息（并自动关闭流），无需手动 Recv。
		msg, err := ev.Output.MessageOutput.GetMessage()
		if err != nil {
			log.Printf("[bridge] 取消息失败: %v", err)
			if hasExit {
				break
			}
			continue
		}
		if msg == nil {
			if hasExit {
				break
			}
			continue
		}

		out.Intermediates = append(out.Intermediates, msg)
		for _, b := range msg.ContentBlocks {
			if b == nil {
				continue
			}
			if text := bc.emit(em, b); text != "" {
				out.LastContent = text
			}
		}

		if hasExit {
			break
		}
	}

	bc.close(em)
	return out
}

// blockCtx 维护 reasoning / text 块的开关状态与工具调用 ID 映射。
//
// AG-UI 要求 Start/Content/End 严格配对，而模型会在 reasoning 与正文之间来回切换，
// 所以块类型变化时必须先给上一类收尾再开新块。
type blockCtx struct {
	currentBlockType schema.ContentBlockType
	reasoningID      string
	reasoningMsgID   string
	textMessageID    string

	// toolCallIDs 把模型给的 CallID 映射到 AG-UI 分配的 toolCallID，
	// 供后续 FunctionToolResult 回填到同一张卡片。
	toolCallIDs map[string]string
}

// emit 处理单个 content block：按类型发出 AG-UI 事件。
// 返回非空字符串表示这是一段 assistant 正文（供调用方记录 LastContent）。
func (s *blockCtx) emit(em *agui.Emitter, b *schema.ContentBlock) string {
	switch b.Type {
	case schema.ContentBlockTypeFunctionToolCall:
		if b.FunctionToolCall == nil {
			return ""
		}
		// 工具调用与 reasoning/text 属于不同消息段，先收尾当前块。
		s.close(em)
		call := b.FunctionToolCall
		toolCallID := em.ToolStart(call.Name)
		em.ToolArgs(toolCallID, call.Arguments)
		em.ToolEnd(toolCallID)
		if call.CallID != "" {
			s.toolCallIDs[call.CallID] = toolCallID
		}
		log.Printf("[bridge] 工具调用: %s(%s)", call.Name, call.Arguments)

	case schema.ContentBlockTypeFunctionToolResult:
		if b.FunctionToolResult == nil {
			return ""
		}
		s.close(em)
		result := b.FunctionToolResult
		toolCallID, ok := s.toolCallIDs[result.CallID]
		if !ok {
			// 没有对应的 tool call（例如结果先于调用到达），补一个 ID 让前端仍能渲染。
			toolCallID = events.GenerateToolCallID()
		}
		content := functionToolResultText(result)
		em.ToolResult(events.GenerateMessageID(), toolCallID, content)
		log.Printf("[bridge] 工具结果: %s", truncate(content, 200))

	case schema.ContentBlockTypeReasoning:
		// 模型的思考内容（OpenAI 扩展字段，按 content 分片）。
		if b.Reasoning == nil || b.Reasoning.OpenAIExtension == nil {
			return ""
		}
		if s.currentBlockType != schema.ContentBlockTypeReasoning {
			s.close(em)
			s.currentBlockType = schema.ContentBlockTypeReasoning
			s.reasoningID = em.ReasoningStart()
			s.reasoningMsgID = em.ReasoningMessageStart()
		}
		for _, content := range b.Reasoning.OpenAIExtension.Content {
			em.ReasoningContent(s.reasoningMsgID, content.Text)
		}

	case schema.ContentBlockTypeAssistantGenText:
		if b.AssistantGenText == nil {
			return ""
		}
		if s.currentBlockType != schema.ContentBlockTypeAssistantGenText {
			s.close(em)
			s.currentBlockType = schema.ContentBlockTypeAssistantGenText
			s.textMessageID = em.TextStart()
		}
		em.TextContent(s.textMessageID, b.AssistantGenText.Text)
		return b.AssistantGenText.Text
	}
	return ""
}

// close 结束当前所处的 reasoning / text 块，发出配对的 End 事件。
// 在块类型切换、中断、或整条流结束时调用。
func (s *blockCtx) close(em *agui.Emitter) {
	switch s.currentBlockType {
	case schema.ContentBlockTypeReasoning:
		em.ReasoningMessageEnd(s.reasoningMsgID)
		em.ReasoningEnd(s.reasoningID)
	case schema.ContentBlockTypeAssistantGenText:
		em.TextEnd(s.textMessageID)
	}
	s.currentBlockType = ""
}

// functionToolResultText 把一个工具结果的内容块拼成纯文本。
func functionToolResultText(result *schema.FunctionToolResult) string {
	if result == nil {
		return ""
	}
	var parts []string
	for _, block := range result.Content {
		if block == nil {
			continue
		}
		switch block.Type {
		case schema.FunctionToolResultContentBlockTypeText:
			if block.Text != nil {
				parts = append(parts, block.Text.Text)
			}
		default:
			parts = append(parts, strings.TrimSpace(block.String()))
		}
	}
	return strings.Join(parts, "\n")
}

// truncate 截断日志里的长文本。
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
