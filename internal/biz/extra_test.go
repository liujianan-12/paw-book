package biz

import (
	"context"
	"testing"
	"time"

	"paw-book/internal/model"
)

// 覆盖错误类型与时钟等基础分支。
func TestBizErrorAndClock(t *testing.T) {
	e := errf(CodeNotFound, "预约 %d 不存在", 7)
	if e.Code != CodeNotFound || e.Error() == "" {
		t.Fatalf("BizError 字段不符: %+v", e)
	}
	if RealClock()().IsZero() {
		t.Fatal("RealClock 不应返回零值时间")
	}
}

// R2.2 全局过期扫描：批量清理 + 幂等 + 名额对账。
func TestSweepExpiredGlobal(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	slot1 := mustCreateSlot(t, db, 4*time.Hour, time.Hour, 3)
	slot2 := mustCreateSlot(t, db, 5*time.Hour, time.Hour, 3)

	r1 := mustCreateReservation(t, db, slot1, 1, 1, testBase)
	r2 := mustCreateReservation(t, db, slot2, 2, 3, testBase)
	r3 := mustCreateReservation(t, db, slot1, 1, 2, testBase.Add(4*time.Minute)) // 06:09 到期

	n, err := SweepExpired(ctx, db, testBase.Add(6*time.Minute))
	if err != nil || n != 2 {
		t.Fatalf("应清理 2 条，得到 %d err=%v", n, err)
	}
	for _, id := range []int64{r1.ID, r2.ID} {
		if st, ct := reservationStatus(t, db, id); st != model.ResExpired || ct != model.CancelExpired {
			t.Fatalf("预约 %d 应为过期/超时，得到 %s/%s", id, st, ct)
		}
	}
	if st, _ := reservationStatus(t, db, r3.ID); st != model.ResPendingPayment {
		t.Fatalf("未到期预约不应被清理，得到 %s", st)
	}
	if got := reservedCount(t, db, slot1); got != 1 {
		t.Fatalf("slot1 名额应为 1（仅 r3），得到 %d", got)
	}
	if got := reservedCount(t, db, slot2); got != 0 {
		t.Fatalf("slot2 名额应为 0，得到 %d", got)
	}

	n, err = SweepExpired(ctx, db, testBase.Add(7*time.Minute))
	if err != nil || n != 0 {
		t.Fatalf("重复扫描应清理 0 条，得到 %d err=%v", n, err)
	}
}

func TestListAvailableSlotsEmptyStore(t *testing.T) {
	db := testDB(t)
	slots, err := ListAvailableSlots(context.Background(), db, 2, testBase)
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 0 {
		t.Fatalf("别店无时段，得到 %d", len(slots))
	}
}

// R1 时段挂的项目不存在/已下架时不得预约。
func TestCreateReservationItemUnavailable(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`INSERT INTO service_item(id, store_id, name, price_cents, active) VALUES(99, 1, '已下架', 9900, 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO slot(id, store_id, item_id, start_at, end_at, capacity, reserved_count, created_by, created_at)
		VALUES(20, 1, 99, ?, ?, 3, 0, 1, ?)`,
		testBase.Add(2*time.Hour).UTC().Format(time.RFC3339),
		testBase.Add(3*time.Hour).UTC().Format(time.RFC3339),
		testBase.UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	_, err := CreateReservation(context.Background(), db,
		CreateReservationParams{SlotID: 20, UserID: 1, PetID: 1}, testBase, 5*time.Minute)
	wantBizCode(t, err, CodeNotFound)
}

// R4 已确认但无成功支付记录时，不能凭空创建退款。
func TestCancelConfirmedWithoutPayment(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 4*time.Hour, time.Hour, 3)
	res, err := db.Exec(`INSERT INTO reservation(store_id, slot_id, user_id, pet_id, item_id, status, price_cents,
		pay_expire_at, seat_released, version, created_at, updated_at)
		VALUES(1, ?, 1, 1, 1, 'CONFIRMED', 9900, ?, 0, 0, ?, ?)`,
		slotID, fmtTime(testBase.Add(5*time.Minute)), fmtTime(testBase), fmtTime(testBase))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	_, err = CancelReservation(context.Background(), db, id, 1, "", testBase.Add(time.Minute), 2*time.Hour)
	wantBizCode(t, err, CodeStateInvalid)
}

