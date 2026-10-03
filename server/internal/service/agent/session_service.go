package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	agui "github.com/HandsomeNPC/agui-go-sdk"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/HandsomeNPC/EinoKit/internal/checkpoint"
	"github.com/HandsomeNPC/EinoKit/internal/database"
	"github.com/HandsomeNPC/EinoKit/internal/loop"
	"github.com/HandsomeNPC/EinoKit/internal/model"
	"github.com/HandsomeNPC/EinoKit/internal/repository"
)

// messageKindAgentic 本项目消息表示固定为 agentic（见 loop 包文档与 schema.AgenticMessage）。
const messageKindAgentic = "agentic"

type SessionService struct{}

// SessionSvcApp 包级单例，供 handler 引用。
var SessionSvcApp = new(SessionService)

// Create 创建一个新会话，返回新生成的会话 ID。
func (s *SessionService) Create(ctx context.Context) (string, error) {
	id := uuid.New().String()
	if err := s.repo().Insert(ctx, &model.Session{
		ID:          id,
		MessageKind: messageKindAgentic,
	}); err != nil {
		return "", err
	}
	return id, nil
}

// List 列出全部会话
func (s *SessionService) List(ctx context.Context) ([]*model.Session, error) {
	rows, err := s.repo().SelectList(ctx)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []*model.Session{}
	}
	return rows, nil
}

// Exists 判断会话是否存在。
func (s *SessionService) Exists(ctx context.Context, id string) (bool, error) {
	if _, err := s.repo().SelectByID(ctx, id); err != nil {
		if errors.Is(err, repository.ErrSessionNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// PendingApproval 返回会话当前待审批的上下文；没有待审批时返回 nil。
func (s *SessionService) PendingApproval(ctx context.Context, id string) (*loop.PendingApproval, error) {
	sess, err := s.repo().SelectByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if sess.PendingInterruptID == "" {
		return nil, nil
	}

	var pending loop.PendingApproval
	if sess.PendingApproval != "" {
		if err := json.Unmarshal([]byte(sess.PendingApproval), &pending); err != nil {
			return nil, fmt.Errorf("service: 解析待审批上下文失败: %w", err)
		}
	}
	// 以库里的中断 ID 为准，不信任 pending_approval 里可能过期的值。
	pending.InterruptID = sess.PendingInterruptID
	return &pending, nil
}

// Chat 处理一条新的用户消息，流式推送 AG-UI 事件。
func (s *SessionService) Chat(c *gin.Context, em *agui.Emitter, sessionID, prompt string) (loop.TurnResult, error) {
	deps, err := s.deps(c.Request.Context())
	if err != nil {
		return loop.TurnResult{}, err
	}
	return loop.RunChat(c, em, deps, sessionID, prompt)
}

// Resume 用审批结果恢复被中断的运行，流式推送 AG-UI 事件。
func (s *SessionService) Resume(c *gin.Context, em *agui.Emitter, sessionID string,
	pending *loop.PendingApproval, result *loop.ApprovalResult,
) (loop.TurnResult, error) {
	deps, err := s.deps(c.Request.Context())
	if err != nil {
		return loop.TurnResult{}, err
	}
	return loop.RunResume(c, em, deps, sessionID, pending, result)
}

// Abort 立即终止会话正在运行的轮次，返回是否确有轮次被终止。
func (s *SessionService) Abort(sessionID string) bool {
	return loop.StopSession(sessionID)
}

// Delete 停掉会话的运行并删除其全部数据。
func (s *SessionService) Delete(ctx context.Context, id string) error {
	loop.DropSession(id)
	return s.repo().DeleteByID(ctx, id)
}

// deps 组装 loop 层运行一轮所需的依赖。
func (s *SessionService) deps(ctx context.Context) (loop.Deps, error) {
	a, err := Default(ctx)
	if err != nil {
		return loop.Deps{}, err
	}
	return loop.Deps{
		Agent: a,
		Store: checkpoint.Default(),
		Repo:  s.repo(),
	}, nil
}

func (s *SessionService) repo() *repository.SessionRepository {
	return repository.NewSessionRepository(database.DB())
}
