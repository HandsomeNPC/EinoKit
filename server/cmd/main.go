// Package main 是 EinoKit 后端入口：加载配置、初始化依赖、构建路由、启动 HTTP 服务。
package main

import (
	"github.com/HandsomeNPC/EinoKit/internal/checkpoint"
	"github.com/HandsomeNPC/EinoKit/internal/config"
	"github.com/HandsomeNPC/EinoKit/internal/database"
	"github.com/HandsomeNPC/EinoKit/internal/redis"
	"github.com/HandsomeNPC/EinoKit/internal/router"
	"github.com/HandsomeNPC/EinoKit/internal/service/agent"
)

func main() {
	config.Load("config/application.yaml")

	database.Init()
	defer database.CloseInstance()

	redis.Init()
	defer redis.CloseInstance()

	// 须在 redis.Init 之后：TurnLoop 的 checkpoint 快照落在这里。
	// Prefix/TTL 由 application.yaml 的 checkpoint 段配置；TTL 留空则永久存储。
	checkpoint.Init()
	agent.SessionSvcApp = agent.NewSessionService(checkpoint.GetInstance())

	r := router.InitRouter()
	router.InitServer(r)
}
