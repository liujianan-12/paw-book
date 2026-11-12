// Package store 负责数据库连接与迁移执行。
// 规则 .agent/02 §6：启动时**不自动改表**，迁移由 cmd/migrate 显式执行。
package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"paw-book/migrations"
)

// Open 打开（必要时创建）SQLite 数据库连接池。
// _txlock=immediate：写事务开始即取写锁，等价于 MySQL 的 SELECT ... FOR UPDATE 串行化效果
// （见 docs/assumptions.md D1 的替代方案说明）。
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_txlock=immediate", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库 %s: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("连接数据库 %s: %w", path, err)
	}
	// 05 §3.0：SQLite 无行锁，写路径用单连接串行化 + BEGIN IMMEDIATE + 条件更新保证并发正确性
	db.SetMaxOpenConns(1)
	return db, nil
}

// Migrate 按文件名顺序执行 migrations/*.sql（脚本可重复执行）。
func Migrate(ctx context.Context, db *sql.DB) error {
	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		return fmt.Errorf("读取迁移目录: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		content, err := migrations.FS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("读取迁移 %s: %w", name, err)
		}
		for _, stmt := range splitStatements(string(content)) {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("执行迁移 %s 失败: %w", name, err)
			}
		}
	}
	return nil
}

// splitStatements 按分号拆分 SQL 语句并跳过注释行（迁移脚本不含分号字面量）。
func splitStatements(text string) []string {
	var out []string
	var cur strings.Builder
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		cur.WriteString(line)
		cur.WriteString("\n")
		if strings.HasSuffix(trimmed, ";") {
			if stmt := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(cur.String()), ";")); stmt != "" {
				out = append(out, stmt)
			}
			cur.Reset()
		}
	}
	if rest := strings.TrimSpace(cur.String()); rest != "" {
		out = append(out, strings.TrimSuffix(rest, ";"))
	}
	return out
}

// SeedSlots 预置 3 个未来时段（每时段容量 3），已有时段则跳过（E6）。
func SeedSlots(ctx context.Context, db *sql.DB, now time.Time) (int, error) {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM slot`).Scan(&n); err != nil {
		return 0, fmt.Errorf("查询时段数量: %w", err)
	}
	if n > 0 {
		return 0, nil
	}
	base := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Add(24 * time.Hour)
	hours := []int{9, 11, 14}
	for _, h := range hours {
		start := base.Add(time.Duration(h) * time.Hour)
		end := start.Add(time.Hour)
		if _, err := db.ExecContext(ctx,
			`INSERT INTO slot(store_id,item_id,start_at,end_at,capacity,reserved_count,created_by,created_at)
			 VALUES(1,1,?,?,3,0,1,?)`,
			start.Format(time.RFC3339), end.Format(time.RFC3339), now.UTC().Format(time.RFC3339)); err != nil {
			return 0, fmt.Errorf("预置时段: %w", err)
		}
	}
	return len(hours), nil
}
