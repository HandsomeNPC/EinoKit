package thread

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/HandsomeNPC/EinoKit/internal/database"
)

// ErrThreadNotFound 会话主键不存在时返回的哨兵错误。
var ErrThreadNotFound = errors.New("thread: 会话不存在")

// Insert 创建一个会话。主键 id 须由调用方预先填好。
func Insert(t *ThreadInfo) error {
	if t == nil {
		return fmt.Errorf("thread: 会话为空")
	}
	if t.ThreadID == "" {
		return fmt.Errorf("thread: 会话主键为空")
	}
	if err := database.GetInstance().WithContext(context.Background()).Create(t).Error; err != nil {
		return fmt.Errorf("thread: 插入会话 id=%s 失败: %w", t.ThreadID, err)
	}
	return nil
}

// SelectByID 按主键查会话，不存在时返回 ErrThreadNotFound。
func SelectByID(id string) (*ThreadInfo, error) {
	if id == "" {
		return nil, ErrThreadNotFound
	}

	var t ThreadInfo
	err := database.GetInstance().WithContext(context.Background()).Where("thread_id = ?", id).First(&t).Error
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
		res := tx.Where("thread_id = ?", id).Delete(&ThreadInfo{})
		if res.Error != nil {
			return fmt.Errorf("thread: 删除会话 id=%s 失败: %w", id, res.Error)
		}
		return nil
	})
	return err
}

// AppendMessage 把一条消息追加到指定 (threadID, runID) 的 payload 中。
// payload 表示一轮对话(run)中全部消息的 JSON 数组:
//   - 若 (threadID, runID) 行不存在,新建一行,payload = [payload];
//   - 若已存在,把 payload 作为新元素追加到现有数组末尾。
//
// payload 参数为已序列化的单条消息 JSON;调用方负责 marshal,
// 本包不感知消息泛型类型。整个读-改-写在单事务内完成并加行锁,
// 保证同一 (threadID, runID) 的并发追加不会丢消息。
func AppendMessage(threadID, runID, payload string) error {
	if threadID == "" {
		return fmt.Errorf("thread: 会话主键为空")
	}
	if runID == "" {
		return fmt.Errorf("thread: runID 为空")
	}
	if payload == "" {
		return fmt.Errorf("thread: 消息内容为空")
	}

	err := database.GetInstance().WithContext(context.Background()).Transaction(func(tx *gorm.DB) error {
		// SELECT ... FOR UPDATE 锁住同一 (thread_id, run_id) 行,
		// 使并发追加串行化;行不存在时返回 ErrRecordNotFound。
		var existing ThreadMessage
		findErr := tx.Where("thread_id = ? AND run_id = ?", threadID, runID).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&existing).Error

		switch {
		case findErr == nil:
			// 行已存在 —— 解析现有数组,追加新消息,写回。
			var msgs []json.RawMessage
			if len(existing.Payload) > 0 {
				if uErr := json.Unmarshal(existing.Payload, &msgs); uErr != nil {
					return fmt.Errorf("thread: 解析 payload 失败 id=%s run=%s: %w", threadID, runID, uErr)
				}
			}
			msgs = append(msgs, json.RawMessage(payload))
			newPayload, mErr := json.Marshal(msgs)
			if mErr != nil {
				return fmt.Errorf("thread: 序列化 payload 失败 id=%s run=%s: %w", threadID, runID, mErr)
			}
			return tx.Model(&ThreadMessage{}).
				Where("id = ?", existing.ID).
				Update("payload", newPayload).Error

		case errors.Is(findErr, gorm.ErrRecordNotFound):
			// 行不存在 —— 新建,首条消息即为数组唯一元素。
			arr, mErr := json.Marshal([]json.RawMessage{json.RawMessage(payload)})
			if mErr != nil {
				return fmt.Errorf("thread: 序列化 payload 失败 id=%s run=%s: %w", threadID, runID, mErr)
			}
			return tx.Create(&ThreadMessage{
				ThreadID: threadID,
				RunID:    runID,
				Payload:  arr,
			}).Error

		default:
			return fmt.Errorf("thread: 查询消息 id=%s run=%s 失败: %w", threadID, runID, findErr)
		}
	})
	if err != nil {
		return fmt.Errorf("thread: 追加会话消息 id=%s run=%s 失败: %w", threadID, runID, err)
	}
	return nil
}

// SelectMessages 按 run 追加顺序取出会话全部消息行,对标 mem.Session.GetMessages。
// 每行的 Payload 是该 run 内全部消息的 JSON 数组,调用方需按数组解码再展开。
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
