package thread

import "time"

// ThreadInfo 会话表
type ThreadInfo struct {
	ThreadID         string                 `gorm:"column:thread_id;type:varchar(64);primaryKey" json:"threadId"`
	Title            string                 `gorm:"column:title;type:varchar(255);not null;default:''" json:"title"`
	AgentType        string                 `gorm:"column:agent_type;type:varchar(64);not null;default:''" json:"agentType"`
	PendingInterrupt []PendingInterruptItem `gorm:"column:pending_interrupt;type:jsonb;serializer:json" json:"-"`
	CreatedAt        time.Time              `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt        time.Time              `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

// PendingInterruptItem 是单个待确认中断的展示/回填快照
type PendingInterruptItem struct {
	InterruptID   string `json:"interruptId"`
	InterruptInfo any    `json:"interruptInfo"`
	ToolCallID    string `json:"toolCallId,omitempty"`
}

// TableName 显式指定表名。
func (ThreadInfo) TableName() string {
	return "thread_info"
}
