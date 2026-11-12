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

func payNotify(t *testing.T, db *sql.DB, txnID, orderNo string, success bool, now time.Time) *PayNotifyResult {
	t.Helper()
	r, err := HandlePayNotify(context.Background(), db, txnID, orderNo, success, now)
	if err != nil {
		t.Fatalf("处理支付回调失败: %v", err)
	}
	return r
}

// confirmedReservation 预置一个"已确认（已支付）"预约，返回预约。
func confirmedReservation(t *testing.T, db *sql.DB, slotStartIn time.Duration, userID, petID int64) *model.Reservation {
	t.Helper()
	slotID := mustCreateSlot(t, db, slotStartIn, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, userID, petID, testBase)
	p := mustCreatePayment(t, db, r.ID, userID, testBase)
	payNotify(t, db, "txn-"+p.OrderNo, p.OrderNo, true, testBase.Add(time.Minute))
	if st, _ := reservationStatus(t, db, r.ID); st != model.ResConfirmed {
		t.Fatalf("前置失败：预约应已确认，得到 %s", st)
	}
	return r
}

func paymentSuccessCount(t *testing.T, db *sql.DB, reservationID int64) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM payment WHERE reservation_id=? AND status=?`,
		reservationID, model.PaySuccess).Scan(&n); err != nil {
		t.Fatalf("统计成功支付: %v", err)
	}
	return n
}

// —— A4/R2.7 发起支付校验 ————————————————————

func TestCreatePaymentOK(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)

	p := mustCreatePayment(t, db, r.ID, 1, testBase.Add(time.Minute))
	if p.Status != model.PayInit || p.AmountCents != 9900 || p.OrderNo == "" || p.Attempt != 1 {
		t.Fatalf("支付单字段不符: %+v", p)
	}
	if p.ReservationID != r.ID {
		t.Fatalf("支付单应关联预约 %d，得到 %d", r.ID, p.ReservationID)
	}
}

func TestCreatePaymentWrongOwner(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	_, err := CreatePayment(context.Background(), db, r.ID, 2, testBase)
	wantBizCode(t, err, CodeNotFound)
}

func TestCreatePaymentAfterDeadlineRejected(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	_, err := CreatePayment(context.Background(), db, r.ID, 1, testBase.Add(6*time.Minute))
	wantBizCode(t, err, CodePaymentExpired)
	if st, _ := reservationStatus(t, db, r.ID); st != model.ResExpired {
		t.Fatalf("超时支付应把占位清理为过期，得到 %s", st)
	}
}

func TestCreatePaymentNotPending(t *testing.T) {
	db := testDB(t)
	r := confirmedReservation(t, db, 2*time.Hour, 1, 1)
	_, err := CreatePayment(context.Background(), db, r.ID, 1, testBase.Add(2*time.Minute))
	wantBizCode(t, err, CodePaymentNotPend)
}

// —— R2.4/R2.5 回调确认、失败保持与幂等 ————————————

func TestPayNotifyConfirmsReservation(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	p := mustCreatePayment(t, db, r.ID, 1, testBase)

	res := payNotify(t, db, "txn-1", p.OrderNo, true, testBase.Add(time.Minute))
	if res.Result != model.NotifyConfirmed {
		t.Fatalf("首次成功回调应确认预约，得到 %s", res.Result)
	}
	if s, _ := reservationStatus(t, db, r.ID); s != model.ResConfirmed {
		t.Fatalf("预约应转为已确认，得到 %s", s)
	}
	var paidAt sql.NullString
	if err := db.QueryRow(`SELECT paid_at FROM reservation WHERE id=?`, r.ID).Scan(&paidAt); err != nil || !paidAt.Valid {
		t.Fatalf("paid_at 应有值: %v valid=%v", err, paidAt.Valid)
	}
}

func TestPayNotifyFailedThenRetrySucceeds(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	p1 := mustCreatePayment(t, db, r.ID, 1, testBase)

	fail := payNotify(t, db, "txn-fail-1", p1.OrderNo, false, testBase.Add(time.Minute))
	if fail.Result != model.NotifyRejected {
		t.Fatalf("失败回调结果应为 REJECTED，得到 %s", fail.Result)
	}
	if s, _ := reservationStatus(t, db, r.ID); s != model.ResPendingPayment {
		t.Fatalf("失败回调后预约应保持待支付，得到 %s", s)
	}

	// R2.7：重试生成新流水与新 order_no
	p2 := mustCreatePayment(t, db, r.ID, 1, testBase.Add(2*time.Minute))
	if p2.ID == p1.ID || p2.OrderNo == p1.OrderNo || p2.Attempt != 2 {
		t.Fatalf("重试应生成新流水/新单号/attempt=2: %+v", p2)
	}
	res := payNotify(t, db, "txn-ok-2", p2.OrderNo, true, testBase.Add(3*time.Minute))
	if res.Result != model.NotifyConfirmed {
		t.Fatalf("重试成功应确认，得到 %s", res.Result)
	}
	if n := paymentSuccessCount(t, db, r.ID); n != 1 {
		t.Fatalf("同一预约只允许一条成功支付，得到 %d", n)
	}
}

func TestPayNotifyDuplicateIsIdempotent(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	p := mustCreatePayment(t, db, r.ID, 1, testBase)

	payNotify(t, db, "txn-dup", p.OrderNo, true, testBase.Add(time.Minute))
	second := payNotify(t, db, "txn-dup", p.OrderNo, true, testBase.Add(2*time.Minute))
	if second.Result != model.NotifyDuplicate {
		t.Fatalf("重复流水应幂等忽略，得到 %s", second.Result)
	}
	if s, _ := reservationStatus(t, db, r.ID); s != model.ResConfirmed {
		t.Fatalf("重复通知不得改变状态，得到 %s", s)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM payment_notification WHERE channel_txn_id='txn-dup'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("同一渠道流水只应记录 1 条，得到 %d err=%v", n, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM operation_log WHERE action='PAYMENT_NOTIFY' AND result='CONFIRMED'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("确认动作只应发生 1 次，得到 %d err=%v", n, err)
	}
}

func TestPayNotifyUnknownOrder(t *testing.T) {
	db := testDB(t)
	_, err := HandlePayNotify(context.Background(), db, "txn-x", "NO-SUCH-ORDER", true, testBase)
	wantBizCode(t, err, CodePaymentNotFound)
}

// —— R2.6 关闭后支付：异常 + 退款、状态不变 ————————————

func TestPayNotifyAfterCancelMakesAnomalyAndRefund(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	p := mustCreatePayment(t, db, r.ID, 1, testBase)

	if err := CancelUnpaid(ctx, db, r.ID, 1, testBase.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	res := payNotify(t, db, "txn-late-cancel", p.OrderNo, true, testBase.Add(2*time.Minute))
	if res.Result != model.NotifyAnomalyRefund || res.RefundID == 0 {
		t.Fatalf("关闭后支付应异常退款: %+v", res)
	}
	if s, _ := reservationStatus(t, db, r.ID); s != model.ResCancelled {
		t.Fatalf("预约必须保持已取消，得到 %s", s)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM payment_anomaly WHERE payment_id=?`, p.ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("支付异常应恰有 1 条，得到 %d err=%v", n, err)
	}
	id, status, retry, _ := refundOf(t, db, r.ID)
	if id != res.RefundID || status != model.RefundPending || retry != 0 {
		t.Fatalf("退款单应为待处理且未重试: id=%d status=%s retry=%d", id, status, retry)
	}
}

func TestPayNotifyLateSuccessAfterDeadline(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 4*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase) // 占位截止 06:05
	p := mustCreatePayment(t, db, r.ID, 1, testBase)

	// 06:08 渠道才回调成功：先过期占位，再走异常退款
	res := payNotify(t, db, "txn-late", p.OrderNo, true, testBase.Add(8*time.Minute))
	if res.Result != model.NotifyAnomalyRefund {
		t.Fatalf("超时关闭后的迟到成功应走异常退款: %s", res.Result)
	}
	if st, ct := reservationStatus(t, db, r.ID); st != model.ResExpired || ct != model.CancelExpired {
		t.Fatalf("预约应为超时过期，得到 %s/%s", st, ct)
	}
	if id, status, _, _ := refundOf(t, db, r.ID); id == 0 || status != model.RefundPending {
		t.Fatalf("应生成待处理退款单，得到 id=%d status=%s", id, status)
	}
}
