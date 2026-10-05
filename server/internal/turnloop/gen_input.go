package turnloop

import (
	"context"
	"encoding/json"
	"log"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/HandsomeNPC/EinoKit/internal/memory/thread"
	"github.com/HandsomeNPC/EinoKit/internal/msgops"
)

// makeGenInput 返回 GenInput 回调。每轮开始时调用,从队列里的 ChatItem
// 中挑出第一条 query,持久化为用户消息,再拼上会话历史,产出喂给 Agent
// 的 TypedAgentInput。
func makeGenInput(threadID, runID string) func(ctx context.Context, loop *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], items []*ChatItem) (*adk.GenInputResult[*ChatItem, *schema.AgenticMessage], error) {
	return func(ctx context.Context, loop *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], items []*ChatItem) (*adk.GenInputResult[*ChatItem, *schema.AgenticMessage], error) {
		// 找到第一个带 query 的条目。
		var consumed []*ChatItem
		var remaining []*ChatItem
		var queryItem *ChatItem
		for _, item := range items {
			if queryItem == nil && item.Query != "" {
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
				Input: &adk.TypedAgentInput[*schema.AgenticMessage]{
					Messages: []*schema.AgenticMessage{schema.UserAgenticMessage("done")},
				},
				Remaining: items,
			}, nil
		}

		// 现在持久化用户消息 —— GenInput 只在前一轮的 OnAgentEvents
		// 完成其中间消息持久化之后才触发,因此会话历史的顺序得以保证正确。
		userMsg := schema.UserAgenticMessage(queryItem.Query)
		userMsg = msgops.NormalizeForSession(userMsg)
		if payload, mErr := json.Marshal(userMsg); mErr == nil {
			if appendErr := thread.AppendMessage(threadID, runID, string(payload)); appendErr != nil {
				log.Printf("warn: failed to persist user message: %v", appendErr)
			}
		} else {
			log.Printf("warn: failed to marshal user message: %v", mErr)
		}

		history := loadHistory(threadID)
		runMessages := buildRunMessages(threadID, history)

		log.Printf("[genInput] thread=%s query=%q messages=%d", threadID, queryItem.Query, len(runMessages))

		return &adk.GenInputResult[*ChatItem, *schema.AgenticMessage]{
			Input: &adk.TypedAgentInput[*schema.AgenticMessage]{
				Messages:        runMessages,
				EnableStreaming: true,
			},
			Consumed:  consumed,
			Remaining: remaining,
		}, nil
	}
}

// buildRunMessages 在会话历史前面追加一条 [Context] 上下文消息,让
// Agent 在每轮拿到本轮 query 之外的稳定背景信息。这条消息不会落库,
// 只在 GenInput 拼 Input 时临时注入,对标 eino-examples 的 buildRunMessages。
//
// 当前仅放占位提示词;后续如需注入项目根目录、工作区上传文件等,
// 在此处扩展 lines 即可,不必改 makeGenInput。
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
