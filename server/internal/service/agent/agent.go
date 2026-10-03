package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// instruction 是 Agent 的系统提示词。
const instruction = `你是 EinoKit 的奶茶点单助手。
用户想买奶茶时，先把口味、杯型、甜度、杯数问清楚，然后调用 order_milk_tea 下单。
工具返回后，用一句话向用户复述订单号、金额和预计时间。不要编造订单信息。
其他问题正常聊天回答即可。`

var (
	agentOnce sync.Once
	agentInst adk.TypedAgent[*schema.AgenticMessage]
	agentErr  error
)

// Default 懒构建并返回进程级唯一的 Agent。
//
// 模型构造会建 HTTP client、解析工具 schema，不能每个请求做一次。
// 注意 sync.Once 会把首次失败（如 API Key 配错）缓存到进程结束。
func Default(ctx context.Context) (adk.TypedAgent[*schema.AgenticMessage], error) {
	agentOnce.Do(func() {
		agentInst, agentErr = build(ctx)
	})
	return agentInst, agentErr
}

// build 组装 Agent：一个 agentic 模型 + 一个下单工具。
//
// 用 ChatModelAgent 而非 deep agent：后者会拖进 LocalBackend（文件系统 + shell，
// shell 在 Windows 上不可用）、planner 与子 agent，买奶茶一个都不需要。
func build(ctx context.Context) (adk.TypedAgent[*schema.AgenticMessage], error) {
	cm, err := newAgenticModel(ctx)
	if err != nil {
		return nil, fmt.Errorf("agent: 初始化模型失败: %w", err)
	}

	orderTool, err := newOrderMilkTeaTool()
	if err != nil {
		return nil, fmt.Errorf("agent: 初始化下单工具失败: %w", err)
	}

	a, err := adk.NewTypedChatModelAgent(ctx, &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
		Name:        "EinoKitMilkTeaAgent",
		Description: "奶茶点单助手。",
		Instruction: instruction,
		Model:       cm,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: []tool.BaseTool{orderTool},
			},
		},
		MaxIterations: 20,
	})
	if err != nil {
		return nil, fmt.Errorf("agent: 初始化 agent 失败: %w", err)
	}
	return a, nil
}
