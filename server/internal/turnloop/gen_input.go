package turnloop

import (
	"context"
	"encoding/json"
	"log"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/HandsomeNPC/EinoKit/internal/agui"
	"github.com/HandsomeNPC/EinoKit/internal/memory/thread"
	"github.com/HandsomeNPC/EinoKit/internal/msgops"
)

// makeGenInput 返回 GenInput 回调。每轮开始时调用,从队列里的 ChatItem
// 中挑出第一条 query,持久化为用户消息,再拼上会话历史,产出喂给 Agent
// 的 TypedAgentInput。
func makeGenInput(tc ThreadContext) func(ctx context.Context, loop *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], items []*ChatItem) (*adk.GenInputResult[*ChatItem, *schema.AgenticMessage], error) {
	return func(ctx context.Context, loop *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], items []*ChatItem) (*adk.GenInputResult[*ChatItem, *schema.AgenticMessage], error) {
		threadID := tc.ThreadID
		// 找到第一个带 query 的条目。
		var consumed []*ChatItem
		var remaining []*ChatItem
		var queryItem *ChatItem
		for _, item := range items {
			if queryItem == nil && item.Prompt != "" {
				queryItem = item
				consumed = append(consumed, item)
			} else {
				remaining = append(remaining, item)
			}
		}
		if queryItem == nil {
			// 没有 query 条目 —— 停止 loop。
			loop.Stop(adk.WithStopCause("no query items"))
			return &adk.GenInputResult[*ChatItem, *schema.AgenticMessage]{
				Input:     &adk.TypedAgentInput[*schema.AgenticMessage]{Messages: []*schema.AgenticMessage{msgops.NewUser[*schema.AgenticMessage]("done")}},
				Remaining: items,
			}, nil
		}

		// 本轮的 emitter/runID 来自条目携带的 turn，而非构造期闭包——loop 跨请求
		// 常驻，每轮 SSE 流都是新的。
		turn := turnOf(consumed)
		runID := turn.RunID()

		// 持久化用户消息 —— GenInput 只在前一轮的 OnAgentEvents
		// 完成其中间消息持久化之后才触发,因此会话历史的顺序得以保证正确。
		persistUserMessage(threadID, runID, queryItem.Prompt)

		history := loadHistory(threadID)
		runMessages := buildRunMessages(threadID, history)

		log.Printf("[genInput] thread=%s run=%s query=%q messages=%d", threadID, runID, queryItem.Prompt, len(runMessages))

		// 把 emitter 挂到本轮执行 ctx 上，使工具内部能发 STATE_*/ACTIVITY_*——
		// 这类"执行到第几步"的中途状态在 Eino 事件流里没有对应物。
		// RunCtx 必须从传入的 ctx 派生，以保留 TurnLoop 的取消语义。
		runCtx := ctx
		if turn != nil && turn.Emitter != nil {
			runCtx = agui.WithEmitter(ctx, turn.Emitter)
		}

		return &adk.GenInputResult[*ChatItem, *schema.AgenticMessage]{
			RunCtx: runCtx,
			Input: &adk.TypedAgentInput[*schema.AgenticMessage]{
				Messages:        runMessages,
				EnableStreaming: true,
			},
			Consumed:  consumed,
			Remaining: remaining,
		}, nil
	}
}

// persistUserMessage 把本轮用户输入归一化后落库到会话历史。
func persistUserMessage(threadID, runID, prompt string) {
	userMsg := schema.UserAgenticMessage(prompt)
	userMsg = msgops.NormalizeForSession(userMsg)
	if payload, mErr := json.Marshal(userMsg); mErr == nil {
		if appendErr := thread.AppendMessage(threadID, runID, string(payload)); appendErr != nil {
			log.Printf("warn: failed to persist user message: %v", appendErr)
		}
	} else {
		log.Printf("warn: failed to marshal user message: %v", mErr)
	}
}

// buildRunMessages 在会话历史前面追加一条 [Context] 上下文消息,让
// Agent 在每轮拿到本轮 query 之外的稳定背景信息。这条消息不会落库,
func buildRunMessages(threadID string, history []*schema.AgenticMessage) []*schema.AgenticMessage {
	_ = threadID
	lines := []string{
		"[Context]",
		"你是 EinoKit 的对话 Agent,正在为用户处理本轮请求。",
		"通用规则:",
		"  1. 调用工具后,必须用一段文本向用户复述结果;不要只回工具调用。",
		"  2. 不得在本轮结束时只输出工具调用而无文本响应。",
		// TODO: 按需追加项目根目录、上传文件列表、业务规则等上下文。
	}
	ctx := strings.Join(lines, "\n")
	runMessages := make([]*schema.AgenticMessage, 0, len(history)+1)
	runMessages = append(runMessages, schema.UserAgenticMessage(ctx))
	runMessages = append(runMessages, msgops.NormalizeMessagesForModelInput(history)...)
	return runMessages
}

// loadHistory 从 thread_message 表读出全部消息并反序列化为 AgenticMessage
// 列表,顺序按 run 行追加时间、行内按数组顺序展开,对标 mem.Session.GetMessages。
func loadHistory(threadID string) []*schema.AgenticMessage {
	rows, err := thread.SelectMessages(threadID)
	if err != nil {
		log.Printf("warn: failed to load history: %v", err)
		return nil
	}
	msgs := make([]*schema.AgenticMessage, 0, len(rows))
	for _, r := range rows {
		// 每行 payload 是本轮(run)内全部消息的 JSON 数组。
		var batch []*schema.AgenticMessage
		if uErr := json.Unmarshal(r.Payload, &batch); uErr != nil {
			log.Printf("warn: failed to unmarshal message batch: %v", uErr)
			continue
		}
		for i := range batch {
			if batch[i] != nil {
				msgs = append(msgs, msgops.NormalizeForSession(batch[i]))
			}
		}
	}
	return msgs
}
