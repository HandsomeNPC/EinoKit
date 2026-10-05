// Package checkpoint 实现 compose.CheckPointStore 的 Redis 后端，
// 用于 Eino Graph 编排过程中的断点持久化与恢复。
//
// 复用 internal/redis 包级客户端，不自行管理连接池。
package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cloudwego/eino/compose"
	goredis "github.com/redis/go-redis/v9"

	"github.com/HandsomeNPC/EinoKit/internal/config"
	"github.com/HandsomeNPC/EinoKit/internal/redis"
)

// NewRedisCheckpointStore 创建基于 Redis 的 compose.CheckPointStore。
//
// 配置项（Prefix、TTL）来自 config.CheckpointConfig，由 application.yaml 注入；
// Prefix 留空时使用 config.DefaultCheckpointPrefix；TTL 留空时永久存储。
// Redis 客户端复用 internal/redis 包级实例。
func NewRedisCheckpointStore(cfg config.CheckpointConfig) compose.CheckPointStore {
	return &RedisCheckpointStore{
		client: redis.GetInstance(),
		prefix: cfg.PrefixOrDefault(),
		ttl:    cfg.Duration(),
	}
}

type RedisCheckpointStore struct {
	client *goredis.Client
	prefix string
	ttl    time.Duration
}

// Set 写入一条 checkpoint。ttl>0 时携带过期时间，否则永久存储。
func (s *RedisCheckpointStore) Set(ctx context.Context, key string, value []byte) error {
	if s.client == nil {
		return errors.New("checkpoint: redis 客户端未注入")
	}
	k := s.prefix + key
	if s.ttl > 0 {
		if err := s.client.Set(ctx, k, value, s.ttl).Err(); err != nil {
			return fmt.Errorf("checkpoint: 写入 %s 失败: %w", k, err)
		}
		return nil
	}
	if err := s.client.Set(ctx, k, value, 0).Err(); err != nil {
		return fmt.Errorf("checkpoint: 写入 %s 失败: %w", k, err)
	}
	return nil
}

// Get 读取一条 checkpoint；不存在时 existed 为 false。
func (s *RedisCheckpointStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if s.client == nil {
		return nil, false, errors.New("checkpoint: redis 客户端未注入")
	}
	k := s.prefix + key
	v, err := s.client.Get(ctx, k).Bytes()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("checkpoint: 读取 %s 失败: %w", k, err)
	}
	return v, true, nil
}

// Delete 删除一条 checkpoint。
//
// adk.CheckPointStore 本身只要求 Get/Set，但 TurnLoop 在干净退出时会机会性断言
// CheckPointDeleter 来清掉快照（eino adk/turn_loop.go:983）。不实现它的话旧快照
// 会残留，挤占存储且可能让后续轮次读到过期状态——这是最难排查的故障模式。
func (s *RedisCheckpointStore) Delete(ctx context.Context, key string) error {
	if s.client == nil {
		return errors.New("checkpoint: redis 客户端未注入")
	}
	k := s.prefix + key
	if err := s.client.Del(ctx, k).Err(); err != nil {
		return fmt.Errorf("checkpoint: 删除 %s 失败: %w", k, err)
	}
	return nil
}
