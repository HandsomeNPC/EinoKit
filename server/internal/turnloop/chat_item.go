package turnloop

// ChatItem 每一轮用户输入的对话信息
type ChatItem struct {
	Query            string // 用户消息文本(审批条目时为空)
	InterruptContent any    //中断反馈信息
}
