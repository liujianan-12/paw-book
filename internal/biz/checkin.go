package biz

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"paw-book/internal/model"
)

// checkEmployee 校验员工角色与本店归属（R20），返回其门店。
func checkEmployee(ctx context.Context, db DBTX, employeeID int64, wantRole string) (int64, error) {
	var storeID int64
	var role string
	err := db.QueryRowContext(ctx, `SELECT store_id, role FROM employees WHERE id=?`, employeeID).Scan(&storeID, &role)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errf(CodeNotFound, "员工不存在")
	}
	if err != nil {
		return 0, err
	}
	if role != wantRole {
		return 0, errf(CodePermission, "该操作要求 %s 角色（当前 %s）", wantRole, role)
	}
	return storeID, nil
}

// reservationInfo 读取预约的门店与状态（分类失败原因用）。
func reservationInfo(ctx context.Context, db DBTX, reservationID int64) (int64, string, error) {
	var storeID int64
	var status string
	err := db.QueryRowContext(ctx, `SELECT store_id, status FROM reservations WHERE id=?`, reservationID).Scan(&storeID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", errf(CodeNotFound, "预约不存在")
	}
	if err != nil {
		return 0, "", err
	}
	return storeID, status, nil
}

// CheckIn 前台核销到店（R16/R18/R19/R20）。
// 核销三条件：本店前台操作 + 预约已确认 + 无待处理取消申请
// （"无待处理取消申请"由状态唯一性保证：审核中必为 PENDING_REVIEW，不可能同时是已确认）。
// 条件更新保证：与取消并发时恰有一个成功（R19/I5）；重复核销无效（R18）。
func CheckIn(ctx context.Context, db DBTX, reservationID, employeeID int64, now time.Time) error {
	empStore, err := checkEmployee(ctx, db, employeeID, model.RoleFront)
	if err != nil {
		return err
	}
	resStore, status, err := reservationInfo(ctx, db, reservationID)
	if err != nil {
		return err
	}
	if resStore != empStore {
		return errf(CodePermission, "非本店员工无权操作本店预约")
	}
	if status != model.ResConfirmed {
		return errf(CodeInvalidState, "仅已确认且无待审核取消的预约可核销（当前 %s）", status)
	}

	tx, err := beginTx(ctx, db)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	// 条件更新：已确认 → 已核销；并发下与取消只能赢一个
	res, err := tx.ExecContext(ctx,
		`UPDATE reservations SET status=?, checked_in_at=?, updated_at=? WHERE id=? AND status=?`,
		model.ResCheckedIn, fmtTime(now), fmtTime(now), reservationID, model.ResConfirmed)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 事务内重读状态，给出准确失败原因（R19：对手操作刚赢）
		var cur string
		if qerr := tx.QueryRowContext(ctx, `SELECT status FROM reservations WHERE id=?`, reservationID).Scan(&cur); qerr == nil {
			return errf(CodeInvalidState, "核销失败：预约状态已变为 %s", cur)
		}
		return errf(CodeInvalidState, "核销失败：预约状态已变化")
	}
	if err := audit(ctx, tx, "employee", employeeID, "CHECK_IN", fmt.Sprintf("reservation:%d", reservationID), "", now); err != nil {
		return err
	}
	return tx.Commit()
}

// StartService 服务人员登记服务开始（R17）：已核销 → 服务中。
func StartService(ctx context.Context, db DBTX, reservationID, employeeID int64, now time.Time) error {
	return advanceService(ctx, db, reservationID, employeeID, model.RoleStaff,
		model.ResCheckedIn, model.ResInService, "service_started_at", "SERVICE_START", now)
}

// CompleteService 服务人员登记服务完成（R17/R18）：服务中 → 已完成。
// 完成即确认收入时点；条件更新保证重复完成不会重复记收入。
func CompleteService(ctx context.Context, db DBTX, reservationID, employeeID int64, now time.Time) error {
	return advanceService(ctx, db, reservationID, employeeID, model.RoleStaff,
		model.ResInService, model.ResCompleted, "service_finished_at", "SERVICE_COMPLETE", now)
}

func advanceService(ctx context.Context, db DBTX, reservationID, employeeID int64, wantRole, from, to, timeField, action string, now time.Time) error {
	empStore, err := checkEmployee(ctx, db, employeeID, wantRole)
	if err != nil {
		return err
	}
	resStore, status, err := reservationInfo(ctx, db, reservationID)
	if err != nil {
		return err
	}
	if resStore != empStore {
		return errf(CodePermission, "非本店员工无权操作本店预约")
	}
	if status != from {
		return errf(CodeInvalidState, "该操作要求预约处于 %s（当前 %s）", from, status)
	}

	tx, err := beginTx(ctx, db)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	q := fmt.Sprintf(`UPDATE reservations SET status=?, %s=?, updated_at=? WHERE id=? AND status=?`, timeField)
	res, err := tx.ExecContext(ctx, q, to, fmtTime(now), fmtTime(now), reservationID, from)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errf(CodeInvalidState, "预约状态已变化，操作失败")
	}
	if err := audit(ctx, tx, "employee", employeeID, action, fmt.Sprintf("reservation:%d", reservationID), "", now); err != nil {
		return err
	}
	return tx.Commit()
}
