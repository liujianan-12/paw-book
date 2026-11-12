package biz

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"paw-book/internal/model"
)

// reservationStoreStatus 读取预约所属门店与当前状态（越权与状态分类用）。
func reservationStoreStatus(ctx context.Context, db DBTX, reservationID int64) (int64, string, error) {
	var storeID int64
	var status string
	err := db.QueryRowContext(ctx,
		`SELECT store_id, status FROM reservation WHERE id=?`, reservationID).Scan(&storeID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", errf(CodeNotFound, "预约不存在")
	}
	if err != nil {
		return 0, "", err
	}
	return storeID, status, nil
}

// hasPendingCancel 是否存在待处理取消申请（R3.3：待审期间不得核销）。
func hasPendingCancel(ctx context.Context, db DBTX, reservationID int64) (bool, error) {
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM cancel_request WHERE reservation_id=? AND status=?`,
		reservationID, model.CancelReqPending).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// CheckIn 前台核销到店（R3.3/R5.1/R5.3/R6.1）。
// 返回 alreadyDone=true 表示该预约此前已核销（幂等成功，不新增记录）。
func CheckIn(ctx context.Context, db DBTX, reservationID, staffID int64, now time.Time) (bool, error) {
	storeID, err := checkStaff(ctx, db, staffID, model.RoleReception)
	if err != nil {
		return false, err
	}

	tx, err := beginTx(ctx, db)
	if err != nil {
		return false, err
	}
	defer tx.Rollback() //nolint:errcheck

	resStore, status, err := reservationStoreStatus(ctx, tx, reservationID)
	if err != nil {
		return false, err
	}
	if resStore != storeID {
		return false, errf(CodeStoreMismatch, "非本店员工无权核销本店预约")
	}
	switch status {
	case model.ResCancellationReview:
		return false, errf(CodeCheckinPending, "预约存在待处理取消申请，不可核销")
	case model.ResCheckedIn:
		return true, nil // R5.3：重复核销幂等
	case model.ResConfirmed:
		pending, err := hasPendingCancel(ctx, tx, reservationID)
		if err != nil {
			return false, err
		}
		if pending {
			return false, errf(CodeCheckinPending, "预约存在待处理取消申请，不可核销")
		}
	default:
		return false, errf(CodeCheckinNotAllow, "仅已确认预约可核销（当前 %s）", status)
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE reservation SET status=?, checked_in_at=?, updated_at=?
		 WHERE id=? AND status=? AND seat_released=0`,
		model.ResCheckedIn, fmtTime(now), fmtTime(now), reservationID, model.ResConfirmed)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var cur string
		if qerr := tx.QueryRowContext(ctx, `SELECT status FROM reservation WHERE id=?`, reservationID).Scan(&cur); qerr == nil {
			if cur == model.ResCheckedIn {
				return true, nil
			}
			return false, errf(CodeCheckinNotAllow, "核销失败：预约状态已变为 %s", cur)
		}
		return false, errf(CodeStateInvalid, "核销失败：预约状态已变化")
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO checkin_record(reservation_id, staff_id, checked_at) VALUES(?,?,?)
		 ON CONFLICT(reservation_id) DO NOTHING`,
		reservationID, staffID, fmtTime(now)); err != nil {
		return false, err
	}
	if err := audit(ctx, tx, staffID, model.RoleReception, storeID,
		"RESERVATION_CHECKIN", "reservation", reservationID, "ok", "", now); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return false, nil
}

// StartService 服务人员登记服务开始（R5.2）：已到店 → 服务中。
func StartService(ctx context.Context, db DBTX, reservationID, staffID int64, now time.Time) (bool, error) {
	storeID, err := checkStaff(ctx, db, staffID, model.RoleServer)
	if err != nil {
		return false, err
	}

	tx, err := beginTx(ctx, db)
	if err != nil {
		return false, err
	}
	defer tx.Rollback() //nolint:errcheck

	resStore, status, err := reservationStoreStatus(ctx, tx, reservationID)
	if err != nil {
		return false, err
	}
	if resStore != storeID {
		return false, errf(CodeStoreMismatch, "非本店员工无权操作本店预约")
	}
	switch status {
	case model.ResInService, model.ResCompleted:
		return true, nil // 重复开始幂等
	case model.ResCheckedIn:
	default:
		return false, errf(CodeServiceNotIn, "仅已到店预约可开始服务（当前 %s）", status)
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE reservation SET status=?, started_at=?, updated_at=? WHERE id=? AND status=?`,
		model.ResInService, fmtTime(now), fmtTime(now), reservationID, model.ResCheckedIn)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, errf(CodeStateInvalid, "预约状态已变化，开始服务失败")
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO service_record(reservation_id, started_at, started_by) VALUES(?,?,?)
		 ON CONFLICT(reservation_id) DO UPDATE SET
		   started_at=COALESCE(service_record.started_at, excluded.started_at),
		   started_by=COALESCE(service_record.started_by, excluded.started_by)`,
		reservationID, fmtTime(now), staffID); err != nil {
		return false, err
	}
	if err := audit(ctx, tx, staffID, model.RoleServer, storeID,
		"SERVICE_START", "reservation", reservationID, "ok", "", now); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return false, nil
}

// CompleteService 服务人员登记服务完成（R5.2/R5.3）：服务中 → 已完成。
// 完成即唯一收入确认时点；重复完成幂等，不重复记收入。
func CompleteService(ctx context.Context, db DBTX, reservationID, staffID int64, now time.Time) (bool, error) {
	storeID, err := checkStaff(ctx, db, staffID, model.RoleServer)
	if err != nil {
		return false, err
	}

	tx, err := beginTx(ctx, db)
	if err != nil {
		return false, err
	}
	defer tx.Rollback() //nolint:errcheck

	resStore, status, err := reservationStoreStatus(ctx, tx, reservationID)
	if err != nil {
		return false, err
	}
	if resStore != storeID {
		return false, errf(CodeStoreMismatch, "非本店员工无权操作本店预约")
	}
	if status == model.ResCompleted {
		return true, nil // R5.3：重复完成幂等
	}
	if status != model.ResInService {
		return false, errf(CodeServiceNotStart, "仅服务中预约可登记完成（当前 %s）", status)
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE reservation SET status=?, completed_at=?, updated_at=? WHERE id=? AND status=?`,
		model.ResCompleted, fmtTime(now), fmtTime(now), reservationID, model.ResInService)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, errf(CodeStateInvalid, "预约状态已变化，完成服务失败")
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO service_record(reservation_id, completed_at, completed_by) VALUES(?,?,?)
		 ON CONFLICT(reservation_id) DO UPDATE SET
		   completed_at=COALESCE(service_record.completed_at, excluded.completed_at),
		   completed_by=COALESCE(service_record.completed_by, excluded.completed_by)`,
		reservationID, fmtTime(now), staffID); err != nil {
		return false, err
	}
	if err := audit(ctx, tx, staffID, model.RoleServer, storeID,
		"SERVICE_COMPLETE", "reservation", reservationID, "ok", "", now); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return false, nil
}
