package agui

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"
	"github.com/gin-gonic/gin"
)

// Emitter 用于向客户端发送AG-UI事件。
//
// 并发安全：SSEWriter 自身不加锁，而工具内部发 STATE_*/ACTIVITY_* 时可能跑在
// ToolsNode 的并发 goroutine 上（compose.parallelRunToolCall 对多个 tool call
// 真的开 goroutine）。没有这把锁，两个事件的 "data: {...}\n\n" 会交错写入同一个
// ResponseWriter，产出客户端无法解析的帧。
type Emitter struct {
	mu       sync.Mutex
	ctx      *gin.Context
	sse      *sse.SSEWriter
	threadID string //
	RunID    string
	err      error // 传输错误
	encErr   error // 编码错误
}

func NewEmitter(ctx *gin.Context, threadID, runID string) *Emitter {
	return &Emitter{ctx: ctx, sse: sse.NewSSEWriter(), threadID: threadID, RunID: runID}
}

func (e *Emitter) Err() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.err
}

func (e *Emitter) EncErr() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.encErr
}

func (e *Emitter) write(ev events.Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return
	}
	if err := e.sse.WriteEvent(e.ctx, e.ctx.Writer, ev); err != nil {
		if isTransportError(err) {
			if e.err == nil {
				e.err = err
			}
			return
		}
		if e.encErr == nil {
			e.encErr = err
		}
	}
}

func isTransportError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "SSE write failed") || strings.Contains(msg, "SSE flush failed")
}

// --- run lifecycle ---

func (e *Emitter) RunStarted() { e.write(events.NewRunStartedEvent(e.threadID, e.RunID)) }

func (e *Emitter) RunFinishedSuccess() {
	e.write(events.NewRunFinishedEventWithOptions(e.threadID, e.RunID, events.WithSuccessOutcome()))
}

func (e *Emitter) RunFinishedInterrupt(interrupts []types.Interrupt) {
	e.write(events.NewRunFinishedEventWithOptions(e.threadID, e.RunID, events.WithInterruptOutcome(interrupts)))
}

func (e *Emitter) RunError(msg string) {
	e.write(events.NewRunErrorEvent(msg, events.WithRunID(e.RunID)))
}

// --- steps ---

func (e *Emitter) StepStarted(name string)  { e.write(events.NewStepStartedEvent(name)) }
func (e *Emitter) StepFinished(name string) { e.write(events.NewStepFinishedEvent(name)) }

// --- text messages ---

func (e *Emitter) TextStart() string {
	messageID := events.GenerateMessageID()
	e.write(events.NewTextMessageStartEvent(messageID, events.WithRole("assistant")))
	return messageID
}

func (e *Emitter) TextContent(messageId, delta string) {
	if delta == "" {
		return // SDK rejects empty deltas
	}
	e.write(events.NewTextMessageContentEvent(messageId, delta))
}

func (e *Emitter) TextEnd(messageId string) { e.write(events.NewTextMessageEndEvent(messageId)) }

// --- reasoning ---

func (e *Emitter) ReasoningStart() string {
	reasoningID := events.GenerateMessageID()
	e.write(events.NewReasoningStartEvent(reasoningID))
	return reasoningID
}

func (e *Emitter) ReasoningMessageStart() string {
	messageID := events.GenerateMessageID()
	e.write(events.NewReasoningMessageStartEvent(messageID, "assistant"))
	return messageID
}

func (e *Emitter) ReasoningContent(id, delta string) {
	if delta == "" {
		return
	}
	e.write(events.NewReasoningMessageContentEvent(id, delta))
}

func (e *Emitter) ReasoningMessageEnd(id string) { e.write(events.NewReasoningMessageEndEvent(id)) }
func (e *Emitter) ReasoningEnd(id string)        { e.write(events.NewReasoningEndEvent(id)) }

// --- tool calls ---

func (e *Emitter) ToolStart(toolCallName string) string {
	toolCallID := events.GenerateToolCallID()
	e.write(events.NewToolCallStartEvent(toolCallID, toolCallName))
	return toolCallID
}

// ToolStartWithID 用指定的 toolCallID 发 TOOL_CALL_START,使后续 TOOL_CALL_ARGS/
// TOOL_CALL_END/TOOL_CALL_RESULT 能与模型侧的 tool call ID 对齐(否则结果事件无法
// 关联回起始事件)。流式 tool call 用这个;非流式且不关心与模型 ID 对齐时用 ToolStart。
func (e *Emitter) ToolStartWithID(toolCallID, toolCallName string) {
	e.write(events.NewToolCallStartEvent(toolCallID, toolCallName))
}

