package biz

import (
	"context"
	"time"

	"paw-book/internal/model"
)

// DayReservation 日清明细行。
type DayReservation struct {
	ID           int64
	PetName      string
	Status       string
	AmountCents  int64
	CancelType   string
	NoShowReason string
	RefundStatus string // 空表示无退款
	RefundCents  int64
	RefundFailed bool
}

// DaySlot 日清单时段视图。
type DaySlot struct {
	SlotID        int64
	StartAt       string
	EndAt         string
	ActiveCount   int // 有效预约数（占用名额口径）
	Completed     int
	Cancelled     int
	NoShow        int
	RefundPending int // 该时段退款待处理笔数
	Reservations  []DayReservation
}

// DailyReport 店长日清报告（R23/R24）。
// 金额勾稽：已收款 = 已完成收入 + 已退款 + 退款待处理 + 未到店保留 + 履约中占用，
// UnaccountedCents 必须为 0，证明"没有把所有已付款都算作已完成服务"。
type DailyReport struct {
	StoreID               int64
	Day                   string // yyyy-mm-dd
	Slots                 []DaySlot
	CollectedCents        int64 // 支付成功合计
	CompletedIncomeCents  int64 // 已完成服务收入
	RefundedCents         int64 // 已退款成功
	RefundPendingCents    int64 // 退款待处理（含失败，未退回）
	NoShowHeldCents       int64 // 未到店保留（本题约定不退款）
	ActiveHeldCents       int64 // 履约中/已确认占用
	UnaccountedCents      int64 // 勾稽差额（应为 0）
	RefundPendingCount    int
	PaymentExceptionCount int
}

// GetDailyReport 汇总门店某一天（按时段开始时间归属）的经营数据。
func GetDailyReport(ctx context.Context, db DBTX, storeID int64, day time.Time) (*DailyReport, error) {
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.Add(24 * time.Hour)
	rep := &DailyReport{StoreID: storeID, Day: dayStart.Format("2006-01-02")}

	// 1. 当日时段
	slotRows, err := db.QueryContext(ctx,
		`SELECT id, start_at, end_at FROM slots WHERE store_id=? AND start_at>=? AND start_at<? ORDER BY start_at`,
		storeID, fmtTime(dayStart), fmtTime(dayEnd))
	if err != nil {
		return nil, err
	}
	slotIndex := map[int64]int{}
	for slotRows.Next() {
		var s DaySlot
		if err := slotRows.Scan(&s.SlotID, &s.StartAt, &s.EndAt); err != nil {
			slotRows.Close()
			return nil, err
		}
		slotIndex[s.SlotID] = len(rep.Slots)
		rep.Slots = append(rep.Slots, s)
	}
	slotRows.Close()
	if err := slotRows.Err(); err != nil {
		return nil, err
	}

	// 2. 当日预约
	resRows, err := db.QueryContext(ctx,
		`SELECT r.id, r.slot_id, r.status, r.amount_cents, p.name, COALESCE(r.cancel_type,''), COALESCE(r.no_show_reason,'')
		 FROM reservations r
		 JOIN pets p ON p.id=r.pet_id
		 JOIN slots s ON s.id=r.slot_id
		 WHERE r.store_id=? AND s.start_at>=? AND s.start_at<? ORDER BY r.id`,
		storeID, fmtTime(dayStart), fmtTime(dayEnd))
	if err != nil {
		return nil, err
	}
	var all []DayReservation
	var slotOf = map[int64]int64{}
	for resRows.Next() {
		var dr DayReservation
		var slotID int64
		if err := resRows.Scan(&dr.ID, &slotID, &dr.Status, &dr.AmountCents, &dr.PetName, &dr.CancelType, &dr.NoShowReason); err != nil {
			resRows.Close()
			return nil, err
		}
		slotOf[dr.ID] = slotID
		all = append(all, dr)
	}
	resRows.Close()
	if err := resRows.Err(); err != nil {
		return nil, err
	}

	// 3. 退款单（按预约归集）
	refRows, err := db.QueryContext(ctx,
		`SELECT f.reservation_id, f.status, f.amount_cents FROM refunds f
		 JOIN reservations r ON r.id=f.reservation_id
		 JOIN slots s ON s.id=r.slot_id
		 WHERE r.store_id=? AND s.start_at>=? AND s.start_at<?`,
		storeID, fmtTime(dayStart), fmtTime(dayEnd))
	if err != nil {
		return nil, err
	}
	for refRows.Next() {
		var resID int64
		var status string
		var amount int64
		if err := refRows.Scan(&resID, &status, &amount); err != nil {
			refRows.Close()
			return nil, err
		}
		// 逐条归集到对应预约（多笔取最后状态）
		for i := range all {
			if all[i].ID == resID {
				all[i].RefundStatus = status
				all[i].RefundCents = amount
				all[i].RefundFailed = status == model.RefundFailed
				break
			}
		}
	}
	refRows.Close()

	// 4. 已收款与支付异常
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(p.amount_cents),0) FROM payments p
		 JOIN reservations r ON r.id=p.reservation_id
		 JOIN slots s ON s.id=r.slot_id
		 WHERE r.store_id=? AND p.status=? AND s.start_at>=? AND s.start_at<?`,
		storeID, model.PaySuccess, fmtTime(dayStart), fmtTime(dayEnd)).Scan(&rep.CollectedCents); err != nil {
		return nil, err
	}
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM payment_exceptions e
		 JOIN reservations r ON r.id=e.reservation_id
		 JOIN slots s ON s.id=r.slot_id
		 WHERE r.store_id=? AND s.start_at>=? AND s.start_at<?`,
		storeID, fmtTime(dayStart), fmtTime(dayEnd)).Scan(&rep.PaymentExceptionCount); err != nil {
		return nil, err
	}

	// 5. 汇总与勾稽
	for i := range all {
		dr := &all[i]
		idx, ok := slotIndex[slotOf[dr.ID]]
		if ok {
			s := &rep.Slots[idx]
			s.Reservations = append(s.Reservations, *dr)
			switch {
			case model.OccupiesSlot(dr.Status):
				s.ActiveCount++
			}
			switch dr.Status {
			case model.ResCompleted:
				s.Completed++
			case model.ResCancelled:
				s.Cancelled++
			case model.ResNoShow:
				s.NoShow++
			}
		}

		// 金额分桶
		switch dr.Status {
		case model.ResCompleted:
			rep.CompletedIncomeCents += dr.AmountCents
		case model.ResNoShow:
			rep.NoShowHeldCents += dr.AmountCents
		case model.ResConfirmed, model.ResCheckedIn, model.ResInService:
			rep.ActiveHeldCents += dr.AmountCents
		}
		// 退款桶（取消退款与支付异常退款都在这）
		if dr.RefundStatus != "" {
			if dr.RefundStatus == model.RefundSuccess {
				rep.RefundedCents += dr.RefundCents
			} else {
				rep.RefundPendingCents += dr.RefundCents
				rep.RefundPendingCount++
				if ok {
					rep.Slots[idx].RefundPending++
				}
			}
		}
	}
	rep.UnaccountedCents = rep.CollectedCents - (rep.CompletedIncomeCents + rep.RefundedCents +
		rep.RefundPendingCents + rep.NoShowHeldCents + rep.ActiveHeldCents)
	return rep, nil
}
