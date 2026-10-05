package memory

import (
	"github.com/HandsomeNPC/EinoKit/internal/database"
	"github.com/HandsomeNPC/EinoKit/internal/memory/thread"
)

// AutoMigrate 一次性创建/补齐全部记忆域的表
func AutoMigrate() error {
	return database.GetInstance().AutoMigrate(&thread.ThreadInfo{}, &thread.ThreadMessage{})
}
