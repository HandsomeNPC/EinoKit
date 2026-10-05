// Package database 单例管理。
//
// database.go 实现连接构造、连接池与探活，本文件维护其包级默认实例，
// 风格与 internal/redis、internal/checkpoint 一致：
// Init 写入、GetInstance 读取、SetInstance 供测试注入。
package database

import (
	"fmt"
	"log"
	"sync"

	"gorm.io/gorm"

	"github.com/HandsomeNPC/EinoKit/internal/config"
)

// 包级默认实例。
var (
	mu       sync.RWMutex
	instance *gorm.DB
)

// Init 建立连接并设置为包级默认实例。
func Init() {
	c := config.Get()
	cfg := c.Datasource
	db, err := New(cfg)
	if err != nil {
		panic(fmt.Errorf("database: 初始化失败: %w", err))
	}
	mu.Lock()
	instance = db
	mu.Unlock()
	log.Printf("[%s] 数据库已连接 %s:%d/%s",
		c.Server.Name, cfg.Host, cfg.Port, cfg.DBName)
}

// GetInstance 返回包级默认实例，未调用 Init 会 panic。
func GetInstance() *gorm.DB {
	mu.RLock()
	db := instance
	mu.RUnlock()
	if db == nil {
		panic("database: 尚未初始化，请先调用 database.Init")
	}
	return db
}

// SetInstance 设置包级默认实例并返回恢复原值的函数，供测试注入专用连接。
func SetInstance(db *gorm.DB) (restore func()) {
	mu.Lock()
	prev := instance
	instance = db
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
	db := instance
	instance = nil
	mu.Unlock()
	if err := Close(db); err != nil {
		log.Printf("关闭数据库连接失败: %v", err)
	}
}
