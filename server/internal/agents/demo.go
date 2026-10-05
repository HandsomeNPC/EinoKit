package agents

import (
	"context"
	"log"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/HandsomeNPC/EinoKit/internal/chatmodel"
	"github.com/HandsomeNPC/EinoKit/internal/tools"
)

// AgentNameDemo 是 demo Agent 的注册名。
const AgentNameDemo = "demo"

// instruction 是 Agent 的系统提示词。
const instruction = `你是 EinoKit 的奶茶点单助手。
用户想买奶茶时，先把口味、杯型、甜度、杯数问清楚，然后调用 order_milk_tea 下单。
工具返回后，用一句话向用户复述订单号、金额和预计时间。不要编造订单信息。
其他问题正常聊天回答即可。`

// NewDemoAgent 组装奶茶点单 Agent：一个 agentic 模型 + 一个下单工具。
//
// 构造失败只输出错误日志并返回 nil，不拖崩服务。
func NewDemoAgent(ctx context.Context) adk.TypedAgent[*schema.AgenticMessage] {
	cm := chatmodel.NewAgenticModel(ctx)

	orderTool := tools.NewOrderMilkTeaTool()

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
		log.Printf("[agents] 初始化 demo agent 失败: %v", err)
		return nil
	}
	return a
}
