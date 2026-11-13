package service

// T02 独立测试会话新增（Test Agent，依据 .agent/01 R1–R8、.agent/05、docs/test-plan.md）。
// 目的：不复用 Dev 结论，独立复验关键行为；本文件只含测试代码，不修改任何生产代码。

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"paw-book/internal/domain"
)

const t02Cutoff = 2 * time.Hour

// t02Pay 发起支付并置为成功（R2.4 前置），返回支付单。
func t02Pay(t *testing.T, db *sql.DB, resID, userID int64, now time.Time, tag string) *domain.Payment {
	t.Helper()
	p, err := CreatePayment(context.Background(), db, resID, userID, now)
	if err != nil {
		t.Fatalf("发起支付失败: %v", err)
	}
	res, err := HandlePayNotify(context.Background(), db, fmt.Sprintf("t02-%s-%s", tag, p.OrderNo), p.OrderNo, true, now)
	if err != nil {
		t.Fatalf("支付回调失败: %v", err)
	}
	if res.Result != domain.NotifyConfirmed {
		t.Fatalf("支付回调结果应为 CONFIRMED，实际 %s", res.Result)
	}
	return p
}

func t02RefundRow(t *testing.T, db *sql.DB, resID int64) (id int64, refundNo, status string, amount int64, retry int) {
	t.Helper()
	err := db.QueryRow(`SELECT id, refund_no, status, amount_cents, retry_count FROM refund WHERE reservation_id=?`, resID).
		Scan(&id, &refundNo, &status, &amount, &retry)
	if err != nil {
		t.Fatalf("查询退款单(预约 %d): %v", resID, err)
	}
	return
}

// —— R3.1 边界：距开始恰好 2h（=2h）应自动受理并全额退款 ——————————
func TestT02R31_CancelAtExactTwoHoursIsAutoAccepted(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	t02Pay(t, db, r.ID, 1, testBase, "r31")

	start := testBase.Add(2 * time.Hour)
	at := start.Add(-t02Cutoff) // 恰好 = 2h，按 05 §2 口径归“自动受理”
	if !at.Equal(testBase) {
		t.Fatalf("测试前置时间计算错误: %v", at)
	}

	out, err := CancelReservation(context.Background(), db, r.ID, 1, "", at, t02Cutoff)
	if err != nil {
		t.Fatalf("R3.1 边界 =2h 取消应成功，实际错误: %v", err)
	}
	if !out.AutoAccepted {
		t.Fatalf("R3.1 边界 =2h 应自动受理，实际走审核（requestID=%d）", out.RequestID)
	}
	if out.RefundID == 0 {
		t.Fatal("R3.1 自动受理必须创建退款单")
	}
	_, _, rStatus, rAmount, _ := t02RefundRow(t, db, r.ID)
	if rAmount != 9900 {
		t.Fatalf("R3.1 应全额退款 9900 分，实际 %d", rAmount)
	}
	if rStatus != domain.RefundPending {
		t.Fatalf("R3.1 退款初始状态应为 PENDING，实际 %s", rStatus)
	}
	status, cancelType := reservationStatus(t, db, r.ID)
	if status != domain.ResCancelled || cancelType != domain.CancelAutoBefore2H {
		t.Fatalf("R3.1 预约应为 CANCELLED/AUTO_BEFORE_2H，实际 %s/%s", status, cancelType)
	}
	if got := reservedCount(t, db, slotID); got != 0 {
		t.Fatalf("R3.1 自动受理应释放名额，reserved_count=%d", got)
	}
}

