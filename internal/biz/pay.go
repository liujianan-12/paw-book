package biz

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"paw-book/internal/model"
)

// CreatePayment 为预约发起一次支付，生成新支付单等待渠道回调（R6 前置）。
// 仅限本人 + 待支付状态；渠道失败/超时后可再次发起（每次新单号）。
func CreatePayment(ctx context.Context, db DBTX, reservationID, userID int64, now time.Time) (*model.Payment, error) {
	tx, err := beginTx(ctx, db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	var ownerID, amount int64
	var status, deadline string
	err = tx.QueryRowContext(ctx,
		`SELECT user_id, status, amount_cents, pay_deadline FROM reservations WHERE id=?`, reservationID).
		Scan(&ownerID, &status, &amount, &deadline)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errf(CodeNotFound, "预约不存在")
	}
	if err != nil {
		return nil, err
	}
	if ownerID != userID {
		return nil, errf(CodePermission, "只能为本人预约发起支付")
	}
	if status != model.ResPendingPayment {
		return nil, errf(CodeInvalidState, "仅待支付状态可发起支付（当前 %s）", status)
	}
	// 超时口径与回调一致：过了占位截止时间就不再接受新支付单（回调侧仍会兜底清理）
	if dl, err := parseTime(deadline); err != nil {
		return nil, err
	} else if !now.Before(dl) {
		return nil, errf(CodeInvalidState, "支付超时，预约已释放名额")
	}

	outTradeNo := fmt.Sprintf("P%d-%d", reservationID, now.UnixNano())
	res, err := tx.ExecContext(ctx,
		`INSERT INTO payments(reservation_id, out_trade_no, amount_cents, status, created_at) VALUES(?,?,?,?,?)`,
		reservationID, outTradeNo, amount, model.PayCreated, fmtTime(now))
	if err != nil {
		return nil, err
	}
	payID, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := audit(ctx, tx, "user", userID, "PAYMENT_CREATE", fmt.Sprintf("payment:%d", payID), outTradeNo, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &model.Payment{
		ID:            payID,
		ReservationID: reservationID,
		OutTradeNo:    outTradeNo,
		AmountCents:   amount,
		Status:        model.PayCreated,
		CreatedAt:     now,
	}, nil
}

// PayNotifyResult 支付回调处理结果摘要（供页面/日志展示）。
type PayNotifyResult struct {
	ReservationID int64
	PaymentID     int64
	Confirmed     bool  // 本次回调将预约转为已确认
	Duplicated    bool  // 重复成功通知：幂等忽略（R7）
	Exception     bool  // 预约已关闭：登记支付异常并创建退款（R8）
	RefundID      int64 // 伴随创建的退款单
}

// HandlePayNotify 处理支付渠道回调（R6/R7/R8/R9）。
// 规则：
//   - 失败通知：支付单记 FAILED，预约不前进（R9）；
//   - 重复成功通知：幂等忽略，不重复确认（R7）；
//   - 成功且预约仍待支付：转为已确认（R6）；
//   - 成功但预约已关闭（用户取消/超时取消/已确认后二次付款）：
//     登记支付异常并创建退款单，不恢复预约（R8）。
func HandlePayNotify(ctx context.Context, db DBTX, outTradeNo string, success bool, now time.Time) (*PayNotifyResult, error) {
	tx, err := beginTx(ctx, db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	var payID, resID, amount int64
	var payStatus string
	err = tx.QueryRowContext(ctx,
		`SELECT id, reservation_id, amount_cents, status FROM payments WHERE out_trade_no=?`, outTradeNo).
		Scan(&payID, &resID, &amount, &payStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errf(CodeNotFound, "支付单不存在: %s", outTradeNo)
	}
	if err != nil {
		return nil, err
	}

	if !success {
		// R9：渠道失败——仅记录支付单失败，预约状态不动
		if payStatus == model.PayCreated {
			if _, err := tx.ExecContext(ctx, `UPDATE payments SET status=?, notified_at=? WHERE id=?`,
				model.PayFailed, fmtTime(now), payID); err != nil {
				return nil, err
			}
			if err := audit(ctx, tx, "system", 0, "PAYMENT_FAIL", fmt.Sprintf("payment:%d", payID), outTradeNo, now); err != nil {
				return nil, err
			}
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &PayNotifyResult{ReservationID: resID, PaymentID: payID}, nil
	}

	// 成功通知：已处理过则幂等忽略（R7）
	if payStatus == model.PaySuccess {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &PayNotifyResult{ReservationID: resID, PaymentID: payID, Duplicated: true}, nil
	}

	// 先按当前时间清理该预约的过期占位（关闭口径包含超时取消）
	if err := sweepExpiredReservation(ctx, tx, resID, now); err != nil {
		return nil, err
	}

	// 支付单记成功
	if _, err := tx.ExecContext(ctx, `UPDATE payments SET status=?, notified_at=? WHERE id=?`,
		model.PaySuccess, fmtTime(now), payID); err != nil {
		return nil, err
	}

	var resStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM reservations WHERE id=?`, resID).Scan(&resStatus); err != nil {
		return nil, err
	}

	// R6：待支付 → 已确认
	if resStatus == model.ResPendingPayment {
		if _, err := tx.ExecContext(ctx, `UPDATE reservations SET status=?, updated_at=? WHERE id=?`,
			model.ResConfirmed, fmtTime(now), resID); err != nil {
			return nil, err
		}
		if err := audit(ctx, tx, "system", 0, "PAYMENT_CONFIRM", fmt.Sprintf("reservation:%d", resID), outTradeNo, now); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &PayNotifyResult{ReservationID: resID, PaymentID: payID, Confirmed: true}, nil
	}

	// R8：预约已关闭 → 登记支付异常 + 创建退款（不恢复预约）
	excRes, err := tx.ExecContext(ctx,
		`INSERT INTO payment_exceptions(reservation_id, payment_id, amount_cents, reason, created_at) VALUES(?,?,?,?,?)`,
		resID, payID, amount, fmt.Sprintf("预约已关闭（状态 %s）后收到支付成功通知", resStatus), fmtTime(now))
	if err != nil {
		return nil, err
	}
	excID, err := excRes.LastInsertId()
	if err != nil {
		return nil, err
	}
	refundID, err := newRefund(ctx, tx, resID, payID, amount, "预约已关闭后的支付成功退款", now)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_exceptions SET refund_id=? WHERE id=?`, refundID, excID); err != nil {
		return nil, err
	}
	if err := audit(ctx, tx, "system", 0, "PAYMENT_EXCEPTION", fmt.Sprintf("payment:%d", payID), outTradeNo, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &PayNotifyResult{ReservationID: resID, PaymentID: payID, Exception: true, RefundID: refundID}, nil
}

// sweepExpiredReservation 清理单个预约的过期占位（供回调路径使用）。
func sweepExpiredReservation(ctx context.Context, db DBTX, reservationID int64, now time.Time) error {
	_, err := db.ExecContext(ctx,
		`UPDATE reservations SET status=?, cancel_type=?, updated_at=? WHERE id=? AND status=? AND pay_deadline<=?`,
		model.ResCancelled, model.CancelTimeout, fmtTime(now), reservationID, model.ResPendingPayment, fmtTime(now))
	return err
}