// 审核时预约状态已被并发改变 → 明确业务码。
func TestReviewAfterStateChanged(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := confirmedReservation(t, db, time.Hour, 1, 1)
	cres, err := CancelReservation(ctx, db, r.ID, 1, "临时有事", testBase.Add(time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE reservation SET status='CHECKED_IN' WHERE id=?`, r.ID); err != nil {
		t.Fatal(err)
	}
	wantBizCode(t, ReviewCancel(ctx, db, cres.RequestID, 4, true, "", testBase.Add(2*time.Minute)), CodeStateInvalid)
}

func TestRefundNotifyNotFound(t *testing.T) {
	db := testDB(t)
	_, err := HandleRefundNotify(context.Background(), db, "NO-SUCH-REFUND", true, "", testBase)
	wantBizCode(t, err, CodeNotFound)
}

// A16/R4.4：待处理（PENDING）退款也可由客服重试。
func TestRetryPendingRefund(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := confirmedReservation(t, db, 4*time.Hour, 1, 1)
	cres, err := CancelReservation(ctx, db, r.ID, 1, "", testBase.Add(time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RetryRefund(ctx, db, cres.RefundID, 4, testBase.Add(2*time.Minute)); err != nil {
		t.Fatalf("PENDING 退款应可重试: %v", err)
	}
	if _, status, retry, _ := refundOf(t, db, r.ID); status != model.RefundProcessing || retry != 1 {
		t.Fatalf("应转为 PROCESSING/retry=1，得到 %s/%d", status, retry)
	}
}

// R2.5/R2.7：已成功支付后收到失败回调，不得把支付单改回失败。
func TestPayNotifyFailureAfterSuccessIgnored(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	p := mustCreatePayment(t, db, r.ID, 1, testBase)
	payNotify(t, db, "txn-ok", p.OrderNo, true, testBase.Add(time.Minute))

	res := payNotify(t, db, "txn-fail-late", p.OrderNo, false, testBase.Add(2*time.Minute))
	if res.Result != model.NotifyRejected {
		t.Fatalf("失败通知应记录 REJECTED，得到 %s", res.Result)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM payment WHERE id=?`, p.ID).Scan(&status); err != nil || status != model.PaySuccess {
		t.Fatalf("已成功支付不得被失败回调改写，得到 %s err=%v", status, err)
	}
}

func TestGetReservationNotFound(t *testing.T) {
	db := testDB(t)
	_, err := GetReservation(context.Background(), db, 9999, 1)
	wantBizCode(t, err, CodeNotFound)
}

// beginTx 对不支持事务的连接返回明确业务错误，而不是 panic。
type noTxConn struct{ DBTX }

func TestBeginTxUnsupported(t *testing.T) {
	_, err := beginTx(context.Background(), noTxConn{})
	wantBizCode(t, err, CodeInvalidParam)
}

// A7：详情视图应带出取消申请与退款单。
func TestGetReservationWithCancelAndRefund(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := confirmedReservation(t, db, time.Hour, 1, 1)
	cres, err := CancelReservation(ctx, db, r.ID, 1, "宠物生病", testBase.Add(time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := ReviewCancel(ctx, db, cres.RequestID, 4, true, "同意", testBase.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	d, err := GetReservation(ctx, db, r.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if d.CancelReqID != cres.RequestID || d.CancelReqStat != model.CancelReqApproved || d.CancelReason != "宠物生病" {
		t.Fatalf("详情应带出取消申请: %+v", d)
	}
	if d.RefundID == 0 || d.RefundStatus != model.RefundPending {
		t.Fatalf("详情应带出退款单: %+v", d)
	}
}

// A8：列表分页参数越界时按默认值处理。
func TestListMyReservationsLimitClamp(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	mustCreateReservation(t, db, slotID, 1, 1, testBase)
	for _, limit := range []int{0, -1, 1000} {
		list, err := ListMyReservations(ctx, db, 1, limit)
		if err != nil || len(list) != 1 {
			t.Fatalf("limit=%d 应返回 1 条，得到 %d err=%v", limit, len(list), err)
		}
	}
}

// I8：退款单按预约唯一，重复创建复用同一条。
func TestEnsureRefundIsUniquePerReservation(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := confirmedReservation(t, db, 2*time.Hour, 1, 1)
	var payID, amount int64
	if err := db.QueryRow(`SELECT id, amount_cents FROM payment WHERE reservation_id=? AND status='SUCCESS'`, r.ID).
		Scan(&payID, &amount); err != nil {
		t.Fatal(err)
	}
	id1, err := ensureRefund(ctx, db, r.ID, payID, amount, model.ReasonAutoCancel, testBase)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := ensureRefund(ctx, db, r.ID, payID, amount, model.ReasonCSApproved, testBase)
	if err != nil || id2 != id1 {
		t.Fatalf("同一预约退款单应复用: id1=%d id2=%d err=%v", id1, id2, err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM refund WHERE reservation_id=?`, r.ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("退款单应恰有 1 条，得到 %d err=%v", n, err)
	}
}

// R4.4：渠道未给原因时按 CHANNEL_TIMEOUT 记录，便于客服跟进。
func TestRefundNotifyEmptyReasonUsesChannelTimeout(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := confirmedReservation(t, db, 4*time.Hour, 1, 1)
	cres, err := CancelReservation(ctx, db, r.ID, 1, "", testBase.Add(time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := HandleRefundNotify(ctx, db, refundNoByID(t, db, cres.RefundID), false, "", testBase.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, status, _, lastErr := refundOf(t, db, r.ID); status != model.RefundFailed || lastErr != model.ReasonChannelError {
		t.Fatalf("应记 CHANNEL_TIMEOUT: %s/%s", status, lastErr)
	}
}

// R2.7：同一预约已有成功支付时，即使预约状态仍是待支付也不得再次发起。
func TestCreatePaymentWhenAlreadySucceeded(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	if _, err := db.Exec(`INSERT INTO payment(reservation_id, order_no, amount_cents, channel, status, created_at)
		VALUES(?, 'LEGACY-OK', 9900, 'mock', 'SUCCESS', ?)`, r.ID, fmtTime(testBase)); err != nil {
		t.Fatal(err)
	}
	_, err := CreatePayment(context.Background(), db, r.ID, 1, testBase.Add(time.Minute))
	wantBizCode(t, err, CodePaymentNotPend)
}

// R5.1：过期/未到店预约不得核销；R5.2：顺序不能越级。
func TestCheckInAndServiceIllegalStates(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)

	for _, st := range []string{model.ResExpired, model.ResNoShow} {
		res, err := db.Exec(`INSERT INTO reservation(store_id, slot_id, user_id, pet_id, item_id, status, price_cents,
			pay_expire_at, seat_released, version, created_at, updated_at)
			VALUES(1, ?, 1, 1, 1, ?, 9900, ?, 1, 0, ?, ?)`,
			slotID, st, fmtTime(testBase.Add(5*time.Minute)), fmtTime(testBase), fmtTime(testBase))
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		_, err = CheckIn(ctx, db, id, 2, testBase.Add(time.Minute))
		wantBizCode(t, err, CodeCheckinNotAllow)
	}

	r := confirmedReservation(t, db, 2*time.Hour, 2, 3)
	if _, err := CheckIn(ctx, db, r.ID, 2, testBase.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	// 未开始直接完成
	_, err := CompleteService(ctx, db, r.ID, 3, testBase.Add(3*time.Minute))
	wantBizCode(t, err, CodeServiceNotStart)
	// 完成后重复开始也幂等
	if _, err := StartService(ctx, db, r.ID, 3, testBase.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := CompleteService(ctx, db, r.ID, 3, testBase.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	already, err := StartService(ctx, db, r.ID, 3, testBase.Add(6*time.Minute))
	if err != nil || !already {
		t.Fatalf("已完成预约重复开始应幂等: already=%v err=%v", already, err)
	}
}

// 约束冲突识别：只认“唯一约束 + 目标表/列”，不能把其它错误误判成业务冲突。
func TestIsConstraintViolation(t *testing.T) {
	cases := []struct {
		err   error
		table string
		cols  []string
		want  bool
	}{
		{nil, "reservation", []string{"slot_id"}, false},
		{errString("UNIQUE constraint failed: reservation.slot_id, reservation.pet_id"), "reservation", []string{"slot_id", "pet_id"}, true},
		{errString("UNIQUE constraint failed: reservation.slot_id, reservation.pet_id"), "payment", []string{"slot_id"}, false},
		{errString("UNIQUE constraint failed: reservation.slot_id, reservation.pet_id"), "reservation", []string{"order_no"}, false},
		{errString("foreign key constraint failed"), "reservation", nil, false},
	}
	for i, c := range cases {
		if got := isConstraintViolation(c.err, c.table, c.cols...); got != c.want {
			t.Errorf("case %d: want %v got %v", i, c.want, got)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// 内部辅助函数在连接异常或数据损坏时返回错误（不 panic、不静默）。
func TestInternalHelpersErrorPaths(t *testing.T) {
	ctx := context.Background()

	// 时间字段损坏：parseTime 失败必须返回错误
	db := testDB(t)
	if _, err := db.Exec(`INSERT INTO slot(id, store_id, item_id, start_at, end_at, capacity, reserved_count, created_by, created_at)
		VALUES(70, 1, 1, 'zzz', 'zzzz', 3, 0, 1, ?)`,
		testBase.UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if _, err := slotByID(ctx, db, 70); err == nil {
		t.Error("start_at 非法时 slotByID 应返回错误")
	}
	if _, err := db.Exec(`INSERT INTO slot(id, store_id, item_id, start_at, end_at, capacity, reserved_count, created_by, created_at)
		VALUES(71, 1, 1, ?, 'zzz', 3, 0, 1, ?)`,
		testBase.Add(2*time.Hour).UTC().Format(time.RFC3339), testBase.UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if _, err := slotByID(ctx, db, 71); err == nil {
		t.Error("end_at 非法时 slotByID 应返回错误")
	}
	if _, err := ListAvailableSlots(ctx, db, 1, testBase); err == nil {
		t.Error("列表含非法时间时应返回错误")
	}

	// 连接不可用：辅助函数返回错误
	closed := testDB(t)
	_ = closed.Close()
	if _, err := slotByID(ctx, closed, 1); err == nil {
		t.Error("连接不可用时 slotByID 应返回错误")
	}
	if _, err := expireRowsTx(ctx, closed, " AND id=?", testBase, []any{int64(1)}); err == nil {
		t.Error("连接不可用时 expireRowsTx 应返回错误")
	}
	if _, err := ensureRefund(ctx, closed, 1, 1, 9900, model.ReasonAutoCancel, testBase); err == nil {
		t.Error("连接不可用时 ensureRefund 应返回错误")
	}
	if _, err := hasPendingCancel(ctx, closed, 1); err == nil {
		t.Error("连接不可用时 hasPendingCancel 应返回错误")
	}
	if _, err := refundOfReservation(ctx, closed, 1, model.ReasonAutoCancel, testBase); err == nil {
		t.Error("连接不可用时 refundOfReservation 应返回错误")
	}
}

// 支付单更新失败：回调必须报错并回滚预约确认（R2.4 原子性）。
func TestPayNotifyPaymentUpdateFailureRollsBack(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	p := mustCreatePayment(t, db, r.ID, 1, testBase)
	if _, err := db.Exec(`CREATE TRIGGER fail_payment_update BEFORE UPDATE ON payment
		WHEN NEW.status='SUCCESS' BEGIN SELECT RAISE(ABORT, 'pay down'); END`); err != nil {
		t.Fatal(err)
	}
	_, err := HandlePayNotify(ctx, db, "txn-payfail", p.OrderNo, true, testBase.Add(time.Minute))
	if err == nil {
		t.Fatal("支付单更新失败时应返回错误")
	}
	if st, _ := reservationStatus(t, db, r.ID); st != model.ResPendingPayment {
		t.Fatalf("事务应回滚，预约仍为待支付，得到 %s", st)
	}
}

// 时段结束时间损坏时，未到店标记必须报错而不是按零值时间放行。
func TestMarkNoShowInvalidSlotTime(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`INSERT INTO slot(id, store_id, item_id, start_at, end_at, capacity, reserved_count, created_by, created_at)
		VALUES(80, 1, 1, ?, 'zzz', 3, 1, 1, ?)`,
		testBase.UTC().Format(time.RFC3339), testBase.UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	res, err := db.Exec(`INSERT INTO reservation(store_id, slot_id, user_id, pet_id, item_id, status, price_cents,
		pay_expire_at, seat_released, version, created_at, updated_at)
		VALUES(1, 80, 1, 1, 1, 'CONFIRMED', 9900, ?, 0, 0, ?, ?)`,
		fmtTime(testBase.Add(5*time.Minute)), fmtTime(testBase), fmtTime(testBase))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	if err := MarkNoShow(context.Background(), db, id, 1, "未到店", testBase.Add(3*time.Hour)); err == nil {
		t.Fatal("结束时间损坏时应返回错误")
	}
}

// 取消申请唯一约束冲突（应用层预检之后仍可能发生）必须映射为 CANCEL_ALREADY_PENDING。
func TestCancelRequestConstraintFallback(t *testing.T) {
	db := testDB(t)
	r := confirmedReservation(t, db, time.Hour, 1, 1)
	if _, err := db.Exec(`CREATE TRIGGER dup_cancel BEFORE INSERT ON cancel_request
		BEGIN SELECT RAISE(ABORT, 'UNIQUE constraint failed: cancel_request.reservation_id'); END`); err != nil {
		t.Fatal(err)
	}
	_, err := CancelReservation(context.Background(), db, r.ID, 1, "临时有事", testBase.Add(time.Minute), 2*time.Hour)
	wantBizCode(t, err, CodeCancelAlreadyPending)
}

// 退款单写入失败：自动取消必须整体回滚，不能出现“已取消却没有退款单”。
func TestRefundInsertFailureRollsBack(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := confirmedReservation(t, db, 4*time.Hour, 1, 1)
	if _, err := db.Exec(`CREATE TRIGGER fail_refund BEFORE INSERT ON refund
		BEGIN SELECT RAISE(ABORT, 'refund down'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := CancelReservation(ctx, db, r.ID, 1, "", testBase.Add(time.Minute), 2*time.Hour); err == nil {
		t.Fatal("退款单写入失败时应返回错误")
	}
	if st, _ := reservationStatus(t, db, r.ID); st != model.ResConfirmed {
		t.Fatalf("事务应回滚，预约仍为已确认，得到 %s", st)
	}
	if n := reservedCount(t, db, r.SlotID); n != 1 {
		t.Fatalf("名额不应释放，得到 %d", n)
	}
}

// 日清在支付/退款表不可用时返回错误（不得给出错误的金额勾稽结果）。
func TestDailyReportBrokenTables(t *testing.T) {
	ctx := context.Background()

	db1 := testDB(t)
	if _, err := db1.Exec(`DROP TABLE refund`); err != nil {
		t.Fatal(err)
	}
	if _, err := GetDailyReport(ctx, db1, 1, testBase); err == nil {
		t.Error("退款表不可用时日清应返回错误")
	}

	db2 := testDB(t)
	if _, err := db2.Exec(`DROP TABLE payment`); err != nil {
		t.Fatal(err)
	}
	if _, err := GetDailyReport(ctx, db2, 1, testBase); err == nil {
		t.Error("支付表不可用时日清应返回错误")
	}
}

// 预约表不可用时核销返回错误（不得误判为成功或 panic）。
func TestCheckInBrokenReservationTable(t *testing.T) {
	db := testDB(t)
	for _, table := range []string{"checkin_record", "service_record", "refund", "cancel_request", "payment_anomaly", "payment", "reservation"} {
		if _, err := db.Exec(`DROP TABLE ` + table); err != nil {
			t.Fatalf("删除表 %s: %v", table, err)
		}
	}
	if _, err := CheckIn(context.Background(), db, 1, 2, testBase); err == nil {
		t.Fatal("预约表不可用时应返回错误")
	}
}

// 占位截止时间损坏时不得发起支付（防止绕过 5 分钟窗口）。
func TestCreatePaymentInvalidExpireTime(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	res, err := db.Exec(`INSERT INTO reservation(store_id, slot_id, user_id, pet_id, item_id, status, price_cents,
		pay_expire_at, seat_released, version, created_at, updated_at)
		VALUES(1, ?, 1, 1, 1, 'PENDING_PAYMENT', 9900, 'zzz', 0, 0, ?, ?)`,
		slotID, fmtTime(testBase), fmtTime(testBase))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	_, err = CreatePayment(context.Background(), db, id, 1, testBase)
	if err == nil {
		t.Fatal("占位截止时间损坏时应返回错误")
	}
}

// 时段开始时间损坏时，取消流程必须报错而不是按零值时间自动受理。
func TestCancelInvalidSlotStart(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`INSERT INTO slot(id, store_id, item_id, start_at, end_at, capacity, reserved_count, created_by, created_at)
		VALUES(81, 1, 1, 'zzz', 'zzzz', 3, 1, 1, ?)`, fmtTime(testBase)); err != nil {
		t.Fatal(err)
	}
	res, err := db.Exec(`INSERT INTO reservation(store_id, slot_id, user_id, pet_id, item_id, status, price_cents,
		pay_expire_at, seat_released, version, created_at, updated_at)
		VALUES(1, 81, 1, 1, 1, 'CONFIRMED', 9900, ?, 0, 0, ?, ?)`,
		fmtTime(testBase.Add(5*time.Minute)), fmtTime(testBase), fmtTime(testBase))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	if _, err := CancelReservation(context.Background(), db, id, 1, "临时有事", testBase.Add(time.Minute), 2*time.Hour); err == nil {
		t.Fatal("开始时间损坏时应返回错误")
	}
}

// 数据库不可用时，所有业务入口必须返回错误而不是 panic（异常路径）。
func TestDataLayerErrorsArePropagated(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	checks := map[string]func() error{
		"CreateSlot": func() error {
			_, err := CreateSlot(ctx, db, CreateSlotParams{ActorID: 1, StoreID: 1, ItemID: 1,
				StartAt: testBase.Add(time.Hour), EndAt: testBase.Add(2 * time.Hour), Capacity: 3}, testBase)
			return err
		},
		"CreateReservation": func() error {
			_, err := CreateReservation(ctx, db, CreateReservationParams{SlotID: 1, UserID: 1, PetID: 1}, testBase, 5*time.Minute)
			return err
		},
		"CancelUnpaid":       func() error { return CancelUnpaid(ctx, db, 1, 1, testBase) },
		"CancelReservation":  func() error { _, e := CancelReservation(ctx, db, 1, 1, "x", testBase, 2*time.Hour); return e },
		"ReviewCancel":       func() error { return ReviewCancel(ctx, db, 1, 4, true, "", testBase) },
		"CheckIn":            func() error { _, e := CheckIn(ctx, db, 1, 2, testBase); return e },
		"StartService":       func() error { _, e := StartService(ctx, db, 1, 3, testBase); return e },
		"CompleteService":    func() error { _, e := CompleteService(ctx, db, 1, 3, testBase); return e },
		"MarkNoShow":         func() error { return MarkNoShow(ctx, db, 1, 1, "x", testBase) },
		"CreatePayment":      func() error { _, e := CreatePayment(ctx, db, 1, 1, testBase); return e },
		"HandlePayNotify":    func() error { _, e := HandlePayNotify(ctx, db, "t", "o", true, testBase); return e },
		"HandleRefundNotify": func() error { _, e := HandleRefundNotify(ctx, db, "r", true, "", testBase); return e },
		"RetryRefund":        func() error { _, e := RetryRefund(ctx, db, 1, 4, testBase); return e },
		"GetReservation":     func() error { _, e := GetReservation(ctx, db, 1, 1); return e },
		"ListMyReservations": func() error { _, e := ListMyReservations(ctx, db, 1, 10); return e },
		"ListCancelRequests": func() error { _, e := ListCancelRequests(ctx, db, 1, ""); return e },
		"ListRefunds":        func() error { _, e := ListRefunds(ctx, db, 1, true); return e },
		"ListSlots":          func() error { _, e := ListAvailableSlots(ctx, db, 1, testBase); return e },
		"DailyReport":        func() error { _, e := GetDailyReport(ctx, db, 1, testBase); return e },
		"SweepExpired":       func() error { _, e := SweepExpired(ctx, db, testBase); return e },
	}
	for name, fn := range checks {
		if err := fn(); err == nil {
			t.Errorf("%s 在数据库不可用时应返回错误", name)
		}
	}
}
