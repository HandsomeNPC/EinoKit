package thread

import "time"

// ThreadInfo 会话表
type ThreadInfo struct {
	ThreadID  string `gorm:"column:thread_id;type:varchar(64);primaryKey" json:"threadId"`
	Title     string `gorm:"column:title;type:varchar(255);not null;default:''" json:"title"`
	AgentType string `gorm:"column:agent_type;type:varchar(64);not null;default:''" json:"agentType"`
	//TODO 这里中断要显示中断信息和中断ID
	PendingInterrupt []byte `gorm:"column:pending_interrupt;type:jsonb" json:"-"`

	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

// TableName 显式指定表名。
func (ThreadInfo) TableName() string {
	return "thread_info"
}
