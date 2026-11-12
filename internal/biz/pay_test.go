package biz

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"paw-book/internal/model"
)

// —— 测试辅助 ——————————————————————————————

func mustCreatePayment(t *testing.T, db *sql.DB, reservationID, userID int64, now time.Time) *model.Payment {
	t.Helper()
	p, err := CreatePayment(context.Background(), db, reservationID, userID, now)
	if err != nil {
		t.Fatalf("发起支付失败: %v", err)
	}
	return p
}

func payNotify(t *testing.T, db *sql.DB, outTradeNo string, success bool, now time.Time) *PayNotifyResult {
	t.Helper()
	r, err := HandlePayNotify(context.Background(), db, outTradeNo, success, now)
	if err != nil {
		t.Fatalf("处理支付回调失败: %v", err)
	}
	return r
}

func refundStatusOf(t *testing.T, db *sql.DB, id int64) (string, string, int) {
	t.Helper()
	var status, failReason string
	var retry int
	var fr sql.NullString
	if err := db.QueryRow(`SELECT status, fail_reason, retry_count FROM refunds WHERE id=?`, id).Scan(&status, &fr, &retry); err != nil {
		t.Fatalf("查询退款单 %d: %v", id, err)
	}
	if fr.Valid {
		failReason = fr.String
	}
	return status, failReason, retry
}

func reservationRawStatus(t *testing.T, db *sql.DB, id int64) string {
	t.Helper()
	var s string
	if err := db.QueryRow(`SELECT status FROM reservations WHERE id=?`, id).Scan(&s); err != nil {
		t.Fatalf("查询预约 %d: %v", id, err)
	}
	return s
}

// —— 发起支付校验 ——————————————————————————

func TestCreatePaymentOK(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)

	p := mustCreatePayment(t, db, r.ID, 1, testBase.Add(time.Minute))
	if p.Status != model.PayCreated || p.AmountCents != 9900 || p.OutTradeNo == "" {
		t.Fatalf("支付单字段不符: %+v", p)
	}
}

func TestCreatePaymentWrongOwner(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	_, err := CreatePayment(context.Background(), db, r.ID, 2, testBase)
	wantBizCode(t, err, CodePermission)
}

func TestCreatePaymentAfterDeadlineRejected(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	_, err := CreatePayment(context.Background(), db, r.ID, 1, testBase.Add(6*time.Minute))
	wantBizCode(t, err, CodeInvalidState)
}

// —— 回调：确认 / 失败 / 幂等（R6/R9/R7） ————————————

func TestPayNotifyConfirmsReservation(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	p := mustCreatePayment(t, db, r.ID, 1, testBase)

	res := payNotify(t, db, p.OutTradeNo, true, testBase.Add(time.Minute))
	if !res.Confirmed {
		t.Fatal("首次成功回调应确认预约")
	}
	if s := reservationRawStatus(t, db, r.ID); s != model.ResConfirmed {
		t.Fatalf("预约应转为已确认，得到 %s", s)
	}
}

func TestPayNotifyFailedKeepsPending(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	p := mustCreatePayment(t, db, r.ID, 1, testBase)

	payNotify(t, db, p.OutTradeNo, false, testBase.Add(time.Minute))
	if s := reservationRawStatus(t, db, r.ID); s != model.ResPendingPayment {
		t.Fatalf("失败回调后预约应保持待支付，得到 %s", s)
	}
	// 失败后可重新发起支付
	p2 := mustCreatePayment(t, db, r.ID, 1, testBase.Add(2*time.Minute))
	res := payNotify(t, db, p2.OutTradeNo, true, testBase.Add(3*time.Minute))
	if !res.Confirmed {
		t.Fatal("重试支付成功后应确认")
	}
}

func TestPayNotifyDuplicateIsIdempotent(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	p := mustCreatePayment(t, db, r.ID, 1, testBase)

	payNotify(t, db, p.OutTradeNo, true, testBase.Add(time.Minute))
	second := payNotify(t, db, p.OutTradeNo, true, testBase.Add(2*time.Minute))
	if !second.Duplicated {
		t.Fatal("重复成功通知应标记为幂等忽略")
	}
	if s := reservationRawStatus(t, db, r.ID); s != model.ResConfirmed {
		t.Fatalf("重复通知不得改变状态，得到 %s", s)
	}
	// 确认动作只发生过一次
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action='PAYMENT_CONFIRM'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("确认审计应恰有 1 条，得到 %d err=%v", n, err)
	}
}

// —— 回调：预约已关闭（R8） ————————————————————

