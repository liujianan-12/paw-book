package biz

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"paw-book/internal/model"
)

// ensureRefund 创建退款单；同一预约只有一条（I8），已存在则复用。
func ensureRefund(ctx context.Context, db DBTX, reservationID, paymentID, amount int64, reason string, now time.Time) (int64, error) {
	var id int64
	err := db.QueryRowContext(ctx, `SELECT id FROM refund WHERE reservation_id=?`, reservationID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	refundNo := fmt.Sprintf("R%d-%d", reservationID, now.UnixNano())
	res, err := db.ExecContext(ctx, `
		INSERT INTO refund(refund_no, payment_id, reservation_id, amount_cents, reason, status, retry_count, created_at, updated_at)
		VALUES(?,?,?,?,?,?,0,?,?)`,
		refundNo, paymentID, reservationID, amount, reason, model.RefundPending, fmtTime(now), fmtTime(now))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// RefundNotifyResult 退款回调结果。
type RefundNotifyResult struct {
	RefundID int64
	Result   string // SUCCESS / FAILED / DUPLICATE_IGNORED
}

// HandleRefundNotify 处理退款回调（R4.3/R4.4）。
// 退款成功/失败只影响退款单，不改变预约状态（I6）。
func HandleRefundNotify(ctx context.Context, db DBTX, refundNo string, success bool, failReason string, now time.Time) (*RefundNotifyResult, error) {
	tx, err := beginTx(ctx, db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	var refundID int64
	var status string
	err = tx.QueryRowContext(ctx, `SELECT id, status FROM refund WHERE refund_no=?`, refundNo).Scan(&refundID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errf(CodeNotFound, "退款单不存在: %s", refundNo)
	}
	if err != nil {
		return nil, err
	}
	if status == model.RefundSuccess {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &RefundNotifyResult{RefundID: refundID, Result: model.NotifyDuplicate}, nil
	}

	result := model.RefundFailed
	if success {
		result = model.RefundSuccess
		if _, err := tx.ExecContext(ctx, `UPDATE refund SET status=?, updated_at=?, last_error=NULL WHERE id=?`,
			model.RefundSuccess, fmtTime(now), refundID); err != nil {
			return nil, err
		}
	} else {
		if failReason == "" {
			failReason = model.ReasonChannelError
		}
		if _, err := tx.ExecContext(ctx, `UPDATE refund SET status=?, last_error=?, updated_at=? WHERE id=?`,
			model.RefundFailed, failReason, fmtTime(now), refundID); err != nil {
			return nil, err
		}
	}
	if err := audit(ctx, tx, 0, "system", 0, "REFUND_NOTIFY", "refund", refundID, result, failReason, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &RefundNotifyResult{RefundID: refundID, Result: result}, nil
}

// RetryRefund 客服重试退款（R4.4）：仅 FAILED/PENDING 可重试，retry_count+1。
// 返回退款单号，供调用方向模拟渠道重新发起退款。
func RetryRefund(ctx context.Context, db DBTX, refundID, employeeID int64, now time.Time) (string, error) {
	storeID, err := checkStaff(ctx, db, employeeID, model.RoleCS)
	if err != nil {
		return "", err
	}

	tx, err := beginTx(ctx, db)
	if err != nil {
		return "", err
	}
	defer tx.Rollback() //nolint:errcheck

	var refundNo string
	var status string
	var resStore int64
	err = tx.QueryRowContext(ctx,
		`SELECT f.refund_no, f.status, r.store_id FROM refund f JOIN reservation r ON r.id=f.reservation_id WHERE f.id=?`,
		refundID).Scan(&refundNo, &status, &resStore)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errf(CodeNotFound, "退款单不存在")
	}
	if err != nil {
		return "", err
	}
	if resStore != storeID {
		return "", errf(CodeStoreMismatch, "非本店员工无权操作本店退款")
	}
	if status != model.RefundFailed && status != model.RefundPending {
		return "", errf(CodeRefundNotRetry, "仅失败或待处理的退款可重试（当前 %s）", status)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE refund SET status=?, retry_count=retry_count+1, updated_at=? WHERE id=?`,
		model.RefundProcessing, fmtTime(now), refundID); err != nil {
		return "", err
	}
	if err := audit(ctx, tx, employeeID, model.RoleCS, storeID, "REFUND_RETRY", "refund", refundID, "ok", "", now); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return refundNo, nil
}

// RefundView 退款列表条目（A15）。
type RefundView struct {
	ID            int64
	RefundNo      string
	ReservationID int64
	AmountCents   int64
	Reason        string
	Status        string
	RetryCount    int
	LastError     string
	PetName       string
	SlotStart     string
}

// ListRefunds 查询本店退款（onlyUnfinished=true 时只返回 PENDING/PROCESSING/FAILED）。
func ListRefunds(ctx context.Context, db DBTX, storeID int64, onlyUnfinished bool) ([]RefundView, error) {
	cond := ""
	if onlyUnfinished {
		cond = " AND f.status IN ('PENDING','PROCESSING','FAILED')"
	}
	rows, err := db.QueryContext(ctx, `
		SELECT f.id, f.refund_no, f.reservation_id, f.amount_cents, f.reason, f.status,
		       f.retry_count, COALESCE(f.last_error,''), p.name, s.start_at
		FROM refund f
		JOIN reservation r ON r.id = f.reservation_id
		JOIN slot s ON s.id = r.slot_id
		JOIN pet p ON p.id = r.pet_id
		WHERE r.store_id=?`+cond+` ORDER BY f.created_at DESC`, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RefundView
	for rows.Next() {
		var v RefundView
		if err := rows.Scan(&v.ID, &v.RefundNo, &v.ReservationID, &v.AmountCents, &v.Reason, &v.Status,
			&v.RetryCount, &v.LastError, &v.PetName, &v.SlotStart); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
