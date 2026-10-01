package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

// DriverPostgres 支持的数据库驱动。
const DriverPostgres = "postgres"

// SQL 日志级别取值。
const (
	LogLevelSilent = "silent"
	LogLevelError  = "error"
	LogLevelWarn   = "warn"
	LogLevelInfo   = "info"
)

// DatasourceConfig PostgreSQL 数据源配置。
type DatasourceConfig struct {
	Driver          string `mapstructure:"driver"`
	Host            string `mapstructure:"host"`
	Port            int    `mapstructure:"port"`
	Username        string `mapstructure:"username"`
	Password        string `mapstructure:"password"`
	DBName          string `mapstructure:"dbname"`
	Params          string `mapstructure:"params"` // libpq 连接参数，如 "sslmode=disable TimeZone=Asia/Shanghai"
	MaxIdleConns    int    `mapstructure:"maxIdleConns"`
	MaxOpenConns    int    `mapstructure:"maxOpenConns"`
	ConnMaxLifetime int    `mapstructure:"connMaxLifetime"` // 秒
	ConnMaxIdleTime int    `mapstructure:"connMaxIdleTime"` // 秒
	LogLevel        string `mapstructure:"logLevel"`        // silent/error/warn/info，缺省 warn
	SlowThresholdMs int    `mapstructure:"slowThresholdMs"` // 慢 SQL 阈值(毫秒)，缺省 200
}

// DefaultDatasource 返回带默认值的 Datasource 配置。
func DefaultDatasource() DatasourceConfig {
	return DatasourceConfig{
		Driver:          DriverPostgres,
		Params:          "sslmode=disable TimeZone=Asia/Shanghai",
		MaxOpenConns:    100,
		MaxIdleConns:    10,
		ConnMaxLifetime: 1800,
		ConnMaxIdleTime: 600,
		LogLevel:        LogLevelWarn,
		SlowThresholdMs: 200,
	}
}

// setDefaults 将默认值写入 viper，仅对未显式配置的 key 生效。
func (d DatasourceConfig) setDefaults(v *viper.Viper) {
	v.SetDefault("datasource.driver", d.Driver)
	v.SetDefault("datasource.params", d.Params)
	v.SetDefault("datasource.maxIdleConns", d.MaxIdleConns)
	v.SetDefault("datasource.maxOpenConns", d.MaxOpenConns)
	v.SetDefault("datasource.connMaxLifetime", d.ConnMaxLifetime)
	v.SetDefault("datasource.connMaxIdleTime", d.ConnMaxIdleTime)
	v.SetDefault("datasource.logLevel", d.LogLevel)
	v.SetDefault("datasource.slowThresholdMs", d.SlowThresholdMs)
}

// DSN 返回 GORM PostgreSQL(libpq) 连接串。
func (d DatasourceConfig) DSN() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s %s",
		d.Host, d.Port, d.Username, d.Password, d.DBName, d.Params)
}

// MaxLifetime 返回连接最大存活时长。
func (d DatasourceConfig) MaxLifetime() time.Duration {
	return time.Duration(d.ConnMaxLifetime) * time.Second
}

// MaxIdleTime 返回空闲连接最大存活时长。
func (d DatasourceConfig) MaxIdleTime() time.Duration {
	return time.Duration(d.ConnMaxIdleTime) * time.Second
}

// SlowThreshold 返回慢 SQL 阈值，未配置时默认 200ms。
func (d DatasourceConfig) SlowThreshold() time.Duration {
	if d.SlowThresholdMs <= 0 {
		return 200 * time.Millisecond
	}
	return time.Duration(d.SlowThresholdMs) * time.Millisecond
}

// Level 返回规范化后的 SQL 日志级别，未配置时默认 warn。
func (d DatasourceConfig) Level() string {
	if d.LogLevel == "" {
		return LogLevelWarn
	}
	return d.LogLevel
}

// validate 校验数据源配置。
func (d DatasourceConfig) validate() error {
	if d.Driver != "" && d.Driver != DriverPostgres {
		return errInvalid("datasource.driver", "仅支持 "+DriverPostgres)
	}
	if d.Host == "" {
		return errMissing("datasource.host")
	}
	if d.DBName == "" {
		return errMissing("datasource.dbname")
	}
	if d.Port <= 0 {
		return errInvalid("datasource.port", "必须大于 0")
	}
	if d.MaxOpenConns > 0 && d.MaxIdleConns > d.MaxOpenConns {
		return errInvalid("datasource.maxIdleConns", "不能大于 maxOpenConns")
	}
	switch d.LogLevel {
	case "", LogLevelSilent, LogLevelError, LogLevelWarn, LogLevelInfo:
	default:
		return errInvalid("datasource.logLevel",
			"必须为 silent/error/warn/info 之一")
	}
	return nil
}
