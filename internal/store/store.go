// Package store 负责数据库连接与结构初始化。
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// Open 打开（必要时创建）SQLite 数据库并执行建表脚本。
// 脚本可重复执行（全部使用 IF NOT EXISTS），服务每次启动都会执行以保证结构最新。
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
	return db, nil
}

// Migrate 执行建表脚本。
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("执行建表脚本: %w", err)
	}
	return nil
}
