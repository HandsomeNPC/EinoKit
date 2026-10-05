// Package main 是 EinoKit 后端入口：加载配置、初始化依赖、构建路由、启动 HTTP 服务。
package main

import (
	"log"

	"github.com/HandsomeNPC/EinoKit/internal/checkpoint"
	"github.com/HandsomeNPC/EinoKit/internal/config"
	"github.com/HandsomeNPC/EinoKit/internal/database"
	"github.com/HandsomeNPC/EinoKit/internal/memory"
	"github.com/HandsomeNPC/EinoKit/internal/redis"
	"github.com/HandsomeNPC/EinoKit/internal/router"
)

func main() {
	config.Load("config/application.yaml")

	database.Init()
	defer database.CloseInstance()

	// 一次性创建/补齐全部记忆域的表（当前 thread_info / thread_message）。
	// 须在 database.Init 之后：复用其 *gorm.DB 与 NamingStrategy。
	if err := memory.AutoMigrate(); err != nil {
		log.Fatalf("[main] memory AutoMigrate 失败: %v", err)
	}

	redis.Init()
	defer redis.CloseInstance()

	// 须在 redis.Init 之后：TurnLoop 的 checkpoint 快照落在这里。
	// Prefix/TTL 由 application.yaml 的 checkpoint 段配置；TTL 留空则永久存储。
	// loop 包重写后，在此重新接入 agent.ThreadSvcApp = agent.NewThreadService(checkpoint.GetInstance())。
	checkpoint.Init()

	r := router.InitRouter()
	router.InitServer(r)
}
