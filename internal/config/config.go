// Package config 提供运行配置与"教学时钟"参数。
package config

import (
	"os"
	"time"
)

// Config 汇总服务运行参数。
// 业务规则固定为：未付款占名额 5 分钟、开始前 2 小时为取消政策分界。
// 通过环境变量缩短时长仅用于课堂演示与加速测试，代码路径完全相同。
type Config struct {
	Addr           string        // HTTP 监听地址
	DBPath         string        // SQLite 文件路径
	PayHoldTimeout time.Duration // 未付款预约占名额时长（默认 5m）
	CancelCutoff   time.Duration // 临近开始的取消分界（默认 2h）
	PayChannelAddr string        // 模拟支付渠道服务地址
}

// Load 从环境变量读取配置，未设置时使用默认值。
func Load() Config {
	return Config{
		Addr:           env("PAW_ADDR", ":8080"),
		DBPath:         env("PAW_DB", "paw-book.db"),
		PayHoldTimeout: durationEnv("PAW_PAY_HOLD", 5*time.Minute),
		CancelCutoff:   durationEnv("PAW_CANCEL_CUTOFF", 2*time.Hour),
		PayChannelAddr: env("PAW_PAY_CHANNEL", "http://localhost:8081"),
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
