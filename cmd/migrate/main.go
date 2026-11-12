// migrate 命令：显式执行数据库迁移与种子数据（服务启动不自动改表，见 .agent/02 §6）。
package main

import (
	"context"
	"log"
	"time"

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

	ctx := context.Background()
	if err := store.Migrate(ctx, db); err != nil {
		log.Fatalf("执行迁移失败: %v", err)
	}
	n, err := store.SeedSlots(ctx, db, time.Now())
	if err != nil {
		log.Fatalf("预置时段失败: %v", err)
	}
	log.Printf("迁移完成：数据库 %s，新增时段 %d 个（已存在则跳过）", cfg.DBPath, n)
}
