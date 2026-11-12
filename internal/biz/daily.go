package biz

import (
	"context"
	"time"

	"paw-book/internal/model"
)

// DayReservation 日清明细行（可下钻到预约/退款单据）。
type DayReservation struct {
	ID           int64
	PetName      string
	Status       string
	AmountCents  int64
	CancelType   string
	NoShowReason string
	RefundID     int64
	RefundStatus string // 空表示无退款
	RefundCents  int64
	RefundFailed bool
}

// DaySlot 日清单时段视图。
type DaySlot struct {
	SlotID        int64
	StartAt       string
	EndAt         string
	ActiveCount   int // 有效预约数（占名额口径）
	Completed     int
	Cancelled     int
	NoShow        int
	RefundPending int // 该时段退款待处理笔数
	Reservations  []DayReservation
}

// DailyReport 店长日清报告（R8.1/R8.2/R8.3）。
// 金额勾稽：已收款 = 已完成收入 + 已退款 + 退款待处理 + 未到店保留 + 履约中占用，
// UnaccountedCents 必须为 0，证明"没有把所有已付款都算作已完成服务"。
type DailyReport struct {
	StoreID               int64
	Day                   string // yyyy-mm-dd
	Slots                 []DaySlot
	CollectedCents        int64 // 支付成功合计
	CompletedIncomeCents  int64 // 已完成服务收入（收入确认口径）
	RefundedCents         int64 // 已退款成功
	RefundPendingCents    int64 // 退款待处理（含失败，未退回）
	NoShowHeldCents       int64 // 未到店保留（本题约定不退款）
	ActiveHeldCents       int64 // 履约中/已确认/待审核占用
	UnaccountedCents      int64 // 勾稽差额（应为 0）
	RefundPendingCount    int
	PaymentExceptionCount int
}

// GetDailyReport 汇总门店某一天（按时段开始时间归属，A1）的经营数据。
func GetDailyReport(ctx context.Context, db DBTX, storeID int64, day time.Time) (*DailyReport, error) {
	// 过期未支付预约先释放名额，保证日清与名额账一致（R2.2/I1）
	if _, err := SweepExpired(ctx, db, day); err != nil {
		return nil, err
	}

	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.Add(24 * time.Hour)
	rep := &DailyReport{StoreID: storeID, Day: dayStart.Format("2006-01-02")}

	// 1. 当日时段
	slotRows, err := db.QueryContext(ctx,
		`SELECT id, start_at, end_at FROM slot WHERE store_id=? AND start_at>=? AND start_at<? ORDER BY start_at`,
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
		`SELECT r.id, r.slot_id, r.status, r.price_cents, p.name, COALESCE(r.cancel_type,''), COALESCE(r.no_show_reason,'')
		 FROM reservation r
		 JOIN pet p ON p.id=r.pet_id
		 JOIN slot s ON s.id=r.slot_id
		 WHERE r.store_id=? AND s.start_at>=? AND s.start_at<? ORDER BY r.id`,
		storeID, fmtTime(dayStart), fmtTime(dayEnd))
	if err != nil {
		return nil, err
	}
	var all []DayReservation
	slotOf := map[int64]int64{}
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
	byRes := make(map[int64]int, len(all))
	for i := range all {
		byRes[all[i].ID] = i
	}

	// 3. 退款单（一预约一条，I8）
	refRows, err := db.QueryContext(ctx,
		`SELECT f.id, f.reservation_id, f.status, f.amount_cents FROM refund f
		 JOIN reservation r ON r.id=f.reservation_id
		 JOIN slot s ON s.id=r.slot_id
		 WHERE r.store_id=? AND s.start_at>=? AND s.start_at<?`,
		storeID, fmtTime(dayStart), fmtTime(dayEnd))
	if err != nil {
		return nil, err
	}
	for refRows.Next() {
		var refundID, resID, amount int64
		var status string
		if err := refRows.Scan(&refundID, &resID, &status, &amount); err != nil {
			refRows.Close()
			return nil, err
		}
		if i, ok := byRes[resID]; ok {
			all[i].RefundID = refundID
			all[i].RefundStatus = status
			all[i].RefundCents = amount
			all[i].RefundFailed = status == model.RefundFailed
		}
	}
	refRows.Close()
	if err := refRows.Err(); err != nil {
		return nil, err
	}

	// 4. 已收款与支付异常（I6/I10）
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(p.amount_cents),0) FROM payment p
		 JOIN reservation r ON r.id=p.reservation_id
		 JOIN slot s ON s.id=r.slot_id
		 WHERE r.store_id=? AND p.status=? AND s.start_at>=? AND s.start_at<?`,
		storeID, model.PaySuccess, fmtTime(dayStart), fmtTime(dayEnd)).Scan(&rep.CollectedCents); err != nil {
		return nil, err
	}
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM payment_anomaly a
		 JOIN reservation r ON r.id=a.reservation_id
		 JOIN slot s ON s.id=r.slot_id
		 WHERE r.store_id=? AND s.start_at>=? AND s.start_at<?`,
		storeID, fmtTime(dayStart), fmtTime(dayEnd)).Scan(&rep.PaymentExceptionCount); err != nil {
		return nil, err
	}

	// 5. 分组计数与金额分桶
	for i := range all {
		dr := &all[i]
		idx, ok := slotIndex[slotOf[dr.ID]]
		if ok {
			s := &rep.Slots[idx]
			s.Reservations = append(s.Reservations, *dr)
			if model.OccupiesSlot(dr.Status) {
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

		switch dr.Status {
		case model.ResCompleted:
			rep.CompletedIncomeCents += dr.AmountCents
		case model.ResNoShow:
			rep.NoShowHeldCents += dr.AmountCents
		case model.ResConfirmed, model.ResCancellationReview, model.ResCheckedIn, model.ResInService:
			rep.ActiveHeldCents += dr.AmountCents
		}

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
