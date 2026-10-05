package turnloop

// ChatItem 每一轮用户输入的对话信息
type ChatItem struct {
	Query            string // 用户消息文本(审批条目时为空)
	InterruptContent any    // 中断反馈信息
	InterruptID      string // 本条目所解决的 interrupt 的 ID(审批恢复时由调用方填)
}
