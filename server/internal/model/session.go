// Package model 定义数据库实体，对应 GORM 映射的表结构。
package model

import "time"

// Session 会话表（session），持久化单个对话的头部状态。
//
// 字符串列一律 not null + 默认空串：读取时直接扫进非指针 string，
// 留 NULL 会让 GORM 扫描报错。
type Session struct {
	ID                 string    `gorm:"column:id;type:varchar(64);primaryKey" json:"id"`
	Title              string    `gorm:"column:title;type:varchar(255);not null;default:''" json:"title"`
	MessageKind        string    `gorm:"column:message_kind;type:varchar(32);not null;default:''" json:"messageKind"`         // 固定 agentic，见 schema.AgenticMessage
	PendingInterruptID string    `gorm:"column:pending_interrupt_id;type:text;not null;default:''" json:"pendingInterruptId"` // 非空表示 agent 正等待人工审批
	PendingApproval    string    `gorm:"column:pending_approval;type:text;not null;default:''" json:"pendingApproval"`        // 待审批上下文 JSON，见 loop.PendingApproval
	MsgIdx             int       `gorm:"column:msg_idx;not null;default:0" json:"msgIdx"`                                     // 原 A2UI 组件槽位计数；AG-UI 下不再使用，保留列备用
	CreatedAt          time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt          time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

// TableName 显式指定表名。
func (Session) TableName() string {
	return "session"
}
