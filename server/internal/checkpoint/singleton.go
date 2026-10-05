// Package checkpoint 单例管理。
//
// store.go 实现 compose.CheckPointStore 的 Redis 后端，本文件维护其包级默认实例，
// 风格与 internal/redis 一致：Init 写入、Default 读取、SetStore 供测试注入。
package checkpoint

import (
	"log"
	"sync"

	"github.com/cloudwego/eino/compose"

	"github.com/HandsomeNPC/EinoKit/internal/config"
)

// 包级默认实例。
var (
	mu       sync.RWMutex
	instance compose.CheckPointStore
)

// Init 按配置构建 Redis checkpoint store 并设置为包级默认实例。
//
// 须在 redis.Init 之后调用：store 复用 internal/redis 包级客户端。
func Init() {
	c := config.Get()
	store := NewRedisCheckpointStore(c.Checkpoint)
	mu.Lock()
	instance = store
	mu.Unlock()
	log.Printf("[%s] checkpoint store 已就绪 prefix=%s ttl=%s",
		c.Server.Name, c.Checkpoint.PrefixOrDefault(), c.Checkpoint.TTL)
}

// GetInstance 返回包级默认实例，未调用 Init 会 panic。
func GetInstance() compose.CheckPointStore {
	mu.RLock()
	store := instance
	mu.RUnlock()
	if store == nil {
		panic("checkpoint: 尚未初始化，请先调用 checkpoint.Init")
	}
	return store
}

// SetInstance 设置包级默认实例并返回恢复原值的函数，供集成测试注入测试专用 store
// （业务代码用 GetInstance()，不该调本函数；生产路径一律走 Init）。
func SetInstance(store compose.CheckPointStore) (restore func()) {
	mu.Lock()
	prev := instance
	instance = store
	mu.Unlock()
	return func() {
		mu.Lock()
		instance = prev
		mu.Unlock()
	}
}
