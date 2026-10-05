package agent

import (
	"context"
	"errors"

	agui "github.com/HandsomeNPC/agui-go-sdk"
	"github.com/cloudwego/eino/compose"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/HandsomeNPC/EinoKit/internal/loop"
	"github.com/HandsomeNPC/EinoKit/internal/memory/thread"
)

// messageKindAgentic 本项目消息表示固定为 agentic（见 loop 包文档与 schema.AgenticMessage）。
const messageKindAgentic = "agentic"

// ThreadService 负责会话的创建、查询与一轮聊天的编排。
// store 由外部注入（main 启动时通过 NewThreadService 传入），便于测试替换。
type ThreadService struct {
	store compose.CheckPointStore
}

// NewThreadService 构造一个注入了 checkpoint store 的 ThreadService。
func NewThreadService(store compose.CheckPointStore) *ThreadService {
	return &ThreadService{store: store}
}

// ThreadSvcApp 包级单例，供 handler 引用；由 main 在 redis.Init 之后赋值。
var ThreadSvcApp *ThreadService

// Create 创建一个新会话，返回新生成的会话 ID。
//
// 不接收 HTTP 请求 context：会话写入走 thread 包函数默认的
// context.Background()，脱离请求生命周期，避免客户端断开导致写入半途而废。
func (s *ThreadService) Create() (string, error) {
	id := uuid.New().String()
	if err := thread.Insert(&thread.ThreadInfo{
		ID:          id,
		MessageKind: messageKindAgentic,
	}); err != nil {
		return "", err
	}
	return id, nil
}

// List 列出全部会话
func (s *ThreadService) List() ([]*thread.ThreadInfo, error) {
	rows, err := thread.SelectList()
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []*thread.ThreadInfo{}
	}
	return rows, nil
}

// Exists 判断会话是否存在。
func (s *ThreadService) Exists(id string) (bool, error) {
	if _, err := thread.SelectByID(id); err != nil {
		if errors.Is(err, thread.ErrThreadNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Chat 处理一条新的用户消息，流式推送 AG-UI 事件。
// runID 为本轮 AG-UI run 的 ID（由 handler 通过 events.GenerateRunID() 生成），
// 会落到 thread_message.run_id，标记本轮产生的所有消息。
func (s *ThreadService) Chat(c *gin.Context, em *agui.Emitter, threadID, runID, prompt string) (loop.TurnResult, error) {
	deps, err := s.deps(c.Request.Context())
	if err != nil {
		return loop.TurnResult{}, err
	}
	deps.RunID = runID
	return loop.RunChat(c, em, deps, threadID, prompt)
}

// Abort 立即终止会话正在运行的轮次，返回是否确有轮次被终止。
func (s *ThreadService) Abort(threadID string) bool {
	return loop.StopThread(threadID)
}

// Delete 停掉会话的运行并删除其全部数据。
func (s *ThreadService) Delete(id string) error {
	loop.DropThread(id)
	return thread.DeleteByID(id)
}

// deps 组装 loop 层运行一轮所需的依赖。
// Agent 由本包 Default 工厂按配置构建；DB 不在此传递——loop 内部需要时直接
// 调 thread 包函数（后者经注入的 getter 取连接）。
func (s *ThreadService) deps(ctx context.Context) (loop.Deps, error) {
	a, err := Default(ctx)
	if err != nil {
		return loop.Deps{}, err
	}
	return loop.Deps{
		Agent: a,
		Store: s.store,
	}, nil
}
