package config

import (
	"fmt"
	"strconv"

	"github.com/spf13/viper"
)

// ServerConfig HTTP 服务配置。
type ServerConfig struct {
	Name string `mapstructure:"name"` // 服务名，用于日志标识
	Port int    `mapstructure:"port"` // 监听端口，如 8080
}

// DefaultServer 返回带默认值的 Server 配置。
func DefaultServer() ServerConfig {
	return ServerConfig{Name: "EinoKit", Port: 8080}
}

// setDefaults 将默认值写入 viper，仅对未显式配置的 key 生效。
func (s ServerConfig) setDefaults(v *viper.Viper) {
	v.SetDefault("server.name", s.Name)
	v.SetDefault("server.port", s.Port)
}

// validate 校验 Server 配置。
func (s ServerConfig) validate() error {
	if s.Name == "" {
		return errMissing("server.name")
	}
	if s.Port <= 0 || s.Port > 65535 {
		return errInvalid("server.port", fmt.Sprintf("非法端口 %d", s.Port))
	}
	return nil
}

// Addr 拼接监听地址，如 :8080。
func (s ServerConfig) Addr() string {
	return ":" + strconv.Itoa(s.Port)
}
