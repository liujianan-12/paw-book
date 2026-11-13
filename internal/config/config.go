// Package config 提供运行配置与"教学时钟"参数（.agent/05 §2）。
package config

import (
	"os"
	"time"
)

// Config 服务运行参数。
type Config struct {
	Addr           string        // HTTP 监听地址
	DBPath         string        // SQLite 文件路径
	PayHoldTimeout time.Duration // 未付款占位时长（默认 5m）
	CancelCutoff   time.Duration // 免费取消分界（默认 2h）
	PayChannelAddr string        // 模拟支付渠道地址
	ClockMode      string        // 教学时钟模式：real | fixed | offset | manual
	ClockBase      string        // fixed/offset 模式的基准时间（RFC3339）
	PaySecret      string        // 模拟回调签名密钥（X-Mock-Signature）
	SessionSecret  string        // 会话签名密钥（演示用）
}

// Load 读取环境变量，未设置时使用默认值。
func Load() Config {
	return Config{
		Addr:           env("PAW_ADDR", ":8080"),
		DBPath:         env("PAW_DB", "paw-book.db"),
		PayHoldTimeout: durationEnv("PAW_PAY_HOLD", 5*time.Minute),
		CancelCutoff:   durationEnv("PAW_CANCEL_CUTOFF", 2*time.Hour),
		PayChannelAddr: env("PAW_PAY_CHANNEL", "http://localhost:8081"),
		ClockMode:      env("TEACHING_CLOCK_MODE", "real"),
		ClockBase:      env("TEACHING_CLOCK_BASE", "2026-11-14T00:00:00Z"),
		PaySecret:      env("PAW_PAY_SECRET", "mock-secret"),
		SessionSecret:  env("PAW_SESSION_SECRET", "dev-session-secret"),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func durationEnv(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
