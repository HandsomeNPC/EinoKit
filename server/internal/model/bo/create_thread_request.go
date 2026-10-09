package bo

// CreateThreadRequest 是创建会话的请求体。两个字段都可省略：
// Title 留空由首轮对话内容回填，AgentType 留空用 demo agent。
type CreateThreadRequest struct {
	Title     string `json:"title"`
	AgentType string `json:"agentType"`
}
