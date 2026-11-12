package biz

import (
	"context"
	"testing"
	"time"
)

// 构造同一天的四种记录 + 一个履约中预约，验证日清统计与金额勾稽（R23/R24/S5）。
func TestDailyReportReconcilesMoney(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// 1) 已完成服务
	done := confirmedReservation(t, db, 8*time.Hour, 1, 1)
	if err := CheckIn(ctx, db, done.ID, 2, testBase); err != nil {
		t.Fatal(err)
	}
	if err := StartService(ctx, db, done.ID, 3, testBase.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := CompleteService(ctx, db, done.ID, 3, testBase.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	// 2) 已取消 + 退款成功
	cancelRefunded := confirmedReservation(t, db, 9*time.Hour, 1, 1)
	c1, err := CancelReservation(ctx, db, cancelRefunded.ID, 1, "", testBase.Add(time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := HandleRefundNotify(ctx, db, refundNoOf(t, db, c1.RefundID), true, "", testBase.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	// 3) 未到店（时段结束后由店长标记）
	noShow := confirmedReservation(t, db, 10*time.Hour, 1, 1)
	if err := MarkNoShow(ctx, db, noShow.ID, 1, "未到店未联系上", testBase.Add(12*time.Hour)); err != nil {
		t.Fatal(err)
	}

	// 4) 已取消 + 退款失败（待跟进）
	cancelFailed := confirmedReservation(t, db, 11*time.Hour, 2, 3)
	c2, err := CancelReservation(ctx, db, cancelFailed.ID, 2, "", testBase.Add(time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := HandleRefundNotify(ctx, db, refundNoOf(t, db, c2.RefundID), false, "渠道超时", testBase.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	// 5) 仍有效（已确认未到店时间）
	active := confirmedReservation(t, db, 12*time.Hour, 2, 3)
	_ = active

	rep, err := GetDailyReport(ctx, db, 1, testBase)
	if err != nil {
		t.Fatalf("日清查询失败: %v", err)
	}

	if len(rep.Slots) != 5 {
		t.Fatalf("当日应有 5 个时段，得到 %d", len(rep.Slots))
	}
	// 已收款 = 5 笔 × 9900
	if rep.CollectedCents != 5*9900 {
		t.Fatalf("已收款应为 49500 分，得到 %d", rep.CollectedCents)
	}
	// 金额分桶
	if rep.CompletedIncomeCents != 9900 {
		t.Fatalf("已完成收入应为 9900，得到 %d", rep.CompletedIncomeCents)
	}
	if rep.RefundedCents != 9900 {
		t.Fatalf("已退款应为 9900，得到 %d", rep.RefundedCents)
	}
	if rep.RefundPendingCents != 9900 || rep.RefundPendingCount != 1 {
		t.Fatalf("退款待处理应为 9900/1 笔，得到 %d/%d", rep.RefundPendingCents, rep.RefundPendingCount)
	}
	if rep.NoShowHeldCents != 9900 {
		t.Fatalf("未到店保留应为 9900，得到 %d", rep.NoShowHeldCents)
	}
	if rep.ActiveHeldCents != 9900 {
		t.Fatalf("履约中占用应为 9900，得到 %d", rep.ActiveHeldCents)
	}
	// 勾稽：已收款 = 各项之和，差额 0
	if rep.UnaccountedCents != 0 {
		t.Fatalf("金额勾稽差额应为 0，得到 %d", rep.UnaccountedCents)
	}
	// 关键反例：不能把所有已付款都算作已完成服务（R24）
	if rep.CompletedIncomeCents == rep.CollectedCents {
		t.Fatal("不得把全部收款计为已完成服务收入")
	}

	// 各时段状态统计（R23）
	byStatus := map[string]int{}
	for _, s := range rep.Slots {
		switch {
		case s.Completed > 0:
			byStatus["completed"]++
		case s.NoShow > 0:
			byStatus["noshow"]++
		case s.Cancelled > 0:
			byStatus["cancelled"]++
		case s.ActiveCount > 0:
			byStatus["active"]++
		}
	}
	if byStatus["completed"] != 1 || byStatus["noshow"] != 1 || byStatus["cancelled"] != 2 || byStatus["active"] != 1 {
		t.Fatalf("时段状态统计不符: %+v", byStatus)
	}
	// 有退款待处理的时段被单独标记
	pendingSlots := 0
	for _, s := range rep.Slots {
		if s.RefundPending > 0 {
			pendingSlots++
		}
	}
	if pendingSlots != 1 {
		t.Fatalf("应有 1 个时段存在退款待处理，得到 %d", pendingSlots)
	}
}

// 别的日期不应混入（按开始时间归属当天）。
func TestDailyReportScopedToDay(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	confirmedReservation(t, db, 8*time.Hour, 1, 1)

	nextDay, err := GetDailyReport(ctx, db, 1, testBase.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(nextDay.Slots) != 0 || nextDay.CollectedCents != 0 {
		t.Fatalf("次日应为空，得到 %d 个时段 %d 分", len(nextDay.Slots), nextDay.CollectedCents)
	}
}
