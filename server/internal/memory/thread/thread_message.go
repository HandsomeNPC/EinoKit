package thread

import "time"

// ThreadMessage 单条对话消息（thread_message）。
type ThreadMessage struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	ThreadID  string    `gorm:"column:thread_id;type:varchar(64);not null;index" json:"threadId"`
	RunID     string    `gorm:"column:run_id;type:varchar(64);not null;default:'';index" json:"runId"`
	Payload   []byte    `gorm:"column:payload;type:jsonb;not null" json:"payload"` // 已序列化的消息 JSON；用 []byte 以便 pgx 正确绑定 jsonb
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
}

// TableName 显式指定表名。
func (ThreadMessage) TableName() string {
	return "thread_message"
}
