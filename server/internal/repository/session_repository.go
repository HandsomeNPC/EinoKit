// Package repository 数据访问层，封装 GORM 查询与错误归一。
package repository

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/HandsomeNPC/EinoKit/internal/model"
)

// ErrSessionNotFound 会话主键不存在时返回的哨兵错误。
var ErrSessionNotFound = errors.New("repository: 会话不存在")

// SessionRepository 会话数据访问（session / session_message）。
//
// 对标 eino-examples chatwitheino/mem.Store：原 JSONL 文件存储拆为两张表——
// 会话头部状态走 session，逐条消息走 session_message。所有写操作的零值字段
// （如清空中断 ID、msg_idx 归零）走单列 Update，避免 Updates(struct) 跳过零值。
type SessionRepository struct {
	db *gorm.DB
}

// NewSessionRepository 构造会话 repository。
func NewSessionRepository(db *gorm.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

// Insert 创建一个会话。主键 id 须由调用方预先填好。
func (r *SessionRepository) Insert(ctx context.Context, s *model.Session) error {
	if s == nil {
		return fmt.Errorf("repository: 会话为空")
	}
	if s.ID == "" {
		return fmt.Errorf("repository: 会话主键为空")
	}
	if err := r.db.WithContext(ctx).Create(s).Error; err != nil {
		return fmt.Errorf("repository: 插入会话 id=%s 失败: %w", s.ID, err)
	}
	return nil
}

// SelectByID 按主键查会话，不存在时返回 ErrSessionNotFound。
func (r *SessionRepository) SelectByID(ctx context.Context, id string) (*model.Session, error) {
	if id == "" {
		return nil, ErrSessionNotFound
	}

	var s model.Session
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&s).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("repository: 查询会话 id=%s 失败: %w", id, err)
	}
	return &s, nil
}

// SelectList 列出全部会话（按创建时间降序），对标 mem.Store.List。
// session 行即头部状态，无重列，故直接整行读，无需投影。
func (r *SessionRepository) SelectList(ctx context.Context) ([]*model.Session, error) {
	var rows []*model.Session
	err := r.db.WithContext(ctx).Model(&model.Session{}).
		Order("created_at DESC").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("repository: 查询会话列表失败: %w", err)
	}
	return rows, nil
}

// DeleteByID 删除会话及其全部消息，两步同事务以免留下孤儿消息。
// 表无外键约束（迁移期禁用），故级联删除由本方法显式完成。
func (r *SessionRepository) DeleteByID(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("repository: 会话主键为空")
	}

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("session_id = ?", id).Delete(&model.SessionMessage{}).Error; err != nil {
			return fmt.Errorf("repository: 删除会话消息 id=%s 失败: %w", id, err)
		}
		res := tx.Where("id = ?", id).Delete(&model.Session{})
		if res.Error != nil {
			return fmt.Errorf("repository: 删除会话 id=%s 失败: %w", id, res.Error)
		}
		return nil
	})
	return err
}

// AppendMessage 追加一条消息到会话，对标 mem.Session.Append。
// payload 为已序列化的消息 JSON；调用方负责 marshal，repository 不感知消息泛型类型。
func (r *SessionRepository) AppendMessage(ctx context.Context, sessionID, payload string) error {
	if sessionID == "" {
		return fmt.Errorf("repository: 会话主键为空")
	}
	if payload == "" {
		return fmt.Errorf("repository: 消息内容为空")
	}

	m := &model.SessionMessage{SessionID: sessionID, Payload: payload}
	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		return fmt.Errorf("repository: 追加会话消息 id=%s 失败: %w", sessionID, err)
	}
	return nil
}

// SelectMessages 按追加顺序取出会话全部消息，对标 mem.Session.GetMessages。
// session 不存在时返回空切片与 nil，由调用方按需比对。
func (r *SessionRepository) SelectMessages(ctx context.Context, sessionID string) ([]*model.SessionMessage, error) {
	if sessionID == "" {
		return nil, nil
	}

	var rows []*model.SessionMessage
	err := r.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("id").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("repository: 查询会话消息 id=%s 失败: %w", sessionID, err)
	}
	return rows, nil
}

// UpdatePendingInterruptID 保存/清空中断 ID，对标 mem.Session.SetPendingInterruptID。
// 传空串即清空，故走单列 Update——Updates(struct) 会跳过零值，让清空写不进库。
// 主键不存在时返回 ErrSessionNotFound。
func (r *SessionRepository) UpdatePendingInterruptID(ctx context.Context, id, interruptID string) error {
	if id == "" {
		return fmt.Errorf("repository: 会话主键为空")
	}

	res := r.db.WithContext(ctx).Model(&model.Session{}).
		Where("id = ?", id).
		Update("pending_interrupt_id", interruptID)
	if res.Error != nil {
		return fmt.Errorf("repository: 更新会话中断 ID id=%s 失败: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// UpdateMsgIdx 保存 A2UI 组件槽位计数，对标 mem.Session.SetMsgIdx。
// 同样走单列 Update：idx=0 是合法值，Updates(struct) 会漏写。
// 主键不存在时返回 ErrSessionNotFound。
func (r *SessionRepository) UpdateMsgIdx(ctx context.Context, id string, idx int) error {
	if id == "" {
		return fmt.Errorf("repository: 会话主键为空")
	}

	res := r.db.WithContext(ctx).Model(&model.Session{}).
		Where("id = ?", id).
		Update("msg_idx", idx)
	if res.Error != nil {
		return fmt.Errorf("repository: 更新会话槽位计数 id=%s 失败: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrSessionNotFound
	}
	return nil
}
