// Command migrate 建表 / 补列，并验证 jsonb 绑定可用。
//
// 用 GORM AutoMigrate：表不存在就按 model 建出来，已存在就只补缺的列。幂等，可重复执行。
//
// 为什么独立于 cmd/server：建表是显式动作，不应该在每次启动服务时偷偷做。
package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/spf13/viper"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"github.com/HandsomeNPC/EinoKit/internal/model"
)

func main() {
	db := connect()

	// 建表 / 补列。表名由各 model 的 TableName() 决定。
	if err := db.AutoMigrate(&model.Session{}, &model.SessionMessage{}); err != nil {
		log.Fatalf("AutoMigrate 失败: %v", err)
	}
	log.Println("建表/补列完成: session, session_message")

	verify(db)
}

// connect 直接读 datasource 块建连接。
// 不走 config.Load：它会校验 chatmodel.apiKey，而建表用不到模型配置。
func connect() *gorm.DB {
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetConfigFile("config/application.yaml")
	if err := v.ReadInConfig(); err != nil {
		log.Fatalf("读配置失败: %v", err)
	}

	params := v.GetString("datasource.params")
	if params == "" {
		params = "sslmode=disable TimeZone=Asia/Shanghai"
	}
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s %s",
		v.GetString("datasource.host"), v.GetInt("datasource.port"),
		v.GetString("datasource.username"), v.GetString("datasource.password"),
		v.GetString("datasource.dbname"), params)

	// NamingStrategy 与 internal/database 保持一致，否则 AutoMigrate 可能
	// 按复数名建出另一套表。
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		NamingStrategy:                           schema.NamingStrategy{SingularTable: true},
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	return db
}

// verify 插一条再读回来，确认 []byte → jsonb 的绑定真的可用
// （pgx v5 下把 Go string 绑到 jsonb 参数会报 encode plan 错误，这是改成
// []byte 的原因；此处就是把这一点钉死）。验证完即清理。
func verify(db *gorm.DB) {
	sid := uuid.New().String()
	if err := db.Create(&model.Session{ID: sid, MessageKind: "agentic"}).Error; err != nil {
		log.Fatalf("插入会话失败: %v", err)
	}
	defer func() {
		db.Where("session_id = ?", sid).Delete(&model.SessionMessage{})
		db.Where("id = ?", sid).Delete(&model.Session{})
		log.Println("测试数据已清理")
	}()

	payload, err := json.Marshal(map[string]any{"role": "user", "content_blocks": []any{}})
	if err != nil {
		log.Fatalf("构造测试载荷失败: %v", err)
	}
	if err := db.Create(&model.SessionMessage{SessionID: sid, Payload: payload}).Error; err != nil {
		log.Fatalf("插入 jsonb 消息失败(这正是要验证的点): %v", err)
	}
	log.Println("jsonb 绑定 OK")

	var got model.SessionMessage
	if err := db.Where("session_id = ?", sid).First(&got).Error; err != nil {
		log.Fatalf("回读失败: %v", err)
	}
	log.Printf("回读 OK: payload=%s", got.Payload)
}