func TestPayNotifyAfterUserCancelMakesExceptionAndRefund(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	p := mustCreatePayment(t, db, r.ID, 1, testBase)

	// 用户在回调到达前主动取消了预约（关闭）
	if err := CancelPendingReservation(context.Background(), db, r.ID, 1, testBase.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	res := payNotify(t, db, p.OutTradeNo, true, testBase.Add(2*time.Minute))
	if !res.Exception || res.RefundID == 0 {
		t.Fatalf("应登记支付异常并创建退款: %+v", res)
	}
	if s := reservationRawStatus(t, db, r.ID); s != model.ResCancelled {
		t.Fatalf("预约必须保持关闭不恢复，得到 %s", s)
	}
	// 退款单独立可查，金额与支付一致
	status, _, _ := refundStatusOf(t, db, res.RefundID)
	if status != model.RefundPending {
		t.Fatalf("异常退款应处于待退款，得到 %s", status)
	}
	// 支付异常登记存在
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM payment_exceptions WHERE payment_id=? AND refund_id=?`, res.PaymentID, res.RefundID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("支付异常登记应恰有 1 条: n=%d err=%v", n, err)
	}
}

func TestPayNotifyLateSuccessAfterDeadline(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase) // 截止 06:05
	p := mustCreatePayment(t, db, r.ID, 1, testBase)

	// 06:08 渠道才回调成功（迟到通知）
	res := payNotify(t, db, p.OutTradeNo, true, testBase.Add(8*time.Minute))
	if !res.Exception {
		t.Fatalf("超时关闭后的迟到成功应走异常退款: %+v", res)
	}
	// 预约被按超时取消口径处理
	st, ct := reservationStatus(t, db, r.ID)
	if st != model.ResCancelled || ct != model.CancelTimeout {
		t.Fatalf("预约应为超时取消，得到 %s/%s", st, ct)
	}
}

// —— 退款回调（R14/I6） ———————————————————————

func TestRefundNotifyLifecycle(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	p := mustCreatePayment(t, db, r.ID, 1, testBase)

	// 先走异常退款路径拿到退款单
	if err := CancelPendingReservation(ctx, db, r.ID, 1, testBase.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	res := payNotify(t, db, p.OutTradeNo, true, testBase.Add(2*time.Minute))
	if !res.Exception {
		t.Fatalf("应产生异常退款: %+v", res)
	}

	// 第一次退款回调：失败（渠道超时）
	if _, err := HandleRefundNotify(ctx, db, refundNoOf(t, db, res.RefundID), false, "渠道超时", testBase.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	st, reason, retry := refundStatusOf(t, db, res.RefundID)
	if st != model.RefundFailed || reason != "渠道超时" || retry != 1 {
		t.Fatalf("退款应记失败与原因: 状态=%s 原因=%s 重试=%d", st, reason, retry)
	}
	// 退款失败不影响预约终态（I6）
	if s := reservationRawStatus(t, db, r.ID); s != model.ResCancelled {
		t.Fatalf("退款失败不得改变预约，得到 %s", s)
	}

	// 重试后成功
	if _, err := HandleRefundNotify(ctx, db, refundNoOf(t, db, res.RefundID), true, "", testBase.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if st, _, _ := refundStatusOf(t, db, res.RefundID); st != model.RefundSuccess {
		t.Fatalf("重试后退款应成功，得到 %s", st)
	}

	// 重复成功通知幂等
	dup, err := HandleRefundNotify(ctx, db, refundNoOf(t, db, res.RefundID), true, "", testBase.Add(5*time.Minute))
	if err != nil || !dup.Duplicated {
		t.Fatalf("重复退款成功通知应幂等: %+v err=%v", dup, err)
	}
}

func refundNoOf(t *testing.T, db *sql.DB, id int64) string {
	t.Helper()
	var no string
	if err := db.QueryRow(`SELECT refund_no FROM refunds WHERE id=?`, id).Scan(&no); err != nil {
		t.Fatalf("查询退款单号: %v", err)
	}
	return no
}

// —— 全局扫描（T6，R4/R13/I4） ——————————————————

func TestSweepExpiredReservations(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	slot1 := mustCreateSlot(t, db, 4*time.Hour, time.Hour, 3)
	slot2 := mustCreateSlot(t, db, 5*time.Hour, time.Hour, 3)

	r1 := mustCreateReservation(t, db, slot1, 1, 1, testBase)                    // 截止 06:05
	r2 := mustCreateReservation(t, db, slot2, 2, 3, testBase)                    // 截止 06:05
	r3 := mustCreateReservation(t, db, slot1, 1, 2, testBase.Add(4*time.Minute)) // 截止 06:09，未过期

	// 06:06 扫描：r1、r2 应被取消，r3 未到期不动
	n, err := SweepExpiredReservations(ctx, db, testBase.Add(6*time.Minute))
	if err != nil || n != 2 {
		t.Fatalf("应取消 2 条，得到 %d err=%v", n, err)
	}
	for _, id := range []int64{r1.ID, r2.ID} {
		if st, ct := reservationStatus(t, db, id); st != model.ResCancelled || ct != model.CancelTimeout {
			t.Fatalf("过期预约应为超时取消，得到 %s/%s", st, ct)
		}
	}
	if st, _ := reservationStatus(t, db, r3.ID); st != model.ResPendingPayment {
		t.Fatalf("未过期预约不应被取消，得到 %s", st)
	}

	// 幂等：再次扫描无新增
	n2, err := SweepExpiredReservations(ctx, db, testBase.Add(7*time.Minute))
	if err != nil || n2 != 0 {
		t.Fatalf("重复扫描应取消 0 条，得到 %d err=%v", n2, err)
	}

	// 名额已释放：slot1 上新的有效预约可成立
	mustCreateReservation(t, db, slot1, 2, 3, testBase.Add(8*time.Minute))
}