// —— R3.2 边界：距开始 2h-1s 必须填原因并转客服审核；名额保持；重复提交被拒 ——
func TestT02R32_BeforeTwoHoursRequiresReasonAndKeepsSeat(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	t02Pay(t, db, r.ID, 1, testBase, "r32")

	start := testBase.Add(2 * time.Hour)
	at := start.Add(-t02Cutoff + time.Second) // 2h-1s

	_, err := CancelReservation(context.Background(), db, r.ID, 1, "   ", at, t02Cutoff)
	wantBizCode(t, err, domain.CodeCancelReason)

	out, err := CancelReservation(context.Background(), db, r.ID, 1, "临时加班", at, t02Cutoff)
	if err != nil {
		t.Fatalf("R3.2 有原因应转审核，实际错误: %v", err)
	}
	if out.AutoAccepted || out.RequestID == 0 {
		t.Fatalf("R3.2 应进入 CANCELLATION_REVIEW，实际 auto=%v requestID=%d", out.AutoAccepted, out.RequestID)
	}
	if status, _ := reservationStatus(t, db, r.ID); status != domain.ResCancellationReview {
		t.Fatalf("R3.2 预约状态应为 CANCELLATION_REVIEW，实际 %s", status)
	}
	if got := reservedCount(t, db, slotID); got != 1 {
		t.Fatalf("R3.2/R3.3 待审核期间名额必须保持，reserved_count=%d", got)
	}
	if n := t02Count(t, db, "SELECT COUNT(*) FROM refund WHERE reservation_id=?", r.ID); n != 0 {
		t.Fatalf("R3.2 待审核阶段不应产生退款单，实际 %d 条", n)
	}

	// A10：待审中重复提交 → CANCEL_ALREADY_PENDING
	_, err = CancelReservation(context.Background(), db, r.ID, 1, "再次提交", at, t02Cutoff)
	wantBizCode(t, err, domain.CodeCancelAlreadyPending)
}

func t02Count(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("计数查询失败 %q: %v", q, err)
	}
	return n
}

// —— R3.3/R5.1 待审取消期间前台核销必须被拒绝，且无核销记录、名额不减 ——
func TestT02R33_CheckInBlockedWhileCancelPending(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	t02Pay(t, db, r.ID, 1, testBase, "r33")

	start := testBase.Add(2 * time.Hour)
	at := start.Add(-time.Hour) // <2h
	out, err := CancelReservation(ctx, db, r.ID, 1, "宠物生病", at, t02Cutoff)
	if err != nil || out.RequestID == 0 {
		t.Fatalf("前置：应提交取消审核，err=%v", err)
	}

	_, err = CheckIn(ctx, db, r.ID, 2, at) // staff 2 = rec@store1
	wantBizCode(t, err, domain.CodeCheckinPending)

	if n := t02Count(t, db, `SELECT COUNT(*) FROM checkin_record WHERE reservation_id=?`, r.ID); n != 0 {
		t.Fatalf("R3.3 待审期间不得产生核销记录，实际 %d 条", n)
	}
	if status, _ := reservationStatus(t, db, r.ID); status != domain.ResCancellationReview {
		t.Fatalf("R3.3 状态不应变化，实际 %s", status)
	}
	if got := reservedCount(t, db, slotID); got != 1 {
		t.Fatalf("R3.3 名额应保持 1，实际 %d", got)
	}
}

