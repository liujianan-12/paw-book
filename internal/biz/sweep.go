package biz

import (
	"context"
	"fmt"
	"time"

	"paw-book/internal/model"
)

// SweepExpiredReservations 全局扫描：取消所有过期待支付预约（R4/R13/I4）。
// 由服务定时调用（双保险之一）；预约/支付路径内的懒清理是另一道保险。
// 返回本次取消的数量；幂等：重复扫描不会二次取消（条件里已排除非待支付状态）。
func SweepExpiredReservations(ctx context.Context, db DBTX, now time.Time) (int64, error) {
	res, err := db.ExecContext(ctx,
		`UPDATE reservations SET status=?, cancel_type=?, updated_at=? WHERE status=? AND pay_deadline<=?`,
		model.ResCancelled, model.CancelTimeout, fmtTime(now), model.ResPendingPayment, fmtTime(now))
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n > 0 {
		if err := audit(ctx, db, "system", 0, "SWEEP_EXPIRED", "reservations", fmt.Sprintf("取消 %d 条过期待支付预约", n), now); err != nil {
			return 0, err
		}
	}
	return n, nil
}
