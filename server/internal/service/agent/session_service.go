package agent

import (
	"context"
	"errors"

	agui "github.com/HandsomeNPC/agui-go-sdk"
	"github.com/cloudwego/eino/compose"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/HandsomeNPC/EinoKit/internal/database"
	"github.com/HandsomeNPC/EinoKit/internal/loop"
	"github.com/HandsomeNPC/EinoKit/internal/model"
	"github.com/HandsomeNPC/EinoKit/internal/repository"
)

// messageKindAgentic 本项目消息表示固定为 agentic（见 loop 包文档与 schema.AgenticMessage）。
const messageKindAgentic = "agentic"

// SessionService 负责会话的创建、查询与一轮聊天的编排。
// store 由外部注入（main 启动时通过 NewSessionService 传入），便于测试替换。
type SessionService struct {
	store compose.CheckPointStore
}

// NewSessionService 构造一个注入了 checkpoint store 的 SessionService。
func NewSessionService(store compose.CheckPointStore) *SessionService {
	return &SessionService{store: store}
}

// SessionSvcApp 包级单例，供 handler 引用；由 main 在 redis.Init 之后赋值。
var SessionSvcApp *SessionService

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

// Chat 处理一条新的用户消息，流式推送 AG-UI 事件。
func (s *SessionService) Chat(c *gin.Context, em *agui.Emitter, sessionID, prompt string) (loop.TurnResult, error) {
	deps, err := s.deps(c.Request.Context())
	if err != nil {
		return loop.TurnResult{}, err
	}
	return loop.RunChat(c, em, deps, sessionID, prompt)
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
		Store: s.store,
		Repo:  s.repo(),
	}, nil
}

func (s *SessionService) repo() *repository.SessionRepository {
	return repository.NewSessionRepository(database.GetInstance())
}
