// Package biz 实现核心业务规则（名额、预约、支付、取消、核销、退款）。
// 所有规则编号（R*）与 docs/01-需求说明.md 对应；状态一律使用 internal/model 常量。
package biz

import (
	"context"
	"database/sql"
	"time"
)

// DBTX 数据库执行抽象；*sql.DB 与 *sql.Tx 均实现该接口。
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// txBeginner 可开启事务的连接（*sql.DB 实现；*sql.Tx 不行，防止事务嵌套）。
type txBeginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// beginTx 在支持事务的连接上开启事务。
// SQLite DSN 已配置 _txlock=immediate：写事务一开始就拿写锁，避免并发提交时锁升级失败。
func beginTx(ctx context.Context, db DBTX) (*sql.Tx, error) {
	b, ok := db.(txBeginner)
	if !ok {
		return nil, errf(CodeInvalidParam, "当前连接不支持开启事务")
	}
	return b.BeginTx(ctx, nil)
}

// Clock 教学时钟：业务中一切"当前时间"必须通过它获取。
// 测试注入固定时钟；课堂演示注入加速时钟；线上使用 RealClock。代码路径完全相同。
type Clock func() time.Time

// RealClock 真实系统时钟。
func RealClock() Clock { return time.Now }

// fmtTime 统一时间存储格式：UTC + RFC3339。
// 同一格式下 SQLite 文本比较（字典序）即时间序，便于 SQL 过滤。
func fmtTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// parseTime 解析存储的时间。
func parseTime(s string) (time.Time, error) { return time.Parse(time.RFC3339, s) }

// audit 记录审计日志：所有状态变更必须留痕（AGENTS.md 代码约定）。
func audit(ctx context.Context, db DBTX, actorType string, actorID int64, action, target, detail string, now time.Time) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO audit_logs(actor_type, actor_id, action, target, detail, created_at) VALUES(?,?,?,?,?,?)`,
		actorType, actorID, action, target, detail, fmtTime(now))
	return err
}
