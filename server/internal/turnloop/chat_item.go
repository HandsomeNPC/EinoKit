package turnloop

import "errors"

// ErrSubmitFailed 条目无法入队（loop 反复在退出边界上）时返回。
var ErrSubmitFailed = errors.New("turnloop: 无法提交本次请求，请重试")

// ChatItem 每一轮用户输入的对话信息
type ChatItem struct {
	Query            string // 用户消息文本(审批条目时为空)
	InterruptContent any    // 中断反馈信息
	InterruptID      string // 本条目所解决的 interrupt 的 ID(审批恢复时由调用方填)

	// turn 指回提交本条目的那次 HTTP 请求，让回调能找到该往哪条 SSE 流写、
	// 用哪个 runID 落库。由 Submit 填写，业务代码不该直接设置。
	//
	// 不导出：它是 loop 与 handler 之间的内部关联，序列化进 checkpoint 时
	// 会被丢弃（见 gen_resume 对旧条目的处理）——这是期望行为，重启后
	// 旧请求的 SSE 连接早已断开。
	turn *Turn
}
