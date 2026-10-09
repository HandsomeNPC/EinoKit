package thread

import (
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
)

// ThreadInfo 会话表
type ThreadInfo struct {
	ThreadID  string `gorm:"column:thread_id;type:varchar(64);primaryKey" json:"threadId"`
	Title     string `gorm:"column:title;type:varchar(255);not null;default:''" json:"title"`
	AgentType string `gorm:"column:agent_type;type:varchar(64);not null;default:''" json:"agentType"`

	// PendingInterrupt 是待确认中断的展示/回填副本，**不是权威状态**。
	//
	// 唯一权威是 Redis 里的 checkpoint——只有它能让 ADK 真的 resume。本列存在
	// 仅因为 checkpoint 读不出来：adk 的 serialization 结构未导出且是 gob 编码，
	// 拿不到 interrupt ID 与提示文案，而 /render 回填审批卡片、resume 回填
	// Targets 都需要它们。读取务必走 loadPendingInterrupts（先查 checkpoint，
	// 不在就一律当作无待确认中断），否则 checkpoint 过期后前端会显示一张
	// 点了就报错的卡片。
	//
	// 直接用 AG-UI 的 types.Interrupt：RUN_FINISHED 的 outcome、/render 的响应、
	// 前端的 AGUIInterrupt 三处同构，省掉一层转换。
	PendingInterrupt []types.Interrupt `gorm:"column:pending_interrupt;type:jsonb;serializer:json" json:"-"`

	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"createdAt"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updatedAt"`
}

// TableName 显式指定表名。
func (ThreadInfo) TableName() string {
	return "thread_info"
}
