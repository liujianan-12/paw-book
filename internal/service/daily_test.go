package service

import (
	"context"
	"testing"
	"time"

	"paw-book/internal/domain"
)

// —— R8.1/R8.2/R8.3 日清：五类计数、三金额、勾稽为 0 ——————————

func TestDailyReportFiveCategoriesAndReconciliation(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// 补齐 pets：4(u1) 5(u3) 6(u2)
	if _, err := db.Exec(`INSERT INTO pet(id, user_id, name, breed) VALUES
		(4, 1, '球球', '泰迪'), (5, 3, '大黄', '中华田园犬'), (6, 2, '雪球', '比熊')`); err != nil {
		t.Fatal(err)
	}

	slot1 := mustCreateSlot(t, db, 3*time.Hour, time.Hour, 3) // 09:00
	slot2 := mustCreateSlot(t, db, 5*time.Hour, time.Hour, 3) // 11:00
	slot3 := mustCreateSlot(t, db, 8*time.Hour, time.Hour, 3) // 14:00

	// rA：已完成（R5.2）
	rA := mustCreateReservation(t, db, slot1, 1, 1, testBase)
	pA := mustCreatePayment(t, db, rA.ID, 1, testBase)
	payNotify(t, db, "txn-A", pA.OrderNo, true, testBase.Add(time.Minute))
	if _, err := CheckIn(ctx, db, rA.ID, 2, testBase.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := StartService(ctx, db, rA.ID, 3, testBase.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := CompleteService(ctx, db, rA.ID, 3, testBase.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}

	// rB：取消退款成功
	rB := mustCreateReservation(t, db, slot2, 1, 2, testBase)
	pB := mustCreatePayment(t, db, rB.ID, 1, testBase)
	payNotify(t, db, "txn-B", pB.OrderNo, true, testBase.Add(time.Minute))
	cB, err := CancelReservation(ctx, db, rB.ID, 1, "", testBase.Add(10*time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := HandleRefundNotify(ctx, db, refundNoByID(t, db, cB.RefundID), true, "", testBase.Add(11*time.Minute)); err != nil {
		t.Fatal(err)
	}

	// rC：取消退款失败（待跟进）
	rC := mustCreateReservation(t, db, slot2, 2, 3, testBase)
	pC := mustCreatePayment(t, db, rC.ID, 2, testBase)
	payNotify(t, db, "txn-C", pC.OrderNo, true, testBase.Add(time.Minute))
	cC, err := CancelReservation(ctx, db, rC.ID, 2, "", testBase.Add(10*time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := HandleRefundNotify(ctx, db, refundNoByID(t, db, cC.RefundID), false, "", testBase.Add(11*time.Minute)); err != nil {
		t.Fatal(err)
	}

	// rD：未到店（R7.1）
	rD := mustCreateReservation(t, db, slot3, 1, 4, testBase)
	pD := mustCreatePayment(t, db, rD.ID, 1, testBase)
	payNotify(t, db, "txn-D", pD.OrderNo, true, testBase.Add(time.Minute))
	if err := MarkNoShow(ctx, db, rD.ID, 1, "客户未到场", testBase.Add(10*time.Hour)); err != nil {
		t.Fatal(err)
	}

	// rE：已确认未服务（占用款项）
	rE := mustCreateReservation(t, db, slot3, 3, 5, testBase)
	pE := mustCreatePayment(t, db, rE.ID, 3, testBase)
	payNotify(t, db, "txn-E", pE.OrderNo, true, testBase.Add(time.Minute))

	// rF：关闭后支付（已取消 + 异常退款待处理）
	rF := mustCreateReservation(t, db, slot3, 2, 6, testBase)
	pF := mustCreatePayment(t, db, rF.ID, 2, testBase)
	if err := CancelUnpaid(ctx, db, rF.ID, 2, testBase.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if res := payNotify(t, db, "txn-F", pF.OrderNo, true, testBase.Add(3*time.Minute)); res.Result != domain.NotifyAnomalyRefund {
		t.Fatalf("关闭后支付应异常退款，得到 %s", res.Result)
	}

	rep, err := GetDailyReport(ctx, db, 1, testBase)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Day != "2026-11-14" {
		t.Fatalf("日清归属日期错误: %s", rep.Day)
	}
	if len(rep.Slots) != 3 {
		t.Fatalf("应有 3 个时段，得到 %d", len(rep.Slots))
	}

	// 时段 1：已完成
	if s := rep.Slots[0]; s.ActiveCount != 1 || s.Completed != 1 || s.Cancelled != 0 || s.NoShow != 0 {
		t.Fatalf("时段1计数不符: %+v", s)
	}
	// 时段 2：两条取消，其中 1 条退款待处理
	if s := rep.Slots[1]; s.Cancelled != 2 || s.RefundPending != 1 || s.ActiveCount != 0 {
		t.Fatalf("时段2计数不符: %+v", s)
	}
	// 时段 3：未到店 / 已确认 / 已取消
	if s := rep.Slots[2]; s.NoShow != 1 || s.Cancelled != 1 || s.ActiveCount != 1 || s.RefundPending != 1 {
		t.Fatalf("时段3计数不符: %+v", s)
	}

	// 金额：6 笔成功收款 9900*6
	wantCollected := int64(6 * 9900)
	if rep.CollectedCents != wantCollected {
		t.Fatalf("已收款应为 %d，得到 %d", wantCollected, rep.CollectedCents)
	}
	if rep.CompletedIncomeCents != 9900 {
		t.Fatalf("已完成收入应为 9900，得到 %d", rep.CompletedIncomeCents)
	}
	if rep.RefundedCents != 9900 {
		t.Fatalf("已退款应为 9900，得到 %d", rep.RefundedCents)
	}
	if rep.RefundPendingCents != 2*9900 || rep.RefundPendingCount != 2 {
		t.Fatalf("待处理退款应为 19800/2 笔，得到 %d/%d", rep.RefundPendingCents, rep.RefundPendingCount)
	}
	if rep.NoShowHeldCents != 9900 {
		t.Fatalf("未到店保留应为 9900，得到 %d", rep.NoShowHeldCents)
	}
	if rep.ActiveHeldCents != 9900 {
		t.Fatalf("履约中占用应为 9900，得到 %d", rep.ActiveHeldCents)
	}
	// R8.3：已付款 ≠ 已完成；勾稽差额必须为 0（I6）
	if rep.CompletedIncomeCents == rep.CollectedCents {
		t.Fatal("不得把所有已付款都计作已完成服务")
	}
	if rep.UnaccountedCents != 0 {
		t.Fatalf("金额勾稽差额应为 0，得到 %d", rep.UnaccountedCents)
	}
	if rep.PaymentExceptionCount != 1 {
		t.Fatalf("支付异常应为 1 条，得到 %d", rep.PaymentExceptionCount)
	}

	// 别店无数据
	other, err := GetDailyReport(ctx, db, 2, testBase)
	if err != nil || len(other.Slots) != 0 || other.CollectedCents != 0 {
		t.Fatalf("别店日清应为空: %+v err=%v", other, err)
	}
}