// —— R4.1/R4.2/R4.4 批准后不可核销；退款失败不改变取消状态；重试成功金额一致 ——
func TestT02R41_R44_ApprovedCancelAndRefundFailureLifecycle(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	t02Pay(t, db, r.ID, 1, testBase, "r44")

	start := testBase.Add(2 * time.Hour)
	at := start.Add(-time.Hour)
	out, err := CancelReservation(ctx, db, r.ID, 1, "航班取消", at, t02Cutoff)
	if err != nil {
		t.Fatalf("提交取消申请失败: %v", err)
	}
	if err := ReviewCancel(ctx, db, out.RequestID, 4, true, "同意", at); err != nil { // staff 4 = cs@store1
		t.Fatalf("客服批准失败: %v", err)
	}
	if status, cancelType := reservationStatus(t, db, r.ID); status != domain.ResCancelled || cancelType != domain.CancelCSApproved {
		t.Fatalf("R4.1 批准后应为 CANCELLED/CS_APPROVED，实际 %s/%s", status, cancelType)
	}
	if got := reservedCount(t, db, slotID); got != 0 {
		t.Fatalf("R4.2 批准应释放且仅释放一次名额，reserved_count=%d", got)
	}
	// 重复批准不得二次扣减
	_ = ReviewCancel(ctx, db, out.RequestID, 4, true, "重复点击", at)
	if got := reservedCount(t, db, slotID); got != 0 {
		t.Fatalf("R4.2 重复批准导致名额异常：reserved_count=%d", got)
	}
	if n := t02Count(t, db, `SELECT COUNT(*) FROM refund WHERE reservation_id=?`, r.ID); n != 1 {
		t.Fatalf("R4.2 批准只应生成 1 张退款，实际 %d 张", n)
	}

	// R4.1 批准后核销必须被拒绝且无核销记录
	_, err = CheckIn(ctx, db, r.ID, 2, at)
	wantBizCode(t, err, domain.CodeCheckinNotAllow)
	if n := t02Count(t, db, `SELECT COUNT(*) FROM checkin_record WHERE reservation_id=?`, r.ID); n != 0 {
		t.Fatalf("R4.1 批准后不应有核销记录，实际 %d 条", n)
	}

	// R4.4 退款失败：预约仍已取消、仍不可核销
	refundID, refundNo, _, amount, _ := t02RefundRow(t, db, r.ID)
	if amount != 9900 {
		t.Fatalf("R4.4 退款金额应为 9900，实际 %d", amount)
	}
	if _, err := HandleRefundNotify(ctx, db, refundNo, false, "渠道超时", at.Add(time.Minute)); err != nil {
		t.Fatalf("退款失败回调处理失败: %v", err)
	}
	_, _, rfStatus, _, _ := t02RefundRow(t, db, r.ID)
	if rfStatus != domain.RefundFailed {
		t.Fatalf("R4.4 退款状态应为 FAILED，实际 %s", rfStatus)
	}
	if status, _ := reservationStatus(t, db, r.ID); status != domain.ResCancelled {
		t.Fatalf("R4.4 退款失败不得改变预约状态，实际 %s", status)
	}
	_, err = CheckIn(ctx, db, r.ID, 2, at)
	wantBizCode(t, err, domain.CodeCheckinNotAllow)

	// R4.4 客服重试 → 成功，金额一致、预约仍不可用
	if _, err := RetryRefund(ctx, db, refundID, 4, at.Add(2*time.Minute)); err != nil {
		t.Fatalf("R4.4 失败退款应可重试，实际错误: %v", err)
	}
	_, _, rfStatus, _, retry := t02RefundRow(t, db, r.ID)
	if rfStatus != domain.RefundProcessing || retry != 1 {
		t.Fatalf("R4.4 重试后应为 PROCESSING/retry=1，实际 %s/%d", rfStatus, retry)
	}
	if _, err := HandleRefundNotify(ctx, db, refundNo, true, "", at.Add(3*time.Minute)); err != nil {
		t.Fatalf("退款成功回调失败: %v", err)
	}
	_, _, rfStatus, rfAmount, _ := t02RefundRow(t, db, r.ID)
	if rfStatus != domain.RefundSuccess || rfAmount != 9900 {
		t.Fatalf("R4.4 重试成功后应 SUCCESS/9900，实际 %s/%d", rfStatus, rfAmount)
	}
	if status, _ := reservationStatus(t, db, r.ID); status != domain.ResCancelled {
		t.Fatalf("R4.4 退款成功后预约仍应为 CANCELLED，实际 %s", status)
	}
	_, err = CheckIn(ctx, db, r.ID, 2, at)
	wantBizCode(t, err, domain.CodeCheckinNotAllow)
}

