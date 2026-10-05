package thread

import "time"

// ThreadMessage 单条对话消息（thread_message）。
//
// thread_id 标识所属会话，run_id 标识产生该消息的那一轮对话（AG-UI 的 run 概念，
// 一个用户消息触发的一轮 agent 运行共享同一 run_id）。按 thread_id 过滤、id 升序
// 读取即为对话顺序，故在 thread_id 上建索引；run_id 同样建索引以便按轮次检索。
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
