package chatmodel

import (
	"context"
	"log"

	"github.com/cloudwego/eino-ext/components/model/agenticopenai"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/openai/openai-go/v3/responses"

	"github.com/HandsomeNPC/EinoKit/internal/config"
)

// NewAgenticModel 按配置构造 agentic 模型（Responses API）。
func NewAgenticModel(ctx context.Context) einomodel.AgenticModel {
	cfg := config.Get().ChatModel
	timeout := cfg.Timeout()
	m, err := agenticopenai.NewResponsesModel(ctx, &agenticopenai.ResponsesConfig{
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
	if err != nil {
		log.Printf("[chatmodel] 初始化 agentic 模型失败: %v", err)
		return nil
	}
	return m
}
