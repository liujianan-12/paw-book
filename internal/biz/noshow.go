package biz

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"paw-book/internal/model"
)

// MarkNoShow 店长在时段结束后将已确认预约标记为未到店（R21/R22）。
// 规则：仅本店店长、必填原因、时段必须已结束、终态不可逆、本题约定不自动退款。
func MarkNoShow(ctx context.Context, db DBTX, reservationID, employeeID int64, reason string, now time.Time) error {
	empStore, err := checkEmployee(ctx, db, employeeID, model.RoleManager)
	if err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return errf(CodeInvalidParam, "标记未到店必须填写原因")
	}

	var resStore int64
	var status, endAt string
	err = db.QueryRowContext(ctx,
		`SELECT r.store_id, r.status, s.end_at FROM reservations r JOIN slots s ON s.id=r.slot_id WHERE r.id=?`,
		reservationID).Scan(&resStore, &status, &endAt)
	if errors.Is(err, sql.ErrNoRows) {
		return errf(CodeNotFound, "预约不存在")
	}
	if err != nil {
		return err
	}
	if resStore != empStore {
		return errf(CodePermission, "非本店员工无权操作本店预约")
	}
	if status != model.ResConfirmed {
		return errf(CodeInvalidState, "仅已确认预约可标记未到店（当前 %s）", status)
	}
	end, err := parseTime(endAt)
	if err != nil {
		return err
	}
	if now.Before(end) {
		return errf(CodeInvalidState, "时段尚未结束（结束于 %s），不能标记未到店", end.Format(time.RFC3339))
	}

	tx, err := beginTx(ctx, db)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx,
		`UPDATE reservations SET status=?, no_show_reason=?, updated_at=? WHERE id=? AND status=?`,
		model.ResNoShow, reason, fmtTime(now), reservationID, model.ResConfirmed)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errf(CodeInvalidState, "预约状态已变化，标记失败")
	}
	if err := audit(ctx, tx, "employee", employeeID, "MARK_NO_SHOW", fmt.Sprintf("reservation:%d", reservationID), reason, now); err != nil {
		return err
	}
	return tx.Commit()
}
