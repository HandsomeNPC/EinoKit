package turnloop

import (
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"

	"github.com/HandsomeNPC/EinoKit/internal/model/bo"
)

// ChatItem 每一轮用户输入的对话信息
type ChatItem struct {
	Prompt string // 用户消息文本
	Resume []types.ResumeEntry

	// turn 是本条目所属轮次的凭据，承载该轮的 emitter/runID 与结束信号。
	//
	// 故意不导出：TurnLoop 用 gob 持久化未处理条目，而 gob 忽略非导出字段。
	// 这正是想要的——从 checkpoint 恢复的旧条目不该带着早已关闭的 SSE 流回来。
	turn *Turn
}

// NewChatItem 把对话接口的请求体 bo.ChatRequest 转换成一个 ChatItem。
// turn 携带本轮的 emitter 与结束信号，由 handler 创建。
func NewChatItem(req *bo.ChatRequest, turn *Turn) *ChatItem {
	if req == nil {
		return nil
	}
	return &ChatItem{
		Prompt: req.Prompt,
		Resume: req.Resume,
		turn:   turn,
	}
}

// Turn 返回本条目所属的轮次凭据，不存在时返回 nil。
func (c *ChatItem) Turn() *Turn {
	if c == nil {
		return nil
	}
	return c.turn
}
