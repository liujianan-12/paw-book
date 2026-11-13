package service

// T02 独立测试会话新增（Test Agent）：R2.2 占位边界、R2.6 关闭后支付、R7 未到店口径复验。

import (
	"context"
	"testing"
	"time"

	"paw-book/internal/domain"
)

// —— R2.1/R2.2 占位 5 分钟边界与“只释放一次” ——————————
func TestT02R22_PayHoldBoundaryAndSingleRelease(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)

	if !r.PayExpireAt.Equal(testBase.Add(5 * time.Minute)) {
		t.Fatalf("R2.1 占位截止应为创建 +5m，实际 %v", r.PayExpireAt)
	}
	if got := reservedCount(t, db, slotID); got != 1 {
		t.Fatalf("R2.1 创建后名额应 +1，实际 %d", got)
	}

	// 4m59s：不过期
	if n, err := SweepExpired(ctx, db, testBase.Add(5*time.Minute-time.Second)); err != nil {
		t.Fatalf("扫描失败: %v", err)
	} else if n != 0 {
		t.Fatalf("R2.2 4m59s 不应过期，实际过期 %d 条", n)
	}
	if st, _ := reservationStatus(t, db, r.ID); st != domain.ResPendingPayment {
		t.Fatalf("R2.2 4m59s 状态应仍为待支付，实际 %s", st)
	}
	if got := reservedCount(t, db, slotID); got != 1 {
		t.Fatalf("R2.2 4m59s 名额应仍占用，实际 %d", got)
	}

	// 5m：过期并释放
	if n, err := SweepExpired(ctx, db, testBase.Add(5*time.Minute)); err != nil {
		t.Fatalf("扫描失败: %v", err)
	} else if n != 1 {
		t.Fatalf("R2.2 5m 应过期 1 条，实际 %d", n)
	}
	if st, _ := reservationStatus(t, db, r.ID); st != domain.ResExpired {
		t.Fatalf("R2.2 5m 状态应为 EXPIRED，实际 %s", st)
	}
	if got := reservedCount(t, db, slotID); got != 0 {
		t.Fatalf("R2.2 过期应释放名额，实际 %d", got)
	}

	// 重复扫描：不得二次释放（名额不得变负）
	for i := 0; i < 3; i++ {
		if n, err := SweepExpired(ctx, db, testBase.Add(10*time.Minute)); err != nil {
			t.Fatalf("重复扫描失败: %v", err)
		} else if n != 0 {
			t.Fatalf("R2.2 第 %d 次重复扫描不应再释放，实际 %d 条", i+1, n)
		}
	}
	if got := reservedCount(t, db, slotID); got != 0 {
		t.Fatalf("R2.2 重复扫描导致名额异常：reservedCount=%d", got)
	}
	if n := t02Count(t, db, `SELECT COUNT(*) FROM reservation WHERE slot_id=? AND status='EXPIRED'`, slotID); n != 1 {
		t.Fatalf("R2.2 应只有 1 条过期决策，实际 %d", n)
	}
	// 名额释放后他人可约（R1.3 名额循环）
	mustCreateReservation(t, db, slotID, 2, 3, testBase.Add(10*time.Minute))
	if got := reservedCount(t, db, slotID); got != 1 {
		t.Fatalf("R1.3 释放后新用户应可预约，reservedCount=%d", got)
	}
}

// —— R2.6 关闭后收到支付成功：状态不变 + 异常登记 + 退款（LATE_PAYMENT） ——
func TestT02R26_LatePaymentAfterExpireKeepsStatusAndRefunds(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)

	// 在占位窗口内发起支付但渠道未回调，占位超时释放
	p, err := CreatePayment(ctx, db, r.ID, 1, testBase)
	if err != nil {
		t.Fatalf("发起支付失败: %v", err)
	}
	if _, err := SweepExpired(ctx, db, testBase.Add(5*time.Minute)); err != nil {
		t.Fatalf("过期扫描失败: %v", err)
	}
	if st, _ := reservationStatus(t, db, r.ID); st != domain.ResExpired {
		t.Fatalf("前置：预约应已 EXPIRED，实际 %s", st)
	}

	// 关闭后收到支付成功
	res, err := HandlePayNotify(ctx, db, "t02-late-txn", p.OrderNo, true, testBase.Add(6*time.Minute))
	if err != nil {
		t.Fatalf("关闭后支付回调处理失败: %v", err)
	}
	if res.Result != domain.NotifyAnomalyRefund {
		t.Fatalf("R2.6 关闭后支付应返回 ANOMALY_REFUND，实际 %s", res.Result)
	}
	if st, _ := reservationStatus(t, db, r.ID); st != domain.ResExpired {
		t.Fatalf("R2.6 预约状态不得恢复，实际 %s", st)
	}
	if n := t02Count(t, db, `SELECT COUNT(*) FROM payment_anomaly WHERE reservation_id=? AND kind=?`, r.ID, domain.ReasonLatePayment); n != 1 {
		t.Fatalf("R2.6 应登记 1 条 LATE_PAYMENT 异常，实际 %d", n)
	}
	var reason string
	var amount int64
	if err := db.QueryRow(`SELECT reason, amount_cents FROM refund WHERE reservation_id=?`, r.ID).Scan(&reason, &amount); err != nil {
		t.Fatalf("R2.6 应生成退款单: %v", err)
	}
	if reason != domain.ReasonLatePayment || amount != 9900 {
		t.Fatalf("R2.6 退款应为 LATE_PAYMENT/9900，实际 %s/%d", reason, amount)
	}
	if got := reservedCount(t, db, slotID); got != 0 {
		t.Fatalf("R2.6 关闭后支付不得重新占名额，reservedCount=%d", got)
	}
}

