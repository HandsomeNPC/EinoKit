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

func NewTurnLoop(threadID, runID string, emitter *agui.Emitter) *adk.TurnLoop[*ChatItem, *schema.AgenticMessage] {
	cfg := adk.TurnLoopConfig[*ChatItem, *schema.AgenticMessage]{
		GenInput:      makeGenInput(threadID, runID), //每轮开始时调用，决定"这一轮 Agent 看到什么"
		PrepareAgent:  makePrepareAgent(threadID),    //每轮调用一次，返回本轮使用的 Agent
		OnAgentEvents: makeOnAgentEvents(emitter),    //接收Agent的事件流，负责渲染输出和持久化中间消息
		//以下三个字段用于声明式 checkpoint（审批恢复）
		GenResume:    makeGenResume(),
		Store:        checkpoint.GetInstance(),
		CheckpointID: threadID,
	}
	return adk.NewTurnLoop(cfg)
}

// PrepareAgent：每轮调用一次，返回本轮使用的 Agent
// 根据 threadID 查 ThreadInfo 拿到 AgentType，再从 registry 取对应 Agent。
func makePrepareAgent(threadID string) func(ctx context.Context, loop *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], consumed []*ChatItem) (adk.TypedAgent[*schema.AgenticMessage], error) {
	return func(ctx context.Context, loop *adk.TurnLoop[*ChatItem, *schema.AgenticMessage], consumed []*ChatItem) (adk.TypedAgent[*schema.AgenticMessage], error) {
		_ = consumed

		info, err := thread.SelectByID(threadID)
		if err != nil {
			return nil, err
		}
		return agents.GetAgent(info.AgentType)
	}
}

// OnAgentEvents 与 GenResume 见 on_agent_events.go、gen_resume.go。
