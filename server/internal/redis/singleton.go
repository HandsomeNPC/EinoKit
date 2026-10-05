// Package redis 单例管理。
//
// redis.go 实现客户端构造与探活，本文件维护其包级默认实例，
// 风格与 internal/checkpoint、internal/database 一致：
// Init 写入、GetInstance 读取、SetInstance 供测试注入。
package redis

import (
	"fmt"
	"log"
	"sync"

	goredis "github.com/redis/go-redis/v9"

	"github.com/HandsomeNPC/EinoKit/internal/config"
)

// 包级默认实例。
var (
	mu       sync.RWMutex
	instance *goredis.Client
)

// Init 建立连接并设置为包级默认实例。
func Init() {
	c := config.Get()
	cfg := c.Redis
	client, err := New(cfg)
	if err != nil {
		panic(fmt.Errorf("redis: 初始化失败: %w", err))
	}
	mu.Lock()
	instance = client
	mu.Unlock()
	log.Printf("[%s] Redis 已连接 %s db=%d", c.Server.Name, cfg.Addr(), cfg.DB)
}

// GetInstance 返回包级默认实例，未调用 Init 会 panic。
func GetInstance() *goredis.Client {
	mu.RLock()
	client := instance
	mu.RUnlock()
	if client == nil {
		panic("redis: 尚未初始化，请先调用 redis.Init")
	}
	return client
}

// SetInstance 设置包级默认实例并返回恢复原值的函数，供集成测试注入测试专用连接
// （业务代码用 GetInstance()，不该调本函数；生产路径一律走 Init）。
func SetInstance(client *goredis.Client) (restore func()) {
	mu.Lock()
	prev := instance
	instance = client
	mu.Unlock()
	return func() {
		mu.Lock()
		instance = prev
		mu.Unlock()
	}
}

// CloseInstance 关闭并清空包级默认实例，供进程退出时 defer 调用。
func CloseInstance() {
	mu.Lock()
	client := instance
	instance = nil
	mu.Unlock()
	if err := Close(client); err != nil {
		log.Printf("关闭 Redis 连接失败: %v", err)
	}
}
