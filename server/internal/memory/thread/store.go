package thread

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
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

// UpdateTitle 更新会话标题。
func UpdateTitle(id, title string) error {
	if id == "" {
		return fmt.Errorf("thread: 会话主键为空")
	}
	res := database.GetInstance().WithContext(context.Background()).Model(&ThreadInfo{}).
		Where("thread_id = ?", id).Update("title", title)
	if res.Error != nil {
		return fmt.Errorf("thread: 更新会话标题 id=%s 失败: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrThreadNotFound
	}
	return nil
}

// SavePendingInterrupt 记录会话待确认的中断快照。
//
// 整组覆盖写，绝不追加：工具可能二次中断并产生全新 interrupt ID，若追加，
// 前端会同时看到已失效的和仍有效的 ID，点哪个都可能报错。
//
// 这里显式 marshal 成 string 再写：把 []types.Interrupt 直接交给 Update 会被
// GORM 当作变参列表展开，单元素切片被拆成其首个元素，结果在列里存成单个
// JSON object 而不是数组——且不报错，读取时才在 Scan 阶段炸（实测）。
func SavePendingInterrupt(id string, items []types.Interrupt) error {
	if id == "" {
		return fmt.Errorf("thread: 会话主键为空")
	}
	if len(items) == 0 {
		return ClearPendingInterrupt(id)
	}
	payload, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("thread: 序列化待确认中断 id=%s 失败: %w", id, err)
	}
	res := database.GetInstance().WithContext(context.Background()).Model(&ThreadInfo{}).
		Where("thread_id = ?", id).Update("pending_interrupt", string(payload))
	if res.Error != nil {
		return fmt.Errorf("thread: 保存待确认中断 id=%s 失败: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrThreadNotFound
	}
	return nil
}

// ClearPendingInterrupt 清掉会话的待确认中断快照。
//
// 会话已被删除时返回 nil 而不是 ErrThreadNotFound：清理是幂等操作，
// 调用方（run 收尾、abort）只想保证"最终没有残留"，不关心行还在不在。
func ClearPendingInterrupt(id string) error {
	if id == "" {
		return fmt.Errorf("thread: 会话主键为空")
	}
	err := database.GetInstance().WithContext(context.Background()).Model(&ThreadInfo{}).
		Where("thread_id = ?", id).Update("pending_interrupt", nil).Error
	if err != nil {
		return fmt.Errorf("thread: 清除待确认中断 id=%s 失败: %w", id, err)
	}
	return nil
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
func AppendMessage(threadID, runID, payload string) error {
	return AppendMessages(threadID, runID, []string{payload})
}

// AppendMessages 把一批消息一次性追加到指定 (threadID, runID) 的 payload 中。
func AppendMessages(threadID, runID string, payloads []string) error {
	if threadID == "" {
		return fmt.Errorf("thread: 会话主键为空")
	}
	if runID == "" {
		return fmt.Errorf("thread: runID 为空")
	}
	if len(payloads) == 0 {
		return nil
	}
	for i, p := range payloads {
		if p == "" {
			return fmt.Errorf("thread: 第 %d 条消息内容为空", i)
		}
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
			// 行已存在 —— 解析现有数组,追加整批新消息,写回一次。
			var msgs []json.RawMessage
			if len(existing.Payload) > 0 {
				if uErr := json.Unmarshal(existing.Payload, &msgs); uErr != nil {
					return fmt.Errorf("thread: 解析 payload 失败 id=%s run=%s: %w", threadID, runID, uErr)
				}
			}
			for _, p := range payloads {
				msgs = append(msgs, json.RawMessage(p))
			}
			newPayload, mErr := json.Marshal(msgs)
			if mErr != nil {
				return fmt.Errorf("thread: 序列化 payload 失败 id=%s run=%s: %w", threadID, runID, mErr)
			}
			return tx.Model(&ThreadMessage{}).
				Where("id = ?", existing.ID).
				Update("payload", newPayload).Error

		case errors.Is(findErr, gorm.ErrRecordNotFound):
			// 行不存在 —— 新建,整批消息作为数组初始元素。
			arr := make([]json.RawMessage, 0, len(payloads))
			for _, p := range payloads {
				arr = append(arr, json.RawMessage(p))
			}
			data, mErr := json.Marshal(arr)
			if mErr != nil {
				return fmt.Errorf("thread: 序列化 payload 失败 id=%s run=%s: %w", threadID, runID, mErr)
			}
			return tx.Create(&ThreadMessage{
				ThreadID: threadID,
				RunID:    runID,
				Payload:  data,
			}).Error

		default:
			return fmt.Errorf("thread: 查询消息 id=%s run=%s 失败: %w", threadID, runID, findErr)
		}
	})
	if err != nil {
		return fmt.Errorf("thread: 批量追加会话消息 id=%s run=%s 失败: %w", threadID, runID, err)
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
