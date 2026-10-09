// Package turnloop 封装 ADK TurnLoop 的各回调。
//
// 本文件是 Eino 事件流 → AG-UI 事件流的桥接。事件映射与各项约束的来龙去脉见
// docs/agui-event-bridge.md，改动前请先读 §2（adapter 的静默丢弃规则）。
package turnloop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/HandsomeNPC/EinoKit/internal/agui"
	"github.com/HandsomeNPC/EinoKit/internal/memory/thread"
	"github.com/HandsomeNPC/EinoKit/internal/msgops"
)

// ErrInterrupted 在 Agent 因等待人工确认而中断时返回，使 TurnLoop 带着
// ExitReason 退出并保留 checkpoint。
//
// 绝不能返回 nil：返回 nil 会让 loop 继续走到"干净退出"，而 TurnLoop 在干净退出时
// 会删掉 checkpoint（adk/turn_loop.go），resume 就彻底失效了。
var ErrInterrupted = errors.New("turnloop: agent 等待人工确认")

// makeOnAgentEvents 返回 OnAgentEvents 回调：消费 Agent 事件流，转成 AG-UI 事件
// 发给前端，并把本轮产生的消息落库。
func makeOnAgentEvents(tc ThreadContext) func(ctx context.Context, turnCtx *adk.TurnContext[*ChatItem, *schema.AgenticMessage], evIter *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) error {
	return func(ctx context.Context, turnCtx *adk.TurnContext[*ChatItem, *schema.AgenticMessage], evIter *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) error {
		threadID := tc.ThreadID

		// emitter/runID 来自本轮条目携带的 turn，而非构造期闭包——loop 跨请求常驻，
		// 每轮的 SSE 流都是新的。
		turn := turnOf(turnCtx.Consumed)
		if turn == nil {
			// 没有 turn 说明条目是从 checkpoint 恢复的（turn 非导出，gob 不保存），
			// 这一轮没有活着的 SSE 流可写。仍要排空事件以完成落库。
			log.Printf("[events] thread=%s 本轮无 turn，仅持久化", threadID)
		}
		s := &turnStream{
			threadID: threadID,
			runID:    turn.RunID(),
			turn:     turn,
			emitter:  turn.emitterOrNil(),
		}
		defer s.flush()

		for {
			event, ok := evIter.Next()
			if !ok {
				break
			}

			// 被抢占的旧轮不发收尾事件：收尾归抢占者那一轮，否则前端会收到两份
			// RUN_FINISHED，第二份把消息状态提前置成 complete。
			select {
			case <-turnCtx.Preempted:
				s.preempted = true
			default:
			}

			if event.Err != nil {
				s.onError(event.Err)
				return nil // 错误已作为 RUN_ERROR 发出，不再终止 loop
			}

			if event.Action != nil && event.Action.Interrupted != nil {
				if err := s.onInterrupt(ctx, event.Action.Interrupted); err != nil {
					log.Printf("[events] thread=%s 保存待确认中断失败: %v", threadID, err)
				}
				return ErrInterrupted
			}

			if event.Output != nil && event.Output.MessageOutput != nil {
				s.onMessage(event.Output.MessageOutput)
			}

			if event.Action != nil && event.Action.Exit {
				break
			}
		}

		s.onFinish(ctx)
		return nil
	}
}

func (t *Turn) emitterOrNil() *agui.Emitter {
	if t == nil {
		return nil
	}
	return t.Emitter
}

// turnStream 持有单轮转换过程中的状态。
type turnStream struct {
	threadID string
	runID    string
	turn     *Turn
	emitter  *agui.Emitter

	// pending 是本轮待落库的消息（assistant 消息与工具结果），统一在收尾时
	// 按一个 run 一行批量写入。
	pending []string

	// openToolCalls 记录已发过 TOOL_CALL_START 的 tool call ID。中断时用它判断
	// 该不该补 TOOL_CALL_END。
	openToolCalls map[string]bool

	preempted bool
}

// emit 判断当前是否还能向前端写事件。
//
// 传输中断后继续排空事件但不再写——持久化必须完成，否则这一轮的对话历史会丢。
func (s *turnStream) canEmit() bool {
	return s.emitter != nil && s.emitter.Err() == nil
}

