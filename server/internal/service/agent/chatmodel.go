// Package agent 构建并持有进程级唯一的对话 Agent。
package agent

import (
	"context"

	"github.com/cloudwego/eino-ext/components/model/agenticopenai"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/openai/openai-go/v3/responses"

	"github.com/HandsomeNPC/EinoKit/internal/config"
)

// newAgenticModel 按配置构造 agentic 模型（Responses API）。
//
// 请求 ReasoningEncryptedContent 是为了让思考内容能随历史回放；
// 相应地落库时必须保留 content block 上的 item ID（见 loop 包的 normalizeForSession）。
func newAgenticModel(ctx context.Context) (einomodel.AgenticModel, error) {
	cfg := config.Get().ChatModel
	timeout := cfg.Timeout()
	return agenticopenai.NewResponsesModel(ctx, &agenticopenai.ResponsesConfig{
		APIKey:  cfg.APIKey,
		Model:   cfg.Model,
		BaseURL: cfg.BaseURL,
		ByAzure: cfg.ByAzure,
		Timeout: &timeout,
		Include: []responses.ResponseIncludable{
			responses.ResponseIncludableReasoningEncryptedContent,
		},
		Reasoning: &responses.ReasoningParam{
			Effort: responses.ReasoningEffortMax,
		},
	})
}