// —— R5.3/A11 重复核销、重复完成幂等：只有一条记录、收入只记一次 ——
func TestT02R53_DuplicateCheckinAndCompleteAreIdempotent(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	t02Pay(t, db, r.ID, 1, testBase, "r53")
	at := testBase.Add(3 * time.Hour)

	for i := 0; i < 3; i++ { // 重复核销 ×3（含不同员工）
		staff := int64(2)
		if i == 2 {
			staff = 5 // 另一名前台
		}
		already, err := CheckIn(ctx, db, r.ID, staff, at)
		if err != nil {
			t.Fatalf("第 %d 次核销失败: %v", i+1, err)
		}
		if i > 0 && !already {
			t.Fatalf("R5.3 重复核销应幂等返回 alreadyDone=true，第 %d 次得到 false", i+1)
		}
	}
	for i := 0; i < 2; i++ {
		if _, err := StartService(ctx, db, r.ID, 3, at); err != nil {
			t.Fatalf("第 %d 次开始服务失败: %v", i+1, err)
		}
	}
	for i := 0; i < 3; i++ {
		already, err := CompleteService(ctx, db, r.ID, 3, at)
		if err != nil {
			t.Fatalf("第 %d 次完成失败: %v", i+1, err)
		}
		if i > 0 && !already {
			t.Fatalf("R5.3 重复完成应幂等返回 alreadyDone=true，第 %d 次得到 false", i+1)
		}
	}

	if n := t02Count(t, db, `SELECT COUNT(*) FROM checkin_record WHERE reservation_id=?`, r.ID); n != 1 {
		t.Fatalf("R5.3 核销记录应唯一，实际 %d 条", n)
	}
	if n := t02Count(t, db, `SELECT COUNT(*) FROM service_record WHERE reservation_id=?`, r.ID); n != 1 {
		t.Fatalf("R5.3 服务记录应唯一，实际 %d 条", n)
	}
	if n := t02Count(t, db, `SELECT COUNT(*) FROM operation_log WHERE action='SERVICE_COMPLETE'`); n != 1 {
		t.Fatalf("R5.3 完成审计应只记 1 条（收入确认一次），实际 %d 条", n)
	}
	rep, err := GetDailyReport(ctx, db, 1, testBase)
	if err != nil {
		t.Fatalf("日清查询失败: %v", err)
	}
	if rep.CompletedIncomeCents != 9900 {
		t.Fatalf("R5.3/R8.3 已完成收入应为 9900，实际 %d", rep.CompletedIncomeCents)
	}
	if rep.UnaccountedCents != 0 {
		t.Fatalf("R8.2 金额勾稽差额应为 0，实际 %d", rep.UnaccountedCents)
	}
}

// —— R2.5/R2.7：同一预约仅一条 SUCCESS 支付；重复/换流水重复通知不得重复确认 ——
func TestT02R25_R27_SingleSuccessPaymentPerReservation(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)

	// 第一次支付失败（R2.7：不改状态、不释放名额）
	p1, err := CreatePayment(ctx, db, r.ID, 1, testBase)
	if err != nil {
		t.Fatalf("首次发起支付失败: %v", err)
	}
	if res, err := HandlePayNotify(ctx, db, "t02-fail-1", p1.OrderNo, false, testBase); err != nil {
		t.Fatalf("支付失败回调处理失败: %v", err)
	} else if res.Result != domain.NotifyRejected {
		t.Fatalf("支付失败回调结果应为 REJECTED，实际 %s", res.Result)
	}
	if status, _ := reservationStatus(t, db, r.ID); status != domain.ResPendingPayment {
		t.Fatalf("R2.7 支付失败后应仍为待支付，实际 %s", status)
	}
	if got := reservedCount(t, db, slotID); got != 1 {
		t.Fatalf("R2.7 支付失败不应释放名额，reserved_count=%d", got)
	}

	// 第二次支付成功 → 新流水、新 order_no → 预约确认
	p2, err := CreatePayment(ctx, db, r.ID, 1, testBase.Add(time.Second))
	if err != nil {
		t.Fatalf("重试发起支付失败: %v", err)
	}
	if p2.OrderNo == p1.OrderNo {
		t.Fatalf("R2.7 重试必须生成新 order_no，两次相同: %s", p1.OrderNo)
	}
	if res, err := HandlePayNotify(ctx, db, "t02-ok-1", p2.OrderNo, true, testBase.Add(time.Second)); err != nil {
		t.Fatalf("支付成功回调失败: %v", err)
	} else if res.Result != domain.NotifyConfirmed {
		t.Fatalf("支付成功应确认，实际 %s", res.Result)
	}
	if status, _ := reservationStatus(t, db, r.ID); status != domain.ResConfirmed {
		t.Fatalf("R2.4 支付成功后应为 CONFIRMED，实际 %s", status)
	}

	// 同流水重复通知 ×3（R2.5）
	for i := 0; i < 3; i++ {
		res, err := HandlePayNotify(ctx, db, "t02-ok-1", p2.OrderNo, true, testBase.Add(time.Second))
		if err != nil {
			t.Fatalf("重复通知处理失败: %v", err)
		}
		if res.Result != domain.NotifyDuplicate {
			t.Fatalf("R2.5 同流水重复通知应 DUPLICATE_IGNORED，实际 %s", res.Result)
		}
	}
	// 换流水号重复成功通知（同一 order_no）也不得重复记收款
	if res, err := HandlePayNotify(ctx, db, "t02-ok-2", p2.OrderNo, true, testBase.Add(2*time.Second)); err != nil {
		t.Fatalf("换流水通知处理失败: %v", err)
	} else if res.Result != domain.NotifyDuplicate {
		t.Fatalf("R2.5 同一支付单二次成功应 DUPLICATE_IGNORED，实际 %s", res.Result)
	}

	if n := t02Count(t, db, `SELECT COUNT(*) FROM payment WHERE reservation_id=? AND status='SUCCESS'`, r.ID); n != 1 {
		t.Fatalf("R2.5/R2.7 同一预约只允许一条 SUCCESS 支付，实际 %d 条", n)
	}
	// 同 channel_txn_id 的重复回调命中幂等闸不落新流水；换流水号的重复成功落 1 条 DUPLICATE 记录
	if n := t02Count(t, db, `SELECT COUNT(*) FROM payment_notification`); n != 3 {
		t.Fatalf("R2.5 回调原始流水应恰好 3 条（1 失败 + 1 成功 + 1 换流水重复），实际 %d 条", n)
	}
	if n := t02Count(t, db, `SELECT COUNT(*) FROM payment_notification WHERE result='DUPLICATE_IGNORED'`); n != 1 {
		t.Fatalf("R2.5 应恰好 1 条 DUPLICATE_IGNORED 通知记录，实际 %d 条", n)
	}
	var paid int
	if err := db.QueryRow(`SELECT COUNT(*) FROM payment WHERE reservation_id=? AND status='SUCCESS' AND paid_at IS NOT NULL AND amount_cents=9900`, r.ID).Scan(&paid); err != nil || paid != 1 {
		t.Fatalf("R2.4 成功支付应记录 paid_at 与 9900 分，实际 %d err=%v", paid, err)
	}
}

