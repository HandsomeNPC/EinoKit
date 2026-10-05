// Package database MySQL + GORM 初始化。
package database

import (
	"context"
	"fmt"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"github.com/HandsomeNPC/EinoKit/internal/config"
)

// New 按配置建立数据库连接并完成连接池设置。
func New(cfg config.DatasourceConfig) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{
		Logger: newLogger(cfg),
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true,
		},
		DisableForeignKeyConstraintWhenMigrating: true,
		SkipDefaultTransaction:                   true,
	})
	if err != nil {
		return nil, fmt.Errorf("database: 连接 %s:%d/%s 失败: %w",
			cfg.Host, cfg.Port, cfg.DBName, err)
	}

	if err := setupPool(db, cfg); err != nil {
		return nil, err
	}
	if err := ping(db); err != nil {
		return nil, err
	}
	return db, nil
}

// setupPool 应用连接池参数。
func setupPool(db *gorm.DB, cfg config.DatasourceConfig) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("database: 获取底层连接池失败: %w", err)
	}
	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(cfg.MaxLifetime())
	}
	if cfg.ConnMaxIdleTime > 0 {
		sqlDB.SetConnMaxIdleTime(cfg.MaxIdleTime())
	}
	return nil
}

// ping 启动探活，确认数据库真实可用。
func ping(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("database: 获取底层连接池失败: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()

	if err := sqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("database: 数据库探活失败: %w", err)
	}
	return nil
}

// Close 关闭连接池，供进程退出时调用。
func Close(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("database: 获取底层连接池失败: %w", err)
	}
	return sqlDB.Close()
}
