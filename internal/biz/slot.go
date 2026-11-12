package biz

import (
	"context"
	"fmt"
	"time"

	"paw-book/internal/model"
)

// 占用名额的状态集合 SQL 片段（假设 A1）；过期未付款的预约不计入（R4 懒释放）。
var countActiveSQL = fmt.Sprintf(
	`SELECT COUNT(*) FROM reservations WHERE slot_id=? AND status IN ('%s','%s','%s','%s','%s') AND NOT (status='%s' AND pay_deadline<=?)`,
	model.ResPendingPayment, model.ResConfirmed, model.ResPendingReview, model.ResCheckedIn, model.ResInService, model.ResPendingPayment)

// countActive 统计时段当前有效预约数（名额占用口径）。
func countActive(ctx context.Context, db DBTX, slotID int64, now time.Time) (int, error) {
	var n int
	if err := db.QueryRowContext(ctx, countActiveSQL, slotID, fmtTime(now)).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// countActiveForPet 统计某宠物在某时段的有效预约数（同宠物同时段唯一，R2）。
func countActiveForPet(ctx context.Context, db DBTX, slotID, petID int64, now time.Time) (int, error) {
	q := countActiveSQL + " AND pet_id=?"
	var n int
	if err := db.QueryRowContext(ctx, q, slotID, fmtTime(now), petID).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// CreateSlotParams 店长发布时段参数（R1）。
type CreateSlotParams struct {
	ActorID        int64 // 操作的店长员工 ID
	StoreID        int64
	ServiceID      int64
	StartAt, EndAt time.Time
	Capacity       int
}

// CreateSlot 店长发布可预约时段：必须明确起止时间且开始于未来（R1）。
func CreateSlot(ctx context.Context, db DBTX, p CreateSlotParams, now time.Time) (int64, error) {
	if !p.EndAt.After(p.StartAt) {
		return 0, errf(CodeInvalidParam, "结束时间必须晚于开始时间")
	}
	if !p.StartAt.After(now) {
		return 0, errf(CodeInvalidParam, "开始时间必须在未来")
	}
	if p.Capacity < 1 || p.Capacity > 3 {
		return 0, errf(CodeInvalidParam, "每时段容量必须在 1–3 之间（本题固定为 3）")
	}
	res, err := db.ExecContext(ctx,
		`INSERT INTO slots(store_id, service_id, start_at, end_at, capacity) VALUES(?,?,?,?,?)`,
		p.StoreID, p.ServiceID, fmtTime(p.StartAt), fmtTime(p.EndAt), p.Capacity)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := audit(ctx, db, "employee", p.ActorID, "SLOT_CREATE", fmt.Sprintf("slot:%d", id), fmt.Sprintf("容量 %d", p.Capacity), now); err != nil {
		return 0, err
	}
	return id, nil
}

// SlotView 可约列表条目：时段 + 名额占用情况。
type SlotView struct {
	model.Slot
	ServiceName string
	ActiveCount int // 有效预约数
	Remaining   int // 剩余名额
}

// ListAvailableSlots 查询门店未来时段及剩余名额（供用户选择，R3）。
func ListAvailableSlots(ctx context.Context, db DBTX, storeID int64, now time.Time) ([]SlotView, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT s.id, s.store_id, s.service_id, s.start_at, s.end_at, s.capacity, COALESCE(v.name,'')
		FROM slots s LEFT JOIN services v ON v.id = s.service_id
		WHERE s.store_id=? AND s.start_at > ? ORDER BY s.start_at`, storeID, fmtTime(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SlotView
	for rows.Next() {
		var v SlotView
		var startAt, endAt string
		if err := rows.Scan(&v.ID, &v.StoreID, &v.ServiceID, &startAt, &endAt, &v.Capacity, &v.ServiceName); err != nil {
			return nil, err
		}
		if v.StartAt, err = parseTime(startAt); err != nil {
			return nil, err
		}
		if v.EndAt, err = parseTime(endAt); err != nil {
			return nil, err
		}
		n, err := countActive(ctx, db, v.ID, now)
		if err != nil {
			return nil, err
		}
		v.ActiveCount = n
		v.Remaining = v.Capacity - n
		out = append(out, v)
	}
	return out, rows.Err()
}

// sweepExpiredForSlot 懒清理：取消该时段内已过期的待支付预约并释放名额（R4）。
// 与 T6 的全局定时扫描使用同一语义；本函数在预约/查询事务内调用。
func sweepExpiredForSlot(ctx context.Context, db DBTX, slotID int64, now time.Time) error {
	_, err := db.ExecContext(ctx,
		`UPDATE reservations SET status=?, cancel_type=?, updated_at=? WHERE slot_id=? AND status=? AND pay_deadline<=?`,
		model.ResCancelled, model.CancelTimeout, fmtTime(now), slotID, model.ResPendingPayment, fmtTime(now))
	return err
}
