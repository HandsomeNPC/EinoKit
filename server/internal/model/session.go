// Package model 定义数据库实体，对应 GORM 映射的表结构。
package model

import "time"

// Session 会话表（session），持久化单个对话的头部状态。
type Session struct {
	ID                 string    `gorm:"column:id;primaryKey" json:"id"`
	Title              string    `gorm:"column:title" json:"title"`
	MessageKind        string    `gorm:"column:message_kind" json:"messageKind"`                // plain/agentic，对标 msgops.Kind
	PendingInterruptID string    `gorm:"column:pending_interrupt_id" json:"pendingInterruptId"` // 非空表示 agent 正等待人工审批
	MsgIdx             int       `gorm:"column:msg_idx" json:"msgIdx"`                          // 中断点的 A2UI 组件槽位计数
	CreatedAt          time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt          time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

// TableName 显式指定表名。
func (Session) TableName() string {
	return "session"
}