// —— R6.2 已核销/服务中/已完成不可走取消退款 ——
func TestT02R62_CancelDeniedInCheckedInServiceAndCompleted(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	at := testBase.Add(3 * time.Hour)

	// 三条预约分别推进到 CHECKED_IN / IN_SERVICE / COMPLETED
	r1 := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	r2 := mustCreateReservation(t, db, slotID, 1, 2, testBase)
	r3 := mustCreateReservation(t, db, slotID, 2, 3, testBase)
	t02Pay(t, db, r1.ID, 1, testBase, "c1")
	t02Pay(t, db, r2.ID, 1, testBase, "c2")
	t02Pay(t, db, r3.ID, 2, testBase, "c3")

	if _, err := CheckIn(ctx, db, r1.ID, 2, at); err != nil {
		t.Fatalf("核销 r1 失败: %v", err)
	}
	if _, err := CheckIn(ctx, db, r2.ID, 2, at); err != nil {
		t.Fatalf("核销 r2 失败: %v", err)
	}
	if _, err := StartService(ctx, db, r2.ID, 3, at); err != nil {
		t.Fatalf("开始 r2 失败: %v", err)
	}
	for _, step := range []string{"checkin", "start", "complete"} {
		var err error
		switch step {
		case "checkin":
			_, err = CheckIn(ctx, db, r3.ID, 2, at)
		case "start":
			_, err = StartService(ctx, db, r3.ID, 3, at)
		case "complete":
			_, err = CompleteService(ctx, db, r3.ID, 3, at)
		}
		if err != nil {
			t.Fatalf("推进 r3 到 COMPLETED 的 %s 步骤失败: %v", step, err)
		}
	}

	for _, tc := range []struct {
		name string
		id   int64
		uid  int64
	}{
		{"CHECKED_IN", r1.ID, 1},
		{"IN_SERVICE", r2.ID, 1},
		{"COMPLETED", r3.ID, 2},
	} {
		_, err := CancelReservation(ctx, db, tc.id, tc.uid, "想取消", at, t02Cutoff)
		wantBizCode(t, err, domain.CodeCancelNotAllow)
		if n := t02Count(t, db, `SELECT COUNT(*) FROM refund WHERE reservation_id=?`, tc.id); n != 0 {
			t.Fatalf("R6.2 %s 状态取消被拒后不得产生退款单，实际 %d 张", tc.name, n)
		}
	}
}