// —— R7.1/R7.2/R7.3 未到店：时段结束前拒绝、无原因拒绝、结束后成功、不退款、名额只释放一次 ——
func TestT02R7_NoShowRules(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3) // 起 06:02 止 07:00? 见下
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	t02Pay(t, db, r.ID, 1, testBase, "r7")

	var startAt, endAt string
	if err := db.QueryRow(`SELECT start_at, end_at FROM slot WHERE id=?`, slotID).Scan(&startAt, &endAt); err != nil {
		t.Fatalf("查询时段失败: %v", err)
	}
	start, _ := time.Parse(time.RFC3339, startAt)
	end, _ := time.Parse(time.RFC3339, endAt)
	t.Logf("时段：%s ~ %s", start.Format(time.RFC3339), end.Format(time.RFC3339))

	// 时段未结束 → 拒绝
	err := MarkNoShow(ctx, db, r.ID, 1, "未到店", end.Add(-time.Minute))
	wantBizCode(t, err, domain.CodeNoShowTooEarly)
	// 已结束但无原因 → 拒绝
	err = MarkNoShow(ctx, db, r.ID, 1, "  ", end)
	wantBizCode(t, err, domain.CodeNoShowNoReason)
	// 非店长 → 拒绝（P4：前台不能代标记）
	err = MarkNoShow(ctx, db, r.ID, 2, "未到店", end)
	wantBizCode(t, err, domain.CodeRoleForbidden)

	if err := MarkNoShow(ctx, db, r.ID, 1, "客户未到店", end); err != nil {
		t.Fatalf("R7.1 时段结束后应可标记未到店，实际错误: %v", err)
	}
	st, _ := reservationStatus(t, db, r.ID)
	if st != domain.ResNoShow {
		t.Fatalf("R7.1 状态应为 NO_SHOW，实际 %s", st)
	}
	if got := reservedCount(t, db, slotID); got != 0 {
		t.Fatalf("R7.1/D7 未到店应释放名额一次，reservedCount=%d", got)
	}
	if n := t02Count(t, db, `SELECT COUNT(*) FROM refund WHERE reservation_id=?`, r.ID); n != 0 {
		t.Fatalf("R7.2 未到店不得自动退款，实际 %d 张退款单", n)
	}
	// 不可撤销/不可重复
	if err := MarkNoShow(ctx, db, r.ID, 1, "再次标记", end); err == nil {
		t.Fatal("R7.1 已标记未到店不应允许重复标记成功")
	}
	if got := reservedCount(t, db, slotID); got != 0 {
		t.Fatalf("重复标记导致名额异常：reservedCount=%d", got)
	}
	// R7.3 用户可查看未到店结果与原因（脱敏展示）
	d, err := GetReservation(ctx, db, r.ID, 1)
	if err != nil {
		t.Fatalf("R7.3 用户查询预约失败: %v", err)
	}
	if d.Status != domain.ResNoShow || d.NoShowReason == "" {
		t.Fatalf("R7.3 用户应看到未到店状态与原因，实际 status=%s reason=%q", d.Status, d.NoShowReason)
	}
	// R8 日清：未到店单独列示且勾稽差为 0
	rep, err := GetDailyReport(ctx, db, 1, start)
	if err != nil {
		t.Fatalf("日清查询失败: %v", err)
	}
	var noShowCount int
	for _, s := range rep.Slots {
		noShowCount += s.NoShow
	}
	if noShowCount != 1 {
		t.Fatalf("R8.1 日清未到店计数应为 1，实际 %d（明细 %+v）", noShowCount, rep.Slots)
	}
	if rep.NoShowHeldCents != 9900 {
		t.Fatalf("R7.2/R8.2 未到店保留金额应为 9900，实际 %d", rep.NoShowHeldCents)
	}
	if rep.CompletedIncomeCents != 0 {
		t.Fatalf("R8.3 未到店不得计入已完成服务收入，实际 %d", rep.CompletedIncomeCents)
	}
	if rep.UnaccountedCents != 0 {
		t.Fatalf("R8.2 勾稽差额应为 0，实际 %d", rep.UnaccountedCents)
	}
	if rep.RefundPendingCount != 0 {
		t.Fatalf("R7.2 未到店不应产生待处理退款，实际 %d 笔", rep.RefundPendingCount)
	}
}
