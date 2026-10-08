package thread

import "time"

// ThreadInfo 会话表
type ThreadInfo struct {
	ThreadID  string `gorm:"column:thread_id;type:varchar(64);primaryKey" json:"threadId"`
	Title     string `gorm:"column:title;type:varchar(255);not null;default:''" json:"title"`
	AgentType string `gorm:"column:agent_type;type:varchar(64);not null;default:''" json:"agentType"`

	// PendingInterrupt 是本会话待用户确认的中断快照，内容为 AG-UI
	// []types.Interrupt 的 JSON 数组；无待确认中断时为空。
	//
	// 存在这里而不是只靠前端回传 interruptId：
	//  1. 刷新页面后前端仍能拿回中断详情，把审批卡片恢复出来；
	//  2. resume 时 interruptId 由后端自己查出，不信任客户端传入的值。
	//
	// 注意它只是"展示用"的副本——真正决定能不能 resume 的是 Redis 里的
	// checkpoint（见 internal/checkpoint）。两者可能不一致（如 checkpoint
	// 配了 TTL 过期、或 Redis 被清空），所以读取方必须以 checkpoint 为准，
	// 详见 handler 的 loadPendingInterrupts。
	//
	// json:"-" —— 这张表会被整行序列化成接口响应（见 CreateThread/ListThreads），
	// 中断详情只应由 /threads/render 返回，不该混进会话列表。
	PendingInterrupt []byte `gorm:"column:pending_interrupt;type:jsonb" json:"-"`

	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

// TableName 显式指定表名。
func (ThreadInfo) TableName() string {
	return "thread_info"
}
