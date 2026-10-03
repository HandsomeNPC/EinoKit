// Package aguibridge 是本项目唯一知道 AG-UI 协议的地方：把 Eino ADK 的
// Agent 事件流翻译成 AG-UI 事件推给前端。
//
// 为什么不发 RUN_STARTED / RUN_FINISHED：一轮 TurnLoop 可能被抢占或中断，
// run 的生命周期由 HTTP handler 掌握，桥接层只负责"一轮事件 → 一串 AG-UI 事件"。
//
// 中断为什么渲染成工具卡片：TDesign 的 chat engine 读不到 AG-UI 规范的 interrupt
// ——@tdesign/ai-chat-engine 的 RunFinishedEventSchema 没有 outcome 字段，且 Zod
// schema 用 z.core.$strip，服务端发的 outcome 会在解析阶段被静默丢弃。所以审批
// 必须走 TOOL_CALL_* 事件，让前端用 useAgentToolcall 渲染可交互卡片。
package aguibridge

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	agui "github.com/HandsomeNPC/agui-go-sdk"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// Msg 是本桥接层处理的消息类型。
type Msg = *schema.AgenticMessage

// ApprovalToolName 是审批卡片在 AG-UI 侧的工具名。
// 前端用 useAgentToolcall 注册同名工具（且不提供 handler）即成为可交互审批卡片。
const ApprovalToolName = "approval_request"

// approvalInfo 是桥接层对审批上下文的最小要求。
//
// 用接口而非直接引用 loop.ApprovalInfo：loop 包 import 本包（iterResult =
// TurnResult），反向 import 会成环。中间件抛出的 *loop.ApprovalInfo 恰好满足它。
type approvalInfo interface {
	GetToolName() string
	GetArgumentsInJSON() string
}

// TurnResult 是一轮事件消费完后交回给 loop 层的结果。
type TurnResult struct {
	LastContent   string // 本轮最后一段 assistant 正文（日志用）
	Intermediates []Msg  // 待落库的消息（assistant 文本/工具调用、工具结果）
	Err           error  // 事件流自身的错误

	// 以下字段仅在发生审批中断时非零。
	InterruptID        string // adk 的 InterruptCtx.ID
	ApprovalToolCallID string // approval_request 卡片的 toolCallID，恢复时打到它
	CallToolCallID     string // 被审批的真实工具调用卡片的 toolCallID
	ModelCallID        string // 模型给的 FunctionToolCall.CallID
	ToolName           string
	Arguments          string
	Message            string // 给人看的提示文本
}

// Interrupted 表示本轮因等待人工审批而中断。
func (r TurnResult) Interrupted() bool { return r.InterruptID != "" }

// StreamTurn 消费一轮 Agent 事件并翻译成 AG-UI 事件。
//
// seedToolCallIDs 用于恢复场景：把上一轮已经发出 TOOL_CALL_START 的
// modelCallID→toolCallID 映射预置进来，使本轮到达的 FunctionToolResult 能回填到
// 原来那张卡片上（而不是新开一张）。新一轮传 nil。
//
// 不发 RUN_STARTED / RUN_FINISHED —— 生命周期事件由 handler 负责。
func StreamTurn(em *agui.Emitter, seedToolCallIDs map[string]string, evs *adk.AsyncIterator[*adk.TypedAgentEvent[Msg]]) TurnResult {
	var out TurnResult

	bc := &blockCtx{toolCallIDs: map[string]string{}}
	for k, v := range seedToolCallIDs {
		bc.toolCallIDs[k] = v
	}

	for {
		ev, ok := evs.Next()
		if !ok {
			break
		}

		if ev.Err != nil {
			log.Printf("[aguibridge] 事件错误: %v", ev.Err)
			bc.close(em)
			out.Err = ev.Err
			return out
		}

		// 中断：Agent 暂停等待人工审批。
		if ev.Action != nil && ev.Action.Interrupted != nil {
			bc.close(em)
			fillInterrupt(&out, bc, ev.Action.Interrupted)
			out.ApprovalToolCallID = emitApprovalCard(em, out.InterruptID, out.ToolName, out.Arguments)
			log.Printf("[aguibridge] 中断: id=%s tool=%s", out.InterruptID, out.ToolName)
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
			log.Printf("[aguibridge] 取消息失败: %v", err)
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

// fillInterrupt 从中断信息里取出 root cause 的 ID 与审批上下文填进 TurnResult。
func fillInterrupt(out *TurnResult, bc *blockCtx, info *adk.InterruptInfo) {
	ctxs := info.InterruptContexts

	var chosen *adk.InterruptCtx
	for _, ic := range ctxs {
		if ic != nil && ic.IsRootCause {
			chosen = ic
			break
		}
	}
	if chosen == nil && len(ctxs) > 0 {
		chosen = ctxs[0]
	}
	if chosen == nil {
		return
	}

	out.InterruptID = chosen.ID
	if ai, ok := chosen.Info.(approvalInfo); ok {
		out.ToolName = ai.GetToolName()
		out.Arguments = ai.GetArgumentsInJSON()
	}
	if s, ok := chosen.Info.(fmt.Stringer); ok {
		out.Message = s.String()
	} else if chosen.Info != nil {
		out.Message = fmt.Sprintf("%v", chosen.Info)
	}

	// 把中断关联到它对应的那次工具调用，恢复时要用这两个 ID 做 seed 映射。
	if out.ToolName != "" {
		if modelCallID, toolCallID, ok := bc.lastCallOf(out.ToolName); ok {
			out.ModelCallID = modelCallID
			out.CallToolCallID = toolCallID
		}
	}
}

// emitApprovalCard 发出一张审批卡片（TOOL_CALL_START/ARGS/END 三连）。
// args 里带上 interruptId，前端 respond 时可原样回传。
func emitApprovalCard(em *agui.Emitter, interruptID, toolName, arguments string) string {
	id := em.ToolStart(ApprovalToolName)
	payload, err := json.Marshal(map[string]string{
		"interruptId": interruptID,
		"toolName":    toolName,
		"arguments":   arguments,
	})
	if err != nil {
		log.Printf("[aguibridge] 审批卡片参数序列化失败: %v", err)
		payload = []byte("{}")
	}
	em.ToolArgs(id, string(payload))
	em.ToolEnd(id)
	return id
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
	// callOrder 记录工具调用顺序（modelCallID），用于把中断关联回它的工具调用。
	callOrder []toolCallRef
}

type toolCallRef struct {
	modelCallID string
	toolCallID  string
	toolName    string
}

// lastCallOf 返回指定工具名最近一次调用的 ID 对。
func (s *blockCtx) lastCallOf(toolName string) (modelCallID, toolCallID string, ok bool) {
	for i := len(s.callOrder) - 1; i >= 0; i-- {
		if s.callOrder[i].toolName == toolName {
			return s.callOrder[i].modelCallID, s.callOrder[i].toolCallID, true
		}
	}
	return "", "", false
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
		s.callOrder = append(s.callOrder, toolCallRef{
			modelCallID: call.CallID, toolCallID: toolCallID, toolName: call.Name,
		})
		log.Printf("[aguibridge] 工具调用: %s(%s)", call.Name, call.Arguments)

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
		log.Printf("[aguibridge] 工具结果: %s", truncate(content, 200))

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
