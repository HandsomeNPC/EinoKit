package agent

import (
	"context"

	"github.com/google/uuid"

	"github.com/HandsomeNPC/EinoKit/internal/database"
	"github.com/HandsomeNPC/EinoKit/internal/model"
	"github.com/HandsomeNPC/EinoKit/internal/repository"
)

type SessionService struct{}

// SessionSvcApp 包级单例，供 handler 引用。
var SessionSvcApp = new(SessionService)

// Create 创建一个新会话，返回新生成的会话 ID。
func (s *SessionService) Create(ctx context.Context) (string, error) {
	id := uuid.New().String()
	if err := repository.NewSessionRepository(database.DB()).
		Insert(ctx, &model.Session{ID: id}); err != nil {
		return "", err
	}
	return id, nil
}

// List 列出全部会话
func (s *SessionService) List(ctx context.Context) ([]*model.Session, error) {
	rows, err := repository.NewSessionRepository(database.DB()).SelectList(ctx)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []*model.Session{}
	}
	return rows, nil
}
