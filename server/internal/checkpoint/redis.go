// Package checkpoint 实现 compose.CheckPointStore 的 Redis 后端，
// 用于 Eino Graph 编排过程中的断点持久化与恢复。
//
// 复用 internal/redis 包级客户端，不自行管理连接池。
package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cloudwego/eino/compose"
	goredis "github.com/redis/go-redis/v9"

	"github.com/HandsomeNPC/EinoKit/internal/redis"
)

const (
	// defaultKeyPrefix checkpoint 在 Redis 中的默认 key 前缀，避免与业务 key 冲突。
	defaultKeyPrefix = "eino:checkpoint:"
)

// Option 配置 RedisCheckpointStore。
type Option func(*config)

type config struct {
	prefix string
	ttl    time.Duration // 0 表示不过期
	client *goredis.Client
}

// WithKeyPrefix 自定义 key 前缀。
func WithKeyPrefix(p string) Option {
	return func(c *config) { c.prefix = p }
}

// WithTTL 设置单条 checkpoint 的过期时间；<=0 表示不过期（默认）。
func WithTTL(d time.Duration) Option {
	return func(c *config) { c.ttl = d }
}

// WithClient 注入自定义 Redis 客户端（默认使用 internal/redis 包级实例）。
func WithClient(cli *goredis.Client) Option {
	return func(c *config) { c.client = cli }
}

// New 创建基于 Redis 的 compose.CheckPointStore。
//
// 未传 WithClient 时，使用 internal/redis.Client() 作为后端。
func New(opts ...Option) compose.CheckPointStore {
	cfg := config{
		prefix: defaultKeyPrefix,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.client == nil {
		cfg.client = redis.Client()
	}
	return &redisStore{
		client: cfg.client,
		prefix: cfg.prefix,
		ttl:    cfg.ttl,
	}
}

type redisStore struct {
	client *goredis.Client
	prefix string
	ttl    time.Duration
}

// Set 写入一条 checkpoint。ttl>0 时携带过期时间，否则永久存储。
func (s *redisStore) Set(ctx context.Context, key string, value []byte) error {
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
func (s *redisStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
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

// 包级默认实例。
var (
	mu           sync.RWMutex
	defaultStore compose.CheckPointStore
)

// Init 基于包级 Redis 客户端初始化默认 CheckPointStore。
//
// 应在 redis.Init 之后、Graph 编排之前调用。
func Init(opts ...Option) {
	mu.Lock()
	defer mu.Unlock()
	defaultStore = New(opts...)
}

// Default 返回包级默认实例，未调用 Init 会 panic。
func Default() compose.CheckPointStore {
	mu.RLock()
	s := defaultStore
	mu.RUnlock()
	if s == nil {
		panic("checkpoint: 尚未初始化，请先调用 checkpoint.Init")
	}
	return s
}

// SetDefault 设置包级默认实例并返回恢复原值的函数，供集成测试注入测试专用 store
// （业务代码用 Default()，生产路径一律走 Init）。
func SetDefault(s compose.CheckPointStore) (restore func()) {
	mu.Lock()
	prev := defaultStore
	defaultStore = s
	mu.Unlock()
	return func() {
		mu.Lock()
		defaultStore = prev
		mu.Unlock()
	}
}
