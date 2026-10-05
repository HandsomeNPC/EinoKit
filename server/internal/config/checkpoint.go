package config

import (
	"time"

	"github.com/spf13/viper"
)

// DefaultCheckpointPrefix checkpoint 在 Redis 中的默认 key 前缀，避免与业务 key 冲突。
const DefaultCheckpointPrefix = "eino:checkpoint:"

// CheckpointConfig Redis checkpoint 存储配置（compose.CheckPointStore 后端）。
type CheckpointConfig struct {
	// Prefix 为 key 前缀，留空时使用 DefaultCheckpointPrefix。
	Prefix string `mapstructure:"prefix"`
	// TTL 单条 checkpoint 的过期时间，支持 Go duration 字符串（如 24h、30m、1.5h）；
	// 留空表示不过期，永久存储。
	TTL string `mapstructure:"ttl"`
}

// DefaultCheckpoint 返回带默认值的 checkpoint 配置。
func DefaultCheckpoint() CheckpointConfig {
	return CheckpointConfig{
		Prefix: DefaultCheckpointPrefix,
	}
}

// setDefaults 将默认值写入 viper，仅对未显式配置的 key 生效。
func (c CheckpointConfig) setDefaults(v *viper.Viper) {
	v.SetDefault("checkpoint.prefix", DefaultCheckpointPrefix)
}

// PrefixOrDefault 返回实际生效的 key 前缀，留空时回退到默认值。
func (c CheckpointConfig) PrefixOrDefault() string {
	if c.Prefix == "" {
		return DefaultCheckpointPrefix
	}
	return c.Prefix
}

// Duration 把 TTL 字符串解析为过期时长；空字符串表示不过期返回 0。
func (c CheckpointConfig) Duration() time.Duration {
	if c.TTL == "" {
		return 0
	}
	d, err := time.ParseDuration(c.TTL)
	if err != nil {
		return 0
	}
	return d
}

// validate 校验 checkpoint 配置：TTL 留空合法（不过期），非空时必须可解析为 duration。
func (c CheckpointConfig) validate() error {
	if c.TTL == "" {
		return nil
	}
	if _, err := time.ParseDuration(c.TTL); err != nil {
		return errInvalid("checkpoint.ttl", "无法解析为 duration（示例: 24h、30m、1.5h）")
	}
	return nil
}