// onMessage 处理一条消息输出：工具结果、流式 assistant、非流式 assistant。
func (s *turnStream) onMessage(mo *adk.TypedMessageVariant[*schema.AgenticMessage]) {
	if msgops.VariantIsToolResult(mo) {
		s.onToolResult(mo)
		return
	}
	if mo.IsStreaming && mo.MessageStream != nil {
		s.onAssistantStream(mo)
		return
	}
	if !msgops.IsNil(mo.Message) {
		s.onAssistantMessage(mo.Message)
	}
}

// onToolResult 发 TOOL_CALL_END + TOOL_CALL_RESULT 并落库。
//
// TOOL_CALL_END 在这里才发（而非 assistant 流结束时）：发了 END 的 tool call，
// 前端渲染器会在没有 result 时也判为"已结束"。把 END 和 RESULT 放在一起发，
// 中断路径上就天然不会发出任何一个，审批表单得以保持可交互。
func (s *turnStream) onToolResult(mo *adk.TypedMessageVariant[*schema.AgenticMessage]) {
	content, toolCallID, toolName := msgops.DrainToolResult(mo)
	log.Printf("[events] thread=%s 工具结果 tool=%s id=%s (%d 字)", s.threadID, toolName, toolCallID, len(content))

	if s.canEmit() && toolCallID != "" {
		s.emitter.ToolEnd(toolCallID)
		delete(s.openToolCalls, toolCallID)
		// messageID 是 TOOL_CALL_RESULT 的必填字段，缺了整个事件会被前端丢弃。
		s.emitter.ToolResult(s.runID+"-"+toolCallID, toolCallID, content)
	}

	s.persist(msgops.NewToolResult[*schema.AgenticMessage](toolCallID, toolName, content))
}

// onAssistantStream 消费一次流式 assistant 输出。
//
// 一个流里可能同时有 reasoning、文本和多个 tool call 的参数分片，按先后顺序
// 分别开合各自的 AG-UI 事件段。
func (s *turnStream) onAssistantStream(mo *adk.TypedMessageVariant[*schema.AgenticMessage]) {
	var (
		chunks    []*schema.AgenticMessage
		textID    string
		reasonID  string
		reasonMID string
		accText   strings.Builder
		accReason strings.Builder
	)

	// tool call 的参数按 Index 聚合：ID/Name 可能晚于第一个参数分片到达，
	// 必须等拿到 ID 才能发 TOOL_CALL_START，否则前端无法关联。
	tcs := map[int]*tcAcc{}
	var tcOrder []int

	for {
		chunk, err := mo.MessageStream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			log.Printf("[events] thread=%s 读取流失败: %v", s.threadID, err)
			break
		}
		chunks = append(chunks, chunk)

		// reasoning：REASONING_START 本身不开内容块，必须补 MESSAGE_START，
		// 否则前端什么都不渲染。
		if delta := msgops.ReasoningText(chunk); delta != "" {
			accReason.WriteString(delta)
			if s.canEmit() {
				if reasonID == "" {
					reasonID = s.emitter.ReasoningStart()
					reasonMID = s.emitter.ReasoningMessageStart()
				}
				s.emitter.ReasoningContent(reasonMID, delta)
			}
		}

		if delta := msgops.AssistantDeltaText(chunk); delta != "" {
			accText.WriteString(delta)
			if s.canEmit() {
				// reasoning 段必须在正文开始前闭合。
				s.closeReasoning(&reasonID, &reasonMID)
				if textID == "" {
					textID = s.emitter.TextStart()
				}
				s.emitter.TextContent(textID, delta)
			}
		}

		for _, tc := range msgops.ToolCalls(chunk) {
			acc := tcs[tc.Index]
			if acc == nil {
				acc = &tcAcc{}
				tcs[tc.Index] = acc
				tcOrder = append(tcOrder, tc.Index)
			}
			if tc.ID != "" && acc.id == "" {
				acc.id = tc.ID
			}
			if tc.Name != "" && acc.name == "" {
				acc.name = tc.Name
			}
			if tc.Args != "" {
				acc.buf.WriteString(tc.Args)
			}
			s.pumpToolCall(acc)
		}
	}

	if s.canEmit() {
		s.closeReasoning(&reasonID, &reasonMID)
		if textID != "" {
			s.emitter.TextEnd(textID)
		}
		// 流结束时把还没发出的参数补齐（ID 直到最后才出现的情况）。
		for _, idx := range tcOrder {
			s.pumpToolCall(tcs[idx])
		}
	}

	log.Printf("[events] thread=%s assistant 流: 正文 %d 字, reasoning %d 字, tool call %d 个",
		s.threadID, accText.Len(), accReason.Len(), len(tcOrder))

	// 落库用合并后的完整消息：它保留了 reasoning 块与 signature，
	// 回放给模型时需要。
	if len(chunks) == 0 {
		return
	}
	merged, err := msgops.ConcatChunks(chunks)
	if err != nil {
		log.Printf("[events] thread=%s 合并流分片失败: %v", s.threadID, err)
		// 退化：用累积的文本与参数重建一条 assistant 消息，至少不丢对话。
		calls := make([]msgops.ToolCall, 0, len(tcOrder))
		for _, idx := range tcOrder {
			acc := tcs[idx]
			if acc.name == "" {
				continue
			}
			calls = append(calls, msgops.ToolCall{ID: acc.id, Name: acc.name, Args: acc.buf.String(), Index: idx})
		}
		if accText.Len() > 0 || len(calls) > 0 {
			s.persist(msgops.NewAssistant[*schema.AgenticMessage](accText.String(), calls))
		}
		return
	}
	if msgops.HasContent(merged) {
		s.persist(merged)
	}
}

