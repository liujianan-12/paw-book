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

// CancelResult 取消申请的处理结果。
type CancelResult struct {
	AutoAccepted bool  // 距开始≥2小时：已自动受理并发起退款（R10）
	ReviewID     int64 // 距开始<2小时：进入客服审核（R11）
	RefundID     int64 // 自动受理时创建的退款单
}

// CancelReservation 用户对已确认预约发起取消（R10/R11/R15）。
// 距开始 ≥ cancelCutoff：自动受理并创建全额退款，名额释放；
// 距开始 < cancelCutoff：必须填写原因，进入取消审核中（名额保留、禁止核销）。
func CancelReservation(ctx context.Context, db DBTX, reservationID, userID int64, reason string, now time.Time, cancelCutoff time.Duration) (*CancelResult, error) {
	tx, err := beginTx(ctx, db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	var ownerID, slotID int64
	var status, startAt string
	err = tx.QueryRowContext(ctx,
		`SELECT r.user_id, r.slot_id, r.status, s.start_at FROM reservations r JOIN slots s ON s.id=r.slot_id WHERE r.id=?`,
		reservationID).Scan(&ownerID, &slotID, &status, &startAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errf(CodeNotFound, "预约不存在")
	}
	if err != nil {
		return nil, err
	}
	if ownerID != userID {
		return nil, errf(CodePermission, "只能取消本人的预约")
	}
	// R15：仅已确认状态可发起本题取消流程（待支付走主动取消，履约中不可取消）
	if status != model.ResConfirmed {
		return nil, errf(CodeInvalidState, "仅已确认预约可取消（当前 %s）", status)
	}

	start, err := parseTime(startAt)
	if err != nil {
		return nil, err
	}
	reason = strings.TrimSpace(reason)

	if start.Sub(now) >= cancelCutoff {
		// R10：提前取消——自动受理 + 全额退款 + 名额释放
		res, err := tx.ExecContext(ctx,
			`UPDATE reservations SET status=?, cancel_type=?, cancel_reason=?, updated_at=? WHERE id=? AND status=?`,
			model.ResCancelled, model.CancelEarly, reason, fmtTime(now), reservationID, model.ResConfirmed)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, errf(CodeInvalidState, "预约状态已变化，取消失败")
		}
		refundID, err := refundForReservation(ctx, tx, reservationID, "提前取消全额退款", now)
		if err != nil {
			return nil, err
		}
		if err := audit(ctx, tx, "user", userID, "RESERVATION_CANCEL_EARLY", fmt.Sprintf("reservation:%d", reservationID), "", now); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &CancelResult{AutoAccepted: true, RefundID: refundID}, nil
	}

	// R11：临近开始——必须填写原因，进入审核
	if reason == "" {
		return nil, errf(CodeInvalidParam, "距开始不足 %v，取消须填写原因由客服审核", cancelCutoff)
	}
	rvRes, err := tx.ExecContext(ctx,
		`INSERT INTO cancel_reviews(reservation_id, user_id, reason, status, created_at) VALUES(?,?,?,?,?)`,
		reservationID, userID, reason, model.ReviewPending, fmtTime(now))
	if err != nil {
		return nil, err
	}
	reviewID, err := rvRes.LastInsertId()
	if err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE reservations SET status=?, cancel_reason=?, updated_at=? WHERE id=? AND status=?`,
		model.ResPendingReview, reason, fmtTime(now), reservationID, model.ResConfirmed)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, errf(CodeInvalidState, "预约状态已变化，申请失败")
	}
	if err := audit(ctx, tx, "user", userID, "RESERVATION_CANCEL_REQUEST", fmt.Sprintf("reservation:%d", reservationID), reason, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &CancelResult{ReviewID: reviewID}, nil
}

// ReviewCancel 客服审核临近开始的取消申请（R12）。
// 批准：预约取消（审核批准）+ 全额退款 + 名额释放；
// 拒绝：预约恢复已确认，名额不变。
// 仅本店客服可操作（R20）。
func ReviewCancel(ctx context.Context, db DBTX, reviewID, employeeID int64, approve bool, now time.Time) error {
	tx, err := beginTx(ctx, db)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	// R20：员工角色与本店校验
	var empStore int64
	var empRole string
	err = tx.QueryRowContext(ctx, `SELECT store_id, role FROM employees WHERE id=?`, employeeID).Scan(&empStore, &empRole)
	if errors.Is(err, sql.ErrNoRows) {
		return errf(CodeNotFound, "员工不存在")
	}
	if err != nil {
		return err
	}
	if empRole != model.RoleCustomerService {
		return errf(CodePermission, "仅客服可审核取消申请（当前角色 %s）", empRole)
	}

	var resID, resStore int64
	var reviewStatus, resStatus string
	err = tx.QueryRowContext(ctx,
		`SELECT cr.reservation_id, cr.status, r.store_id, r.status FROM cancel_reviews cr JOIN reservations r ON r.id=cr.reservation_id WHERE cr.id=?`,
		reviewID).Scan(&resID, &reviewStatus, &resStore, &resStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return errf(CodeNotFound, "取消审核单不存在")
	}
	if err != nil {
		return err
	}
	if empStore != resStore {
		return errf(CodePermission, "非本店员工无权操作本店预约")
	}
	if reviewStatus != model.ReviewPending {
		return errf(CodeInvalidState, "该审核已处理（%s）", reviewStatus)
	}
	// R15 复核：预约仍处于取消审核中（若已被核销等则不能继续走取消流程）
	if resStatus != model.ResPendingReview {
		return errf(CodeInvalidState, "预约状态已变化（%s），无法继续取消流程", resStatus)
	}

	decision := model.ReviewRejected
	if approve {
		decision = model.ReviewApproved
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE cancel_reviews SET status=?, decided_by=?, decided_at=? WHERE id=?`,
		decision, employeeID, fmtTime(now), reviewID); err != nil {
		return err
	}

	if approve {
		// 批准：取消 + 退款 + 名额释放
		if _, err := tx.ExecContext(ctx,
			`UPDATE reservations SET status=?, cancel_type=?, updated_at=? WHERE id=? AND status=?`,
			model.ResCancelled, model.CancelApproved, fmtTime(now), resID, model.ResPendingReview); err != nil {
			return err
		}
		if _, err := refundForReservation(ctx, tx, resID, "客服批准取消的全额退款", now); err != nil {
			return err
		}
	} else {
		// 拒绝：恢复已确认，名额不变（R12）
		if _, err := tx.ExecContext(ctx,
			`UPDATE reservations SET status=?, updated_at=? WHERE id=? AND status=?`,
			model.ResConfirmed, fmtTime(now), resID, model.ResPendingReview); err != nil {
			return err
		}
	}
	if err := audit(ctx, tx, "employee", employeeID, "CANCEL_REVIEW_DECIDED", fmt.Sprintf("review:%d", reviewID), map[bool]string{true: "批准", false: "拒绝"}[approve], now); err != nil {
		return err
	}
	return tx.Commit()
}

// refundForReservation 为预约的最近一笔成功支付创建退款单（A4：取消退款与异常退款同一通道）。
func refundForReservation(ctx context.Context, db DBTX, reservationID int64, reason string, now time.Time) (int64, error) {
	var payID, amount int64
	err := db.QueryRowContext(ctx,
		`SELECT id, amount_cents FROM payments WHERE reservation_id=? AND status=? ORDER BY id DESC LIMIT 1`,
		reservationID, model.PaySuccess).Scan(&payID, &amount)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errf(CodeInvalidState, "预约无成功支付记录，无法创建退款")
	}
	if err != nil {
		return 0, err
	}
	return newRefund(ctx, db, reservationID, payID, amount, reason, now)
}
