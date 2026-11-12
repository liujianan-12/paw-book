package biz

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"paw-book/internal/model"
)

// CreatePayment 为待支付预约发起一次支付（A4/R2.1/R2.7）。
// 每次调用生成新的支付流水与新 order_no；同一预约最多一条成功支付（uk_payment_success）。
func CreatePayment(ctx context.Context, db DBTX, reservationID, userID int64, now time.Time) (*model.Payment, error) {
	tx, err := beginTx(ctx, db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	var ownerID, price int64
	var status, expireAt string
	err = tx.QueryRowContext(ctx,
		`SELECT user_id, status, price_cents, pay_expire_at FROM reservation WHERE id=?`, reservationID).
		Scan(&ownerID, &status, &price, &expireAt)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && ownerID != userID) {
		return nil, errf(CodeNotFound, "预约不存在")
	}
	if err != nil {
		return nil, err
	}
	if status != model.ResPendingPayment {
		return nil, errf(CodePaymentNotPend, "仅待支付状态可发起支付（当前 %s）", status)
	}
	exp, err := parseTime(expireAt)
	if err != nil {
		return nil, err
	}
	if !now.Before(exp) {
		// 顺手把过期占位清理掉，保证名额账正确（R2.2）
		if _, err := expireReservationTx(ctx, tx, reservationID, now); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, errf(CodePaymentExpired, "支付超时，预约已释放名额")
	}

	var succeeded int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM payment WHERE reservation_id=? AND status=?`,
		reservationID, model.PaySuccess).Scan(&succeeded); err != nil {
		return nil, err
	}
	if succeeded > 0 {
		return nil, errf(CodePaymentNotPend, "该预约已支付成功")
	}
	var attempt int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM payment WHERE reservation_id=?`, reservationID).Scan(&attempt); err != nil {
		return nil, err
	}
	attempt++

	orderNo := fmt.Sprintf("P%d-%d", reservationID, now.UnixNano())
	ins, err := tx.ExecContext(ctx,
		`INSERT INTO payment(reservation_id, order_no, amount_cents, channel, status, created_at)
		 VALUES(?,?,?,?,?,?)`,
		reservationID, orderNo, price, "mock", model.PayInit, fmtTime(now))
	if err != nil {
		return nil, err
	}
	payID, err := ins.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := audit(ctx, tx, userID, "user", 0, "PAYMENT_CREATE", "payment", payID, "ok", orderNo, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &model.Payment{
		ID: payID, ReservationID: reservationID, OrderNo: orderNo, AmountCents: price,
		Channel: "mock", Status: model.PayInit, CreatedAt: now, Attempt: attempt,
	}, nil
}

// PayNotifyResult 支付回调处理结果（A5 返回体的 result 字段）。
type PayNotifyResult struct {
	ReservationID int64
	PaymentID     int64
	Result        string // CONFIRMED / DUPLICATE_IGNORED / ANOMALY_REFUND / REJECTED
	RefundID      int64
}

