package thread

import "time"

// ThreadMessage 单条对话消息记录。一行对应一个 run(一轮对话),
// payload 存本轮内全部消息的 JSON 数组,按追加顺序排列。
type ThreadMessage struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	ThreadID  string    `gorm:"column:thread_id;type:varchar(64);not null;uniqueIndex:idx_thread_run" json:"threadId"`
	RunID     string    `gorm:"column:run_id;type:varchar(64);not null;default:'';uniqueIndex:idx_thread_run" json:"runId"`
	Payload   []byte    `gorm:"column:payload;type:jsonb;not null" json:"payload"` // 本轮全部消息的 JSON 数组([msg1,msg2,...]);用 []byte 以便 pgx 正确绑定 jsonb
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
}

// TableName 显式指定表名。
func (ThreadMessage) TableName() string {
	return "thread_message"
}
