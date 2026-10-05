// Package redis Redis 客户端初始化。
package redis

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/HandsomeNPC/EinoKit/internal/config"
)

// pingTimeout 启动时探活 Redis 的超时时间。
const pingTimeout = 5 * time.Second

// New 按配置建立 Redis 客户端并完成连接池设置。
func New(cfg config.RedisConfig) (*goredis.Client, error) {
	client := goredis.NewClient(&goredis.Options{
		Addr:            cfg.Addr(),
		Password:        cfg.Password,
		DB:              cfg.DB,
		ClientName:      cfg.ClientName,
		PoolSize:        cfg.PoolSize,
		MinIdleConns:    cfg.MinIdleConns,
		DialTimeout:     cfg.DialTimeout(),
		ReadTimeout:     cfg.ReadTimeout(),
		WriteTimeout:    cfg.WriteTimeout(),
		ConnMaxIdleTime: cfg.MaxIdleTime(),
	})

	if err := ping(client); err != nil {
		// 探活失败也要回收连接池。
		_ = client.Close()
		return nil, fmt.Errorf("redis: 连接 %s db=%d 失败: %w", cfg.Addr(), cfg.DB, err)
	}
	return client, nil
}

// ping 启动探活，确认 Redis 真实可用。
func ping(client *goredis.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("探活失败: %w", err)
	}
	return nil
}

// Close 关闭客户端，供进程退出时调用。
func Close(client *goredis.Client) error {
	if client == nil {
		return nil
	}
	return client.Close()
}
