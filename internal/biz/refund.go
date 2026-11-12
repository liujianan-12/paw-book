package biz

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"paw-book/internal/model"
)

// newRefund 创建退款单（状态待退款）；取消退款与支付异常退款共用此通道（A4）。
// 退款单独立于预约状态存在与流转（R14/I6）。
func newRefund(ctx context.Context, db DBTX, reservationID, paymentID, amount int64, reason string, now time.Time) (int64, error) {
	refundNo := fmt.Sprintf("R%d-%d", reservationID, now.UnixNano())
	res, err := db.ExecContext(ctx,
		`INSERT INTO refunds(refund_no, reservation_id, payment_id, amount_cents, status, retry_count, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?)`,
		refundNo, reservationID, paymentID, amount, model.RefundPending, 0, fmtTime(now), fmtTime(now))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// RefundNotifyResult 退款回调处理结果摘要。
type RefundNotifyResult struct {
	RefundID   int64
	Success    bool // 本次回调退款成功
	Failed     bool // 本次回调退款失败（预约状态不受影响，I6）
	Duplicated bool // 重复通知：幂等忽略
}

// HandleRefundNotify 处理退款渠道回调（R14/I6）。
// 退款失败不改变预约终态；重复成功通知幂等。
func HandleRefundNotify(ctx context.Context, db DBTX, refundNo string, success bool, failReason string, now time.Time) (*RefundNotifyResult, error) {
	tx, err := beginTx(ctx, db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	var refundID int64
	var status string
	err = tx.QueryRowContext(ctx, `SELECT id, status FROM refunds WHERE refund_no=?`, refundNo).Scan(&refundID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errf(CodeNotFound, "退款单不存在: %s", refundNo)
	}
	if err != nil {
		return nil, err
	}

	// 幂等：已成功的退款单忽略后续通知
	if status == model.RefundSuccess {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &RefundNotifyResult{RefundID: refundID, Duplicated: true}, nil
	}

	if success {
		if _, err := tx.ExecContext(ctx, `UPDATE refunds SET status=?, updated_at=? WHERE id=?`,
			model.RefundSuccess, fmtTime(now), refundID); err != nil {
			return nil, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE refunds SET status=?, fail_reason=?, retry_count=retry_count+1, updated_at=? WHERE id=?`,
			model.RefundFailed, failReason, fmtTime(now), refundID); err != nil {
			return nil, err
		}
	}
	if err := audit(ctx, tx, "system", 0, "REFUND_NOTIFY", fmt.Sprintf("refund:%d", refundID), fmt.Sprintf("success=%v reason=%s", success, failReason), now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &RefundNotifyResult{RefundID: refundID, Success: success, Failed: !success}, nil
}
