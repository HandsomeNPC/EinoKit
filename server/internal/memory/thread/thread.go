package thread

import "time"

// ThreadInfo 会话表
type ThreadInfo struct {
	ID        string    `gorm:"column:id;type:varchar(64);primaryKey" json:"id"`
	Title     string    `gorm:"column:title;type:varchar(255);not null;default:''" json:"title"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

// TableName 显式指定表名。
func (ThreadInfo) TableName() string {
	return "thread_info"
}
