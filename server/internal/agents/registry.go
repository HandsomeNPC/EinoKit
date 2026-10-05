package agents

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// registry 是 Agent 名到已构建实例的进程级注册表。
// 后续新增 Agent 时追加一行 AgentNameXxx: NewXxxAgent(context.Background()) 即可。
var registry = map[string]adk.TypedAgent[*schema.AgenticMessage]{
	AgentNameDemo: NewDemoAgent(context.Background()),
}

// GetAgent 按名字获取已构建的 Agent 实例。未注册的 name 返回错误。
func GetAgent(name string) (adk.TypedAgent[*schema.AgenticMessage], error) {
	a, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("agents: 未注册的 agent: %q", name)
	}
	return a, nil
}
