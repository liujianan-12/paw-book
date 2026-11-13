// Package repo 提供数据库执行抽象、事务、审计与时间/约束等基础访问能力。
// 业务编排在 internal/service，领域状态与错误码在 internal/domain。
package repo

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"paw-book/internal/domain"
)

// DBTX 数据库执行抽象；*sql.DB 与 *sql.Tx 均实现。
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type txBeginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// BeginTx 开启写事务；DSN 已配置 _txlock=immediate，等价于对写路径串行化。
func BeginTx(ctx context.Context, db DBTX) (*sql.Tx, error) {
	b, ok := db.(txBeginner)
	if !ok {
		return nil, domain.Errf(domain.CodeInvalidParam, "当前连接不支持开启事务")
	}
	return b.BeginTx(ctx, nil)
}

// Clock 教学时钟（.agent/05 §2）：业务中一切"当前时间"必须通过它获取。
type Clock func() time.Time

// RealClock 真实时钟。
func RealClock() Clock { return time.Now }

// FmtTime 统一时间存储：UTC RFC3339（字典序即时间序）。
func FmtTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func ParseTime(s string) (time.Time, error) { return time.Parse(time.RFC3339, s) }

// CheckStaff 校验员工存在、角色匹配，返回其所属门店（R5.1/权限：身份只来自服务端会话）。
// 角色不符返回 ROLE_FORBIDDEN；员工不存在返回 NOT_FOUND。
func CheckStaff(ctx context.Context, db DBTX, staffID int64, wantRole string) (int64, error) {
	var storeID int64
	var role string
	err := db.QueryRowContext(ctx, `SELECT store_id, role FROM staff WHERE id=?`, staffID).Scan(&storeID, &role)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, domain.Errf(domain.CodeNotFound, "员工不存在")
	}
	if err != nil {
		return 0, err
	}
	if role != wantRole {
		return 0, domain.Errf(domain.CodeRoleForbidden, "该操作要求 %s 角色（当前 %s）", wantRole, role)
	}
	return storeID, nil
}

// IsConstraintViolation 判断 SQLite 唯一约束冲突（含部分唯一索引）。
// SQLite 报错形如 "UNIQUE constraint failed: reservation.slot_id, reservation.pet_id"，
// 不包含索引名，因此按“表名 + 列名”识别，避免把驱动错误直接抛给接口层。
func IsConstraintViolation(err error, table string, cols ...string) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	if !strings.Contains(strings.ToLower(s), "unique constraint failed") {
		return false
	}
	if table != "" && !strings.Contains(s, table) {
		return false
	}
	for _, c := range cols {
		if !strings.Contains(s, c) {
			return false
		}
	}
	return true
}

// Audit 写操作审计（operation_log）。
func Audit(ctx context.Context, db DBTX, actorID int64, actorRole string, storeID int64,
	action, targetType string, targetID int64, result, detail string, now time.Time) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO operation_log(actor_id, actor_role, store_id, action, target_type, target_id, result, request_id, detail, created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		actorID, actorRole, storeID, action, targetType, strconv.FormatInt(targetID, 10), result, "", detail, FmtTime(now))
	return err
}
