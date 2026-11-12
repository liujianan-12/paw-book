package biz

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"paw-book/internal/model"
)

// MarkNoShow 店长在时段结束后把已确认预约标记为未到店（R7.1/R7.2）。
// 约束：本店店长、必填原因、时段已结束、仅已确认预约；不产生退款，已收款保留由日清核查。
func MarkNoShow(ctx context.Context, db DBTX, reservationID, staffID int64, reason string, now time.Time) error {
	storeID, err := checkStaff(ctx, db, staffID, model.RoleManager)
	if err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return errf(CodeNoShowNoReason, "标记未到店必须填写原因")
	}

	tx, err := beginTx(ctx, db)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	var resStore, slotID int64
	var status, endAt string
	err = tx.QueryRowContext(ctx,
		`SELECT r.store_id, r.slot_id, r.status, s.end_at FROM reservation r JOIN slot s ON s.id=r.slot_id WHERE r.id=?`,
		reservationID).Scan(&resStore, &slotID, &status, &endAt)
	if errors.Is(err, sql.ErrNoRows) {
		return errf(CodeNotFound, "预约不存在")
	}
	if err != nil {
		return err
	}
	if resStore != storeID {
		return errf(CodeStoreMismatch, "非本店员工无权操作本店预约")
	}
	if status != model.ResConfirmed {
		return errf(CodeNotNoShowable, "仅已确认预约可标记未到店（当前 %s）", status)
	}
	end, err := parseTime(endAt)
	if err != nil {
		return err
	}
	if now.Before(end) {
		return errf(CodeNoShowTooEarly, "时段尚未结束（结束于 %s），不能标记未到店", end.Format(time.RFC3339))
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE reservation SET status=?, no_show_at=?, no_show_reason=?, seat_released=1, updated_at=?
		 WHERE id=? AND status=? AND seat_released=0`,
		model.ResNoShow, fmtTime(now), reason, fmtTime(now), reservationID, model.ResConfirmed)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errf(CodeStateInvalid, "预约状态已变化，标记失败")
	}
	// 未到店不再占用名额（I1）：reserved_count-1，且 seat_released 保证只释放一次
	if _, err := tx.ExecContext(ctx,
		`UPDATE slot SET reserved_count=reserved_count-1 WHERE id=? AND reserved_count>0`, slotID); err != nil {
		return err
	}
	if err := audit(ctx, tx, staffID, model.RoleManager, storeID,
		"RESERVATION_NO_SHOW", "reservation", reservationID, "ok", reason, now); err != nil {
		return err
	}
	return tx.Commit()
}
