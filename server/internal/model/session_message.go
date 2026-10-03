package model

import "time"

// SessionMessage 单条对话消息（session_message）。
//
// 按 session_id 过滤、id 升序读取即为对话顺序，故在 session_id 上建索引。
type SessionMessage struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	SessionID string    `gorm:"column:session_id;type:varchar(64);not null;index" json:"sessionId"`
	Payload   []byte    `gorm:"column:payload;type:jsonb;not null" json:"payload"` // 已序列化的消息 JSON；用 []byte 以便 pgx 正确绑定 jsonb
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
}

// TableName 显式指定表名。
func (SessionMessage) TableName() string {
	return "session_message"
}
