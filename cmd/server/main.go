// paw-book 服务入口：加载配置 → 初始化数据库 → 启动 HTTP 服务。
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"paw-book/internal/config"
	"paw-book/internal/store"
)

func main() {
	cfg := config.Load()

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}
	defer db.Close()

	if err := store.Migrate(context.Background(), db); err != nil {
		log.Fatalf("初始化表结构失败: %v", err)
	}
	log.Printf("数据库就绪: %s", cfg.DBPath)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"app": "paw-book", "status": "running"})
	})

	log.Printf("paw-book 启动于 %s（占名额 %v，取消分界 %v，支付渠道 %s）",
		cfg.Addr, cfg.PayHoldTimeout, cfg.CancelCutoff, cfg.PayChannelAddr)
	if err := http.ListenAndServe(cfg.Addr, mux); err != nil {
		log.Fatal(err)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