// pumpToolCall 在拿到 tool call ID 后发出 START，并把已累积的参数作为 ARGS 发出。
//
// 参数按原始分片转发：前端 mergeStringContent 对两端都能 JSON.parse 成对象时做的是
// 展开合并而非拼接，分片（单独 parse 失败）才会走字符串拼接。
func (s *turnStream) pumpToolCall(acc *tcAcc) {
	if acc == nil || acc.id == "" || !s.canEmit() {
		return
	}
	if !acc.started {
		if acc.name == "" {
			return // 没有工具名，前端无法匹配渲染组件，再等后续分片
		}
		s.emitter.ToolStartWithID(acc.id, acc.name)
		acc.started = true
		if s.openToolCalls == nil {
			s.openToolCalls = map[string]bool{}
		}
		s.openToolCalls[acc.id] = true
	}
	if acc.buf.Len() > 0 {
		s.emitter.ToolArgs(acc.id, acc.buf.String())
		acc.buf.Reset()
	}
}

// tcAcc 聚合一个 tool call 在流式输出里分批到达的 ID、名称与参数分片。
type tcAcc struct {
	id      string
	name    string
	started bool
	buf     strings.Builder
}

// closeReasoning 闭合 reasoning 段（MESSAGE_END + END 两件事都要发）。
func (s *turnStream) closeReasoning(reasonID, reasonMID *string) {
	if *reasonID == "" {
		return
	}
	s.emitter.ReasoningMessageEnd(*reasonMID)
	s.emitter.ReasoningEnd(*reasonID)
	*reasonID = ""
	*reasonMID = ""
}

// onAssistantMessage 处理非流式 assistant 消息。
func (s *turnStream) onAssistantMessage(msg *schema.AgenticMessage) {
	text := msgops.AssistantText(msg)
	calls := msgops.ToolCalls(msg)
	log.Printf("[events] thread=%s assistant 消息: %d 字, tool call %d 个", s.threadID, len(text), len(calls))

	if s.canEmit() {
		if reasoning := msgops.ReasoningText(msg); reasoning != "" {
			rID := s.emitter.ReasoningStart()
			rMID := s.emitter.ReasoningMessageStart()
			s.emitter.ReasoningContent(rMID, reasoning)
			s.emitter.ReasoningMessageEnd(rMID)
			s.emitter.ReasoningEnd(rID)
		}
		if text != "" {
			mID := s.emitter.TextStart()
			s.emitter.TextContent(mID, text)
			s.emitter.TextEnd(mID)
		}
		for _, tc := range calls {
			if tc.ID == "" || tc.Name == "" {
				continue
			}
			s.emitter.ToolStartWithID(tc.ID, tc.Name)
			s.emitter.ToolArgs(tc.ID, tc.Args)
			if s.openToolCalls == nil {
				s.openToolCalls = map[string]bool{}
			}
			s.openToolCalls[tc.ID] = true
		}
	}

	if msgops.HasContent(msg) {
		s.persist(msg)
	}
}