// —— R6.1/C4 核销 vs 取消审核并发（10 轮）：每轮唯一合法结果，无双重副作用 ——
// 说明：race 环境已解除 BLOCKED（MSYS2 MinGW-w64，见 docs/evidence/tests/0ee9aee/race-final.txt）；
// 本用例以 10 轮并发 + 终态断言验证唯一性，另在 go test -race 下复跑通过。
func TestT02R61_CheckInVsCancelConcurrentSingleWinner(t *testing.T) {
	ctx := context.Background()
	for round := 1; round <= 10; round++ {
		db := testDB(t)
		slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
		r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
		t02Pay(t, db, r.ID, 1, testBase, fmt.Sprintf("c4-%d", round))
		at := testBase.Add(2*time.Hour - time.Hour) // <2h：取消需走审核

		var (
			checkinErr  error
			cancelErr   error
			cancelReqID int64
			wg          sync.WaitGroup
		)
		wg.Add(2)
		go func() { // 前台核销
			defer wg.Done()
			_, checkinErr = CheckIn(ctx, db, r.ID, 2, at)
		}()
		go func() { // 用户提交取消申请
			defer wg.Done()
			out, err := CancelReservation(ctx, db, r.ID, 1, "临时有事", at, t02Cutoff)
			cancelErr = err
			if err == nil {
				cancelReqID = out.RequestID
			}
		}()
		wg.Wait()

		checkinOK := checkinErr == nil
		cancelOK := cancelErr == nil
		status, _ := reservationStatus(t, db, r.ID)
		recordCount := t02Count(t, db, `SELECT COUNT(*) FROM checkin_record WHERE reservation_id=?`, r.ID)
		pendingReqs := t02Count(t, db, `SELECT COUNT(*) FROM cancel_request WHERE reservation_id=? AND status='PENDING'`, r.ID)
		refunds := t02Count(t, db, `SELECT COUNT(*) FROM refund WHERE reservation_id=?`, r.ID)

		switch {
		case checkinOK && cancelOK:
			t.Fatalf("第 %d 轮：核销与取消同时成功（R6.1 违规），status=%s checkin=%d pending=%d",
				round, status, recordCount, pendingReqs)
		case !checkinOK && !cancelOK:
			t.Fatalf("第 %d 轮：核销与取消都失败（R6.1 违规）checkinErr=%v cancelErr=%v status=%s",
				round, checkinErr, cancelErr, status)
		case checkinOK:
			// 核销胜出：状态已到店、有 1 条核销记录、无待审申请、无退款
			if status != domain.ResCheckedIn || recordCount != 1 || pendingReqs != 0 || refunds != 0 {
				t.Fatalf("第 %d 轮：核销胜出但数据不一致 status=%s checkin=%d pending=%d refund=%d",
					round, status, recordCount, pendingReqs, refunds)
			}
			var be *domain.BizError
			if !errors.As(cancelErr, &be) ||
				(be.Code != domain.CodeCancelNotAllow && be.Code != domain.CodeStateInvalid &&
					be.Code != domain.CodeCheckinPending) {
				t.Fatalf("第 %d 轮：核销胜出时取消失败码应为 CANCEL_NOT_ALLOWED/STATE_INVALID，实际 %v", round, cancelErr)
			}
		default:
			// 取消胜出：待审核、无核销记录、名额保持
			if status != domain.ResCancellationReview || recordCount != 0 || pendingReqs != 1 || cancelReqID == 0 {
				t.Fatalf("第 %d 轮：取消胜出但数据不一致 status=%s checkin=%d pending=%d req=%d",
					round, status, recordCount, pendingReqs, cancelReqID)
			}
			if got := reservedCount(t, db, slotID); got != 1 {
				t.Fatalf("第 %d 轮：待审核期间名额应保持 1，实际 %d", round, got)
			}
			if checkinErr == nil {
				t.Fatalf("第 %d 轮：取消胜出时核销不应成功", round)
			}
		}
	}
}
