package biz

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"paw-book/internal/model"
)

// CreateSlotParams 店长发布时段参数（R1.1）。
type CreateSlotParams struct {
	ActorID  int64
	StoreID  int64
	ItemID   int64
	StartAt  time.Time
	EndAt    time.Time
	Capacity int
}

// CreateSlot 发布可预约时段：仅本店店长（R1.1/P3/P4），起止时间必填且开始于未来，容量 1–3（本题固定 3）。
func CreateSlot(ctx context.Context, db DBTX, p CreateSlotParams, now time.Time) (int64, error) {
	staffStore, err := checkStaff(ctx, db, p.ActorID, model.RoleManager)
	if err != nil {
		return 0, err
	}
	if staffStore != p.StoreID {
		return 0, errf(CodeStoreMismatch, "非本店店长无权发布本店时段")
	}
	if !p.EndAt.After(p.StartAt) {
		return 0, errf(CodeInvalidParam, "结束时间必须晚于开始时间")
	}
	if !p.StartAt.After(now) {
		return 0, errf(CodeInvalidParam, "开始时间必须在未来")
	}
	if p.Capacity < 1 || p.Capacity > 3 {
		return 0, errf(CodeInvalidParam, "每时段容量必须在 1–3 之间（本题固定 3）")
	}
	res, err := db.ExecContext(ctx,
		`INSERT INTO slot(store_id,item_id,start_at,end_at,capacity,reserved_count,created_by,created_at)
		 VALUES(?,?,?,?,?,0,?,?)`,
		p.StoreID, p.ItemID, fmtTime(p.StartAt), fmtTime(p.EndAt), p.Capacity, p.ActorID, fmtTime(now))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := audit(ctx, db, p.ActorID, model.RoleManager, p.StoreID, "SLOT_CREATE", "slot", id, "ok", "", now); err != nil {
		return 0, err
	}
	return id, nil
}

// SlotView 可约列表条目（A1）。
type SlotView struct {
	model.Slot
	ItemName  string
	Remaining int
}

// ListAvailableSlots 查询本店未来时段与剩余名额；查询前先做一次过期清理（R2.2 惰性路径）。
func ListAvailableSlots(ctx context.Context, db DBTX, storeID int64, now time.Time) ([]SlotView, error) {
	if _, err := SweepExpired(ctx, db, now); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT s.id, s.store_id, s.item_id, s.start_at, s.end_at, s.capacity, s.reserved_count, COALESCE(i.name,'')
		FROM slot s LEFT JOIN service_item i ON i.id = s.item_id
		WHERE s.store_id=? AND s.start_at > ? ORDER BY s.start_at`, storeID, fmtTime(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SlotView
	for rows.Next() {
		var v SlotView
		var startAt, endAt string
		if err := rows.Scan(&v.ID, &v.StoreID, &v.ItemID, &startAt, &endAt, &v.Capacity, &v.ReservedCount, &v.ItemName); err != nil {
			return nil, err
		}
		if v.StartAt, err = parseTime(startAt); err != nil {
			return nil, err
		}
		if v.EndAt, err = parseTime(endAt); err != nil {
			return nil, err
		}
		v.Remaining = v.Capacity - v.ReservedCount
		out = append(out, v)
	}
	return out, rows.Err()
}

// slotByID 读取时段（含名额账）。
func slotByID(ctx context.Context, db DBTX, slotID int64) (model.Slot, error) {
	var s model.Slot
	var startAt, endAt string
	err := db.QueryRowContext(ctx,
		`SELECT id, store_id, item_id, start_at, end_at, capacity, reserved_count FROM slot WHERE id=?`, slotID).
		Scan(&s.ID, &s.StoreID, &s.ItemID, &startAt, &endAt, &s.Capacity, &s.ReservedCount)
	if errors.Is(err, sql.ErrNoRows) {
		return s, errf(CodeSlotNotFound, "时段不存在")
	}
	if err != nil {
		return s, err
	}
	if s.StartAt, err = parseTime(startAt); err != nil {
		return s, err
	}
	if s.EndAt, err = parseTime(endAt); err != nil {
		return s, err
	}
	return s, nil
}

// 说明：名额释放统一走各业务事务内的条件更新（seat_released 0→1 + reserved_count-1），
// 保证 I5“只释放一次”，不再提供独立的 releaseSeat 入口以免绕过事务。
