package turnloop

import (
	"context"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/HandsomeNPC/EinoKit/internal/agents"
	"github.com/HandsomeNPC/EinoKit/internal/agui"
	"github.com/HandsomeNPC/EinoKit/internal/checkpoint"
	"github.com/HandsomeNPC/EinoKit/internal/memory/thread"
)

// ThreadContext 是一次会话运行的线程上下文:携带 threadID 与本轮 SSE emitter。
type ThreadContext struct {
	ThreadID string
	Emitter  *agui.Emitter
}

// NewTurnLoop 构造一个会话的 loop,tc 携带的 threadID/emitter 透传给各回调。
func NewTurnLoop(tc ThreadContext) *adk.TurnLoop[*ChatItem, *schema.AgenticMessage] {
	cfg := adk.TurnLoopConfig[*ChatItem, *schema.AgenticMessage]{
		GenInput:      makeGenInput(tc),      //每轮开始时调用，决定"这一轮 Agent 看到什么"
		PrepareAgent:  makePrepareAgent(tc),  //每轮调用一次，返回本轮使用的 Agent
		OnAgentEvents: makeOnAgentEvents(tc), //接收Agent的事件流，负责渲染输出和持久化中间消息
		//以下三个字段用于声明式 checkpoint（审批恢复）
		GenResume:    makeGenResume(tc),
		Store:        checkpoint.GetInstance(),
		CheckpointID: tc.ThreadID,
	}
	return adk.NewTurnLoop(cfg)
}

// PrepareAgent：每轮调用一次，返回本轮使用的 Agent
// 根据 ThreadContext.ThreadID 查 ThreadInfo 拿到 AgentType，再从 registry 取对应 Agent。
func makePrepareAgent(tc ThreadContext) func(ctx context.Context, loop *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], consumed []*ChatItem) (adk.TypedAgent[*schema.AgenticMessage], error) {
	return func(ctx context.Context, loop *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], consumed []*ChatItem) (adk.TypedAgent[*schema.AgenticMessage], error) {
		info, err := thread.SelectByID(tc.ThreadID)
		if err != nil {
			return nil, err
		}
		return agents.GetAgent(info.AgentType)
	}
}
