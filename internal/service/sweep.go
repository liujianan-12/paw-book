package service

import (
	"context"
	"strconv"
	"time"

	"paw-book/internal/domain"
	"paw-book/internal/repo"
)

// SweepExpired 全局扫描：取消超时未支付预约并释放名额（R2.2，幂等）。
// 惰性路径（创建预约/查询时段）与定时路径共用本函数。
func SweepExpired(ctx context.Context, db repo.DBTX, now time.Time) (int64, error) {
	tx, err := repo.BeginTx(ctx, db)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck

	n, err := expireRowsTx(ctx, tx, "", now, nil)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		if err := repo.Audit(ctx, tx, 0, "system", 0, "SWEEP_EXPIRED", "reservation", 0, "ok",
			"取消过期预约 "+strconv.FormatInt(n, 10)+" 条", now); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

// expireSlotTx 在调用方事务内清理单个时段的过期占位（创建预约前调用，R2.2）。
func expireSlotTx(ctx context.Context, tx repo.DBTX, slotID int64, now time.Time) (int64, error) {
	return expireRowsTx(ctx, tx, " AND slot_id=?", now, []any{slotID})
}

// expireReservationTx 在调用方事务内过期单条预约（支付回调前调用，支撑 R2.6）。
// 返回是否真的发生了 EXPIRED 迁移；重复调用安全（seat_released 保证只释放一次）。
func expireReservationTx(ctx context.Context, tx repo.DBTX, reservationID int64, now time.Time) (bool, error) {
	n, err := expireRowsTx(ctx, tx, " AND id=?", now, []any{reservationID})
	return n > 0, err
}

// expireRowsTx 条件更新过期预约；affected=1 时才释放名额，保证只释放一次（I5）。
func expireRowsTx(ctx context.Context, tx repo.DBTX, extraCond string, now time.Time, extraArgs []any) (int64, error) {
	args := []any{domain.ResPendingPayment, repo.FmtTime(now)}
	args = append(args, extraArgs...)
	rows, err := tx.QueryContext(ctx,
		`SELECT id, slot_id FROM reservation
		 WHERE status=? AND pay_expire_at<=? AND seat_released=0`+extraCond, args...)
	if err != nil {
		return 0, err
	}
	type item struct{ id, slotID int64 }
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.slotID); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	var n int64
	for _, it := range items {
		res, err := tx.ExecContext(ctx,
			`UPDATE reservation SET status=?, cancel_type=?, cancelled_at=?, seat_released=1, updated_at=?
			 WHERE id=? AND status=? AND seat_released=0`,
			domain.ResExpired, domain.CancelExpired, repo.FmtTime(now), repo.FmtTime(now), it.id, domain.ResPendingPayment)
		if err != nil {
			return 0, err
		}
		if aff, _ := res.RowsAffected(); aff == 1 {
			if _, err := tx.ExecContext(ctx,
				`UPDATE slot SET reserved_count=reserved_count-1 WHERE id=? AND reserved_count>0`, it.slotID); err != nil {
				return 0, err
			}
			n++
		}
	}
	return n, nil
}
