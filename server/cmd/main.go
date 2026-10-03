// Package main 是 EinoKit 后端入口：加载配置、初始化依赖、构建路由、启动 HTTP 服务。
package main

import (
	"time"

	"github.com/HandsomeNPC/EinoKit/internal/checkpoint"
	"github.com/HandsomeNPC/EinoKit/internal/config"
	"github.com/HandsomeNPC/EinoKit/internal/database"
	"github.com/HandsomeNPC/EinoKit/internal/redis"
	"github.com/HandsomeNPC/EinoKit/internal/router"
)

func main() {
	config.Load("config/application.yaml")

	database.Init()
	defer database.CloseDefault()

	redis.Init()
	defer redis.CloseDefault()

	// 须在 redis.Init 之后：TurnLoop 的 checkpoint 快照落在这里。
	// TTL 兜底清理被用户遗弃的快照。
	checkpoint.Init(checkpoint.WithTTL(24 * time.Hour))

	r := router.InitRouter()
	router.InitServer(r)
}