// onInterrupt 处理中断：落快照并发 RUN_FINISHED 带 interrupt outcome。
//
// 刻意不发该 tool call 的 TOOL_CALL_END / TOOL_CALL_RESULT：前端靠"没有 result
// 且没有 END"把卡片判为可交互状态，审批表单才能渲染出来。见 docs §1。
func (s *turnStream) onInterrupt(ctx context.Context, info *adk.InterruptInfo) error {
	interrupts := interruptsOf(info.InterruptContexts)
	if len(interrupts) == 0 {
		log.Printf("[events] thread=%s 中断但无上下文", s.threadID)
		return nil
	}
	for _, it := range interrupts {
		log.Printf("[events] thread=%s 中断 id=%s toolCallID=%s msg=%q",
			s.threadID, it.ID, it.ToolCallID, it.Message)
	}

	// 先落库再发事件：事件发出后前端立刻可能发起 resume，那时快照必须已就位。
	// 整组覆盖，不追加——二次中断会产生全新 ID，追加会让前端看到失效 ID。
	if err := thread.SavePendingInterrupt(s.threadID, interrupts); err != nil {
		return err
	}

	s.flush()
	if s.canEmit() && !s.preempted {
		s.emitter.RunFinishedInterrupt(interrupts)
	}
	s.turn.finishIfSet()
	return nil
}

// onError 把错误作为 RUN_ERROR 发给前端，并清掉待确认中断。
func (s *turnStream) onError(err error) {
	log.Printf("[events] thread=%s run=%s 事件错误: %v", s.threadID, s.runID, err)
	s.flush()
	s.clearPending()
	if s.canEmit() && !s.preempted {
		s.emitter.RunError(err.Error())
	}
	s.turn.finishIfSet()
}

// onFinish 正常收尾。
func (s *turnStream) onFinish(ctx context.Context) {
	s.flush()
	s.clearPending()

	if s.preempted {
		// 被抢占：不发 RUN_FINISHED，收尾归抢占者那一轮。但本轮的 HTTP 连接仍要
		// 放开，否则 handler 会一直阻塞到超时。
		log.Printf("[events] thread=%s run=%s 被抢占，跳过 RUN_FINISHED", s.threadID, s.runID)
		s.turn.finishIfSet()
		return
	}
	if s.canEmit() {
		s.emitter.RunFinishedSuccess()
	}
	s.turn.finishIfSet()
}

// clearPending 清掉待确认中断副本。所有非中断的收尾路径都要调用，否则会残留
// 一张点下去必然报错的审批卡片。
func (s *turnStream) clearPending() {
	if err := thread.ClearPendingInterrupt(s.threadID); err != nil {
		log.Printf("[events] thread=%s 清除待确认中断失败: %v", s.threadID, err)
	}
}

// persist 把一条消息加入待落库队列。
func (s *turnStream) persist(msg *schema.AgenticMessage) {
	if msg == nil {
		return
	}
	payload, err := json.Marshal(msgops.NormalizeForSession(msg))
	if err != nil {
		log.Printf("[events] thread=%s 序列化消息失败: %v", s.threadID, err)
		return
	}
	s.pending = append(s.pending, string(payload))
}

// flush 把待落库消息一次性写入。幂等：多条收尾路径都会调用。
func (s *turnStream) flush() {
	if len(s.pending) == 0 {
		return
	}
	if s.runID == "" {
		// 没有 runID 无法定位行（resume 自 checkpoint 且无 turn 的极端情况）。
		log.Printf("[events] thread=%s runID 为空，丢弃 %d 条待落库消息", s.threadID, len(s.pending))
		s.pending = nil
		return
	}
	batch := s.pending
	s.pending = nil
	if err := thread.AppendMessages(s.threadID, s.runID, batch); err != nil {
		log.Printf("[events] thread=%s run=%s 落库 %d 条消息失败: %v", s.threadID, s.runID, len(batch), err)
	}
}

// finishIfSet 在 turn 存在时唤醒等待的 handler。
func (t *Turn) finishIfSet() {
	if t == nil {
		return
	}
	t.Finish()
}
