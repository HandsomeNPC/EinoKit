package agents

import (
	"context"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// builders 是 Agent 名到构造函数的注册表。这里只登记构造函数而不是实例：
// 构造 Agent 要读 config（模型 APIKey 等），而包级变量的初始化发生在 main
// 执行 config.Load 之前，直接建实例会在进程启动时 panic。
// 后续新增 Agent 时追加一行 AgentNameXxx: NewXxxAgent 即可。
var builders = map[string]func(context.Context) adk.TypedAgent[*schema.AgenticMessage]{
	AgentNameDemo: NewDemoAgent,
}

// 已构建实例的缓存。Agent 内部持有模型客户端，按名复用一份即可。
var (
	mu       sync.Mutex
	instance = map[string]adk.TypedAgent[*schema.AgenticMessage]{}
)

// GetAgent 按名字获取 Agent 实例，首次调用时惰性构建并缓存。
// 未注册的 name 返回错误；构造失败（如配置缺失）同样返回错误而不是 nil Agent。
func GetAgent(name string) (adk.TypedAgent[*schema.AgenticMessage], error) {
	mu.Lock()
	defer mu.Unlock()

	if a, ok := instance[name]; ok {
		return a, nil
	}

	build, ok := builders[name]
	if !ok {
		return nil, fmt.Errorf("agents: 未注册的 agent: %q", name)
	}

	// NewXxxAgent 构造失败时返回 nil（只打日志），这里转成错误往上报，
	// 避免把 nil Agent 交给 TurnLoop 后在运行期空指针。
	a := build(context.Background())
	if a == nil {
		return nil, fmt.Errorf("agents: 构建 agent %q 失败，请检查模型配置", name)
	}
	instance[name] = a
	return a, nil
}

// IsRegistered 报告 name 是否为已登记的 Agent，不触发构建。
// 用于创建会话时校验 agentType，避免为了校验名字就去连模型。
func IsRegistered(name string) bool {
	_, ok := builders[name]
	return ok
}
