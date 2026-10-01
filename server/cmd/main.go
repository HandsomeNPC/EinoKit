// Package main 是 EinoKit 后端入口：加载配置、构建路由、启动 HTTP 服务。
package main

import (
	"github.com/HandsomeNPC/EinoKit/internal/config"
	"github.com/HandsomeNPC/EinoKit/internal/router"
)

func main() {
	config.Load("config/application.yaml")

	r := router.InitRouter()
	router.InitServer(r)
}
