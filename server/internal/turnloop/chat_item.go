package turnloop

import (
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"

	"github.com/HandsomeNPC/EinoKit/internal/model/bo"
)

// ChatItem 每一轮用户输入的对话信息
type ChatItem struct {
	Prompt string // 用户消息文本
	Resume []types.ResumeEntry
}

// NewChatItem 把对话接口的请求体 bo.ChatRequest 转换成一个 ChatItem。
func NewChatItem(req *bo.ChatRequest) *ChatItem {
	if req == nil {
		return nil
	}
	return &ChatItem{
		Prompt: req.Prompt,
		Resume: req.Resume,
	}
}