func (e *Emitter) ToolStartWithParentMessageID(toolCallName string, parentMessageID string) string {
	toolCallID := events.GenerateToolCallID()
	event := events.NewToolCallStartEvent(toolCallID, toolCallName, events.WithParentMessageID(parentMessageID))
	e.write(event)
	return toolCallID
}

func (e *Emitter) ToolArgs(toolCallID, delta string) {
	if delta == "" {
		return
	}
	e.write(events.NewToolCallArgsEvent(toolCallID, delta))
}

// ToolArgsAllowEmpty 发送 TOOL_CALL_ARGS,允许 delta 为空字符串。
// 标准 SDK 的 Validate 会拒绝空 delta,这里通过原始字节写入以复现 delta:"" 的场景。
func (e *Emitter) ToolArgsAllowEmpty(toolCallID, delta string) {
	payload, err := json.Marshal(struct {
		Type       string `json:"type"`
		ToolCallID string `json:"toolCallId"`
		Delta      string `json:"delta"`
	}{Type: "TOOL_CALL_ARGS", ToolCallID: toolCallID, Delta: delta})

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return
	}
	if err != nil {
		if e.encErr == nil {
			e.encErr = err
		}
		return
	}
	if err := e.sse.WriteBytes(e.ctx, e.ctx.Writer, payload); err != nil {
		if isTransportError(err) {
			if e.err == nil {
				e.err = err
			}
			return
		}
		if e.encErr == nil {
			e.encErr = err
		}
	}
}

func (e *Emitter) ToolEnd(toolCallID string) { e.write(events.NewToolCallEndEvent(toolCallID)) }

func (e *Emitter) ToolResult(messageID, toolCallID, content string) {
	if content == "" {
		content = "(empty)"
	}
	e.write(events.NewToolCallResultEvent(messageID, toolCallID, content))
}

// --- state ---

func (e *Emitter) StateSnapshot(snapshot any) {
	e.write(events.NewStateSnapshotEvent(snapshot))
}

func (e *Emitter) StateDelta(ops []events.JSONPatchOperation) {
	if len(ops) == 0 {
		return
	}
	e.write(events.NewStateDeltaEvent(ops))
}

// StateDeltaFrom 计算 before -> after 的 JSON Patch 并作为 STATE_DELTA 事件发出。
// 用法:emitter.StateDeltaFrom(prevState, nextState),省去手写 JSONPatchOperation。
func (e *Emitter) StateDeltaFrom(before, after any) {
	e.StateDelta(DiffState(before, after))
}

func (e *Emitter) MessagesSnapshot(msgs []types.Message) {
	e.write(events.NewMessagesSnapshotEvent(scrubEncryptedValues(msgs)))
}

// scrubEncryptedValues 把每条消息里的 EncryptedValue 和 EncryptedContent 两个字段清空,防止加密的推理内容(reasoning blob)随 MESSAGES_SNAPSHOT 泄漏给客户端。
func scrubEncryptedValues(msgs []types.Message) []types.Message {
	needsScrub := false
	for i := range msgs {
		if msgs[i].EncryptedValue != "" || msgs[i].EncryptedContent != "" {
			needsScrub = true
			break
		}
	}
	if !needsScrub {
		return msgs
	}
	out := make([]types.Message, len(msgs))
	copy(out, msgs)
	for i := range out {
		out[i].EncryptedValue = ""
		out[i].EncryptedContent = ""
	}
	return out
}

// --- activity / custom ---

func (e *Emitter) ActivitySnapshot(messageID, activityType string, content any) {
	e.write(events.NewActivitySnapshotEvent(messageID, activityType, content))
}

func (e *Emitter) ActivityDelta(messageID, activityType string, patch []events.JSONPatchOperation) {
	if len(patch) == 0 {
		return
	}
	e.write(events.NewActivityDeltaEvent(messageID, activityType, patch))
}

func (e *Emitter) ReasoningEncryptedValue(subtype events.ReasoningEncryptedValueSubtype, entityID, encryptedValue string) {
	e.write(events.NewReasoningEncryptedValueEvent(subtype, entityID, encryptedValue))
}

func (e *Emitter) Custom(name string, value any) {
	e.write(events.NewCustomEvent(name, events.WithValue(value)))
}
