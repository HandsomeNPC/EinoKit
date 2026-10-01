package model

import "time"

// SessionMessage 单条对话消息（session_message）。
type SessionMessage struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	SessionID string    `gorm:"column:session_id;index" json:"sessionId"`
	Payload   string    `gorm:"column:payload;type:jsonb" json:"payload"` // 已序列化的消息 JSON
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
}

// TableName 显式指定表名。
func (SessionMessage) TableName() string {
	return "session_message"
}
