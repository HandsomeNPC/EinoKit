package turnloop

// ChatItem 每一轮用户输入的对话信息
type ChatItem struct {
	Query string // 用户消息文本
	// Resume 是本条目要解决的中断反馈:key 为 interrupt ID,value 为反馈内容
	Resume map[string]any
	// ThreadID 指回提交本条目的会话,让回调能找到对应的 SSE 流与 runID。
	ThreadID string
}
