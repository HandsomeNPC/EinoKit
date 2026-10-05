package thread

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/HandsomeNPC/EinoKit/internal/database"
)

// ErrThreadNotFound 会话主键不存在时返回的哨兵错误。
var ErrThreadNotFound = errors.New("thread: 会话不存在")

// Insert 创建一个会话。主键 id 须由调用方预先填好。
func Insert(t *ThreadInfo) error {
	if t == nil {
		return fmt.Errorf("thread: 会话为空")
	}
	if t.ID == "" {
		return fmt.Errorf("thread: 会话主键为空")
	}
	if err := database.GetInstance().WithContext(context.Background()).Create(t).Error; err != nil {
		return fmt.Errorf("thread: 插入会话 id=%s 失败: %w", t.ID, err)
	}
	return nil
}

// SelectByID 按主键查会话，不存在时返回 ErrThreadNotFound。
func SelectByID(id string) (*ThreadInfo, error) {
	if id == "" {
		return nil, ErrThreadNotFound
	}

	var t ThreadInfo
	err := database.GetInstance().WithContext(context.Background()).Where("id = ?", id).First(&t).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrThreadNotFound
		}
		return nil, fmt.Errorf("thread: 查询会话 id=%s 失败: %w", id, err)
	}
	return &t, nil
}

// SelectList 列出全部会话（按创建时间降序），对标 mem.Store.List。
// thread 行即头部状态，无重列，故直接整行读，无需投影。
func SelectList() ([]*ThreadInfo, error) {
	var rows []*ThreadInfo
	err := database.GetInstance().WithContext(context.Background()).Model(&ThreadInfo{}).
		Order("created_at DESC").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("thread: 查询会话列表失败: %w", err)
	}
	return rows, nil
}

// DeleteByID 删除会话及其全部消息，两步同事务以免留下孤儿消息。
// 表无外键约束（迁移期禁用），故级联删除由本方法显式完成。
func DeleteByID(id string) error {
	if id == "" {
		return fmt.Errorf("thread: 会话主键为空")
	}

	err := database.GetInstance().WithContext(context.Background()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("thread_id = ?", id).Delete(&ThreadMessage{}).Error; err != nil {
			return fmt.Errorf("thread: 删除会话消息 id=%s 失败: %w", id, err)
		}
		res := tx.Where("id = ?", id).Delete(&ThreadInfo{})
		if res.Error != nil {
			return fmt.Errorf("thread: 删除会话 id=%s 失败: %w", id, res.Error)
		}
		return nil
	})
	return err
}

// AppendMessage 追加一条消息到会话，对标 mem.Session.Append。
// threadID 标识所属会话，runID 标识产生该消息的那一轮对话（AG-UI run 概念）。
// payload 为已序列化的消息 JSON；调用方负责 marshal，本包不感知消息泛型类型。
func AppendMessage(threadID, runID, payload string) error {
	if threadID == "" {
		return fmt.Errorf("thread: 会话主键为空")
	}
	if payload == "" {
		return fmt.Errorf("thread: 消息内容为空")
	}

	m := &ThreadMessage{ThreadID: threadID, RunID: runID, Payload: []byte(payload)}
	if err := database.GetInstance().WithContext(context.Background()).Create(m).Error; err != nil {
		return fmt.Errorf("thread: 追加会话消息 id=%s 失败: %w", threadID, err)
	}
	return nil
}

// SelectMessages 按追加顺序取出会话全部消息，对标 mem.Session.GetMessages。
// 会话不存在时返回空切片与 nil，由调用方按需比对。
func SelectMessages(threadID string) ([]*ThreadMessage, error) {
	if threadID == "" {
		return nil, nil
	}

	var rows []*ThreadMessage
	err := database.GetInstance().WithContext(context.Background()).
		Where("thread_id = ?", threadID).
		Order("id").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("thread: 查询会话消息 id=%s 失败: %w", threadID, err)
	}
	return rows, nil
}

// UpdateMsgIdx 保存 A2UI 组件槽位计数，对标 mem.Session.SetMsgIdx。
// 同样走单列 Update：idx=0 是合法值，Updates(struct) 会漏写。
// 主键不存在时返回 ErrThreadNotFound。
//
// 注意：AG-UI 是追加式事件协议，没有组件槽位概念，本方法当前无调用方，
// 保留以备将来需要位置游标的渲染协议。
func UpdateMsgIdx(id string, idx int) error {
	if id == "" {
		return fmt.Errorf("thread: 会话主键为空")
	}

	res := database.GetInstance().WithContext(context.Background()).Model(&ThreadInfo{}).
		Where("id = ?", id).
		Update("msg_idx", idx)
	if res.Error != nil {
		return fmt.Errorf("thread: 更新会话槽位计数 id=%s 失败: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrThreadNotFound
	}
	return nil
}
