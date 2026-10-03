package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/cloudwego/eino/schema"

	"github.com/HandsomeNPC/EinoKit/internal/repository"
)

// Responses / Ark API 在回放历史时要求 item 的状态与 ID 完整，
// 这几个 key 由各 agentic 模型实现挂在 ContentBlock.Extra 上。
const (
	arkItemStatusKey    = "ark-item-status"
	openAIItemStatusKey = "openai-item-status"
	itemStatusCompleted = "completed"
)

// marshalMsg 把一条消息序列化成入库用的 JSON。
func marshalMsg(m Msg) (string, error) {
	data, err := json.Marshal(normalizeForSession(m))
	if err != nil {
		return "", fmt.Errorf("loop: 序列化消息失败: %w", err)
	}
	return string(data), nil
}

// unmarshalMsg 把库里的一行 payload 还原成消息。
func unmarshalMsg(payload []byte) (Msg, error) {
	var msg schema.AgenticMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		return nil, fmt.Errorf("loop: 反序列化消息失败: %w", err)
	}
	return normalizeForSession(&msg), nil
}

// loadHistory 按追加顺序读出会话全部消息。
// 单行解析失败只记日志跳过——一条坏数据不该让整个会话打不开。
func loadHistory(ctx context.Context, repo *repository.SessionRepository, sessionID string) ([]Msg, error) {
	rows, err := repo.SelectMessages(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]Msg, 0, len(rows))
	for _, row := range rows {
		msg, err := unmarshalMsg(row.Payload)
		if err != nil {
			log.Printf("[loop] 跳过损坏的消息 session=%s id=%d: %v", sessionID, row.ID, err)
			continue
		}
		out = append(out, msg)
	}
	return out, nil
}

// normalizeForSession 返回一条"可安全入库、也可安全回放给模型"的消息。
//
// agentic 模型实现会把 Responses API 的 item ID 挂在 content block 上，回放
// assistant 消息 / reasoning 块 / 工具调用时**必须**带上这些 ID，否则 API 会拒绝；
// 同时流式专用的 StreamingMeta 必须剥掉。所以写入和读取两侧都要过这一道。
func normalizeForSession(msg Msg) Msg {
	if msg == nil {
		return nil
	}

	out := &schema.AgenticMessage{
		Role:          msg.Role,
		ContentBlocks: make([]*schema.ContentBlock, 0, len(msg.ContentBlocks)),
		Extra:         cloneMap(msg.Extra),
	}
	for _, block := range msg.ContentBlocks {
		if nb := normalizeBlock(block); nb != nil {
			out.ContentBlocks = append(out.ContentBlocks, nb)
		}
	}
	return out
}

// normalizeMessagesForModelInput 在把历史喂回模型前做一遍归一化。
// 当前与 normalizeForSession 等价；留出这层边界，便于日后区分入库格式与模型入参规则。
func normalizeMessagesForModelInput(msgs []Msg) []Msg {
	out := make([]Msg, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, normalizeForSession(m))
	}
	return out
}

func normalizeBlock(block *schema.ContentBlock) *schema.ContentBlock {
	if block == nil {
		return nil
	}
	out := *block
	out.Extra = normalizeBlockExtra(block.Type, block.Extra)
	out.StreamingMeta = nil
	return &out
}

// normalizeBlockExtra 给需要的块补上 completed 状态，并保留原有的 item ID 等 extra。
func normalizeBlockExtra(blockType schema.ContentBlockType, extra map[string]any) map[string]any {
	out := make(map[string]any, len(extra)+2)
	for k, v := range extra {
		out[k] = v
	}
	if needsCompletedStatus(blockType) {
		out[arkItemStatusKey] = itemStatusCompleted
		out[openAIItemStatusKey] = itemStatusCompleted
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func needsCompletedStatus(blockType schema.ContentBlockType) bool {
	switch blockType {
	case schema.ContentBlockTypeReasoning,
		schema.ContentBlockTypeAssistantGenText,
		schema.ContentBlockTypeFunctionToolCall:
		return true
	default:
		return false
	}
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// userText 取用户消息的纯文本，用于给会话生成标题。
func userText(msg Msg) string {
	if msg == nil || msg.Role != schema.AgenticRoleTypeUser {
		return ""
	}
	var parts []string
	for _, block := range msg.ContentBlocks {
		if block != nil && block.Type == schema.ContentBlockTypeUserInputText && block.UserInputText != nil {
			parts = append(parts, block.UserInputText.Text)
		}
	}
	return strings.Join(parts, "\n")
}