// HandlePayNotify 处理支付渠道回调（R2.4/R2.5/R2.6，幂等第一道闸为 channel_txn_id 唯一）。
// 成功且预约待支付未过期 → 确认；预约已关闭（过期/取消）→ 登记异常并退款，预约状态不变。
func HandlePayNotify(ctx context.Context, db DBTX, channelTxnID, orderNo string, success bool, now time.Time) (*PayNotifyResult, error) {
	tx, err := beginTx(ctx, db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	// R2.5：同一渠道流水只处理一次（幂等第一道闸）
	var seen int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM payment_notification WHERE channel_txn_id=?`, channelTxnID).Scan(&seen); err != nil {
		return nil, err
	}
	if seen > 0 {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &PayNotifyResult{Result: model.NotifyDuplicate}, nil
	}

	var payID, resID, amount int64
	var payStatus string
	err = tx.QueryRowContext(ctx,
		`SELECT id, reservation_id, amount_cents, status FROM payment WHERE order_no=?`, orderNo).
		Scan(&payID, &resID, &amount, &payStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errf(CodePaymentNotFound, "支付单不存在: %s", orderNo)
	}
	if err != nil {
		return nil, err
	}

	result := model.NotifyRejected
	var refundID int64

	switch {
	case !success:
		// R2.7：支付失败不改预约状态，仍为待支付，可再次发起支付
		if payStatus != model.PaySuccess {
			if _, err := tx.ExecContext(ctx, `UPDATE payment SET status=? WHERE id=?`,
				model.PayFailed, payID); err != nil {
				return nil, err
			}
		}
	case payStatus == model.PaySuccess:
		// 同一支付单重复成功通知：幂等
		result = model.NotifyDuplicate
	default:
		// 该预约是否已有另一条成功支付（不同支付流水重复成功）→ 不重复记收款
		var otherID int64
		qerr := tx.QueryRowContext(ctx,
			`SELECT id FROM payment WHERE reservation_id=? AND status=? AND id<>?`,
			resID, model.PaySuccess, payID).Scan(&otherID)
		if qerr == nil {
			result = model.NotifyDuplicate
		} else if !errors.Is(qerr, sql.ErrNoRows) {
			return nil, qerr
		} else {
			refundID, result, err = applyPaySuccess(ctx, tx, payID, resID, amount, channelTxnID, now)
			if err != nil {
				return nil, err
			}
		}
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO payment_notification(channel_txn_id, order_no, result, received_at) VALUES(?,?,?,?)`,
		channelTxnID, orderNo, result, fmtTime(now)); err != nil {
		return nil, err
	}
	if err := audit(ctx, tx, 0, "system", 0, "PAYMENT_NOTIFY", "payment", payID, result, orderNo, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &PayNotifyResult{ReservationID: resID, PaymentID: payID, Result: result, RefundID: refundID}, nil
}

// applyPaySuccess 在事务内落地一次新的支付成功（确认或异常退款）。
func applyPaySuccess(ctx context.Context, tx DBTX, payID, resID, amount int64, channelTxnID string, now time.Time) (int64, string, error) {
	// 先把可能已到期的占位清理为 EXPIRED（R2.2），再判断预约是否仍待支付
	if _, err := expireReservationTx(ctx, tx, resID, now); err != nil {
		return 0, "", err
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE reservation SET status=?, paid_at=?, updated_at=?
		 WHERE id=? AND status=? AND seat_released=0`,
		model.ResConfirmed, fmtTime(now), fmtTime(now), resID, model.ResPendingPayment)
	if err != nil {
		return 0, "", err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		// 正常确认：本支付流水成功，其余未完成尝试标记为被取代
		if _, err := tx.ExecContext(ctx,
			`UPDATE payment SET status=?, paid_at=?, provider_txn_id=? WHERE id=?`,
			model.PaySuccess, fmtTime(now), channelTxnID, payID); err != nil {
			return 0, "", err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE payment SET status=? WHERE reservation_id=? AND status=? AND id<>?`,
			model.PayFailed, resID, model.PayInit, payID); err != nil {
			return 0, "", err
		}
		return 0, model.NotifyConfirmed, nil
	}

	// 预约已关闭（已取消/已过期/其他终态）：登记异常 + 退款，预约状态不变（R2.6/I10）
	var resStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM reservation WHERE id=?`, resID).Scan(&resStatus); err != nil {
		return 0, "", err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE payment SET status=?, paid_at=?, provider_txn_id=? WHERE id=?`,
		model.PaySuccess, fmtTime(now), channelTxnID, payID); err != nil {
		return 0, "", err
	}
	detail := fmt.Sprintf("预约状态 %s 后收到支付成功通知", resStatus)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO payment_anomaly(reservation_id, payment_id, kind, detail, created_at) VALUES(?,?,?,?,?)`,
		resID, payID, model.ReasonLatePayment, detail, fmtTime(now)); err != nil {
		return 0, "", err
	}
	refundID, err := ensureRefund(ctx, tx, resID, payID, amount, model.ReasonLatePayment, now)
	if err != nil {
		return 0, "", err
	}
	return refundID, model.NotifyAnomalyRefund, nil
}
