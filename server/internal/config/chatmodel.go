package config

import (
	"time"

	"github.com/spf13/viper"
)

// ChatModelConfig 对话模型配置（agenticopenai / Responses API）。
type ChatModelConfig struct {
	APIKey    string `mapstructure:"apiKey"`    // API Key，真实值只写在 application.yaml（已 gitignore）
	Model     string `mapstructure:"model"`     // 模型名，如 gpt-5
	BaseURL   string `mapstructure:"baseURL"`   // 接入点地址
	ByAzure   bool   `mapstructure:"byAzure"`   // 是否走 Azure OpenAI
	TimeoutMs int    `mapstructure:"timeoutMs"` // 请求超时(毫秒)，缺省 180000
}

// DefaultChatModel 返回带默认值的对话模型配置。
func DefaultChatModel() ChatModelConfig {
	return ChatModelConfig{TimeoutMs: 180000}
}

// setDefaults 将默认值写入 viper，仅对未显式配置的 key 生效。
func (c ChatModelConfig) setDefaults(v *viper.Viper) {
	v.SetDefault("chatmodel.timeoutMs", c.TimeoutMs)
	v.SetDefault("chatmodel.byAzure", c.ByAzure)
}

// Timeout 返回请求超时时长，未配置(<=0)时默认 3 分钟。
func (c ChatModelConfig) Timeout() time.Duration {
	if c.TimeoutMs <= 0 {
		return 3 * time.Minute
	}
	return time.Duration(c.TimeoutMs) * time.Millisecond
}

// validate 校验对话模型配置。
func (c ChatModelConfig) validate() error {
	if c.APIKey == "" {
		return errMissing("chatmodel.apiKey")
	}
	if c.Model == "" {
		return errMissing("chatmodel.model")
	}
	if c.BaseURL == "" {
		return errMissing("chatmodel.baseURL")
	}
	if c.TimeoutMs < 0 {
		return errInvalid("chatmodel.timeoutMs", "不能为负数")
	}
	return nil
}
