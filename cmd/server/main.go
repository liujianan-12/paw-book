// 服务入口：装配 SQLite、教学时钟与 A1–A20 HTTP 接口（模拟支付，非真实接入）。
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"paw-book/internal/api"
	"paw-book/internal/config"
	"paw-book/internal/store"
)

func main() {
	cfg := config.Load()

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()
	if err := store.Migrate(context.Background(), db); err != nil {
		log.Fatalf("迁移失败: %v", err)
	}
	nowFn := buildClock(cfg)
	if os.Getenv("PAW_SEED_DEMO") == "1" {
		if n, err := store.SeedSlots(context.Background(), db, nowFn()); err != nil {
			log.Fatalf("预置演示时段失败: %v", err)
		} else if n > 0 {
			log.Printf("已预置 %d 个演示时段", n)
		}
	}

	srv := api.New(db, nowFn, cfg.CancelCutoff, cfg.PayHoldTimeout, cfg.PaySecret)
	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("paw-book 监听 %s（模拟支付，仅教学演示）", cfg.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP 服务失败: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
}

// buildClock 按教学时钟模式注入时间（05 §2：业务代码不直接 time.Now）。
func buildClock(cfg config.Config) func() time.Time {
	if cfg.ClockMode == "real" {
		return time.Now
	}
	base, err := time.Parse(time.RFC3339, cfg.ClockBase)
	if err != nil {
		log.Printf("TEACHING_CLOCK_BASE 非法（%v），回退真实时钟", err)
		return time.Now
	}
	start := time.Now()
	return func() time.Time {
		if cfg.ClockMode == "fixed" || cfg.ClockMode == "manual" {
			return base
		}
		return base.Add(time.Since(start))
	}
}
