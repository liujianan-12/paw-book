package biz

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"paw-book/internal/model"
)

// 本文件覆盖"数据库写失败 → 事务回滚 → 明确错误"的异常路径（04 §4：失败不得留下部分数据）。

func addPets(t *testing.T, db *sql.DB, from, to int64, userID int64) {
	t.Helper()
	for i := from; i <= to; i++ {
		if _, err := db.Exec(`INSERT INTO pet(id, user_id, name, breed) VALUES(?,?,?, '串')`,
			i, userID, "宠物"); err != nil {
			t.Fatalf("插入宠物 %d: %v", i, err)
		}
	}
}

// 审计表写失败：各写操作的审计分支必须返回错误并回滚业务写入。
func TestAuditFailureRollsBack(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	addPets(t, db, 20, 40, 1)

	slotPending1 := mustCreateSlot(t, db, 6*time.Hour, time.Hour, 3)
	slotPending2 := mustCreateSlot(t, db, 7*time.Hour, time.Hour, 3)
	pendingPay := mustCreateReservation(t, db, slotPending1, 1, 20, testBase)
	pendingCancel := mustCreateReservation(t, db, slotPending2, 1, 21, testBase)

	// 预置各状态实体（在安装触发器之前完成）
	checkinTarget := confirmedReservation(t, db, 8*time.Hour, 1, 22)
	startTarget := confirmedReservation(t, db, 9*time.Hour, 1, 23)
	if _, err := CheckIn(ctx, db, startTarget.ID, 2, testBase.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	completeTarget := confirmedReservation(t, db, 10*time.Hour, 1, 24)
	if _, err := CheckIn(ctx, db, completeTarget.ID, 2, testBase.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := StartService(ctx, db, completeTarget.ID, 3, testBase.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	noShowTarget := confirmedReservation(t, db, time.Hour, 1, 25)
	autoCancelTarget := confirmedReservation(t, db, 12*time.Hour, 1, 26)
	reviewTarget := confirmedReservation(t, db, time.Hour, 1, 27)
	reviewReq, err := CancelReservation(ctx, db, reviewTarget.ID, 1, "临时有事", testBase.Add(time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	refundTarget := confirmedReservation(t, db, 14*time.Hour, 1, 28)
	refundRes, err := CancelReservation(ctx, db, refundTarget.ID, 1, "", testBase.Add(time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := HandleRefundNotify(ctx, db, refundNoByID(t, db, refundRes.RefundID), false, "", testBase.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	notifyTarget := mustCreateReservation(t, db, mustCreateSlot(t, db, 15*time.Hour, time.Hour, 3), 1, 29, testBase)
	notifyPay := mustCreatePayment(t, db, notifyTarget.ID, 1, testBase)
	createSlotForReservation := mustCreateSlot(t, db, 22*time.Hour, time.Hour, 3)

	// 审计写入失败
	if _, err := db.Exec(`CREATE TRIGGER fail_audit BEFORE INSERT ON operation_log BEGIN SELECT RAISE(ABORT, 'audit down'); END`); err != nil {
		t.Fatal(err)
	}

	ops := map[string]func() error{
		"CreateSlot": func() error {
			_, e := CreateSlot(ctx, db, CreateSlotParams{ActorID: 1, StoreID: 1, ItemID: 1,
				StartAt: testBase.Add(20 * time.Hour), EndAt: testBase.Add(21 * time.Hour), Capacity: 3}, testBase)
			return e
		},
		"CreateReservation": func() error {
			_, e := CreateReservation(ctx, db, CreateReservationParams{
				SlotID: createSlotForReservation, UserID: 1, PetID: 30}, testBase, 5*time.Minute)
			return e
		},
		"CancelUnpaid":  func() error { return CancelUnpaid(ctx, db, pendingCancel.ID, 1, testBase.Add(time.Minute)) },
		"CreatePayment": func() error { _, e := CreatePayment(ctx, db, pendingPay.ID, 1, testBase.Add(time.Minute)); return e },
		"CheckIn":       func() error { _, e := CheckIn(ctx, db, checkinTarget.ID, 2, testBase.Add(time.Minute)); return e },
		"StartService":  func() error { _, e := StartService(ctx, db, startTarget.ID, 3, testBase.Add(3*time.Minute)); return e },
		"CompleteService": func() error {
			_, e := CompleteService(ctx, db, completeTarget.ID, 3, testBase.Add(4*time.Minute))
			return e
		},
		"MarkNoShow": func() error { return MarkNoShow(ctx, db, noShowTarget.ID, 1, "未到店", testBase.Add(3*time.Hour)) },
		"CancelAuto": func() error {
			_, e := CancelReservation(ctx, db, autoCancelTarget.ID, 1, "", testBase.Add(time.Minute), 2*time.Hour)
			return e
		},
		"ReviewCancel": func() error {
			return ReviewCancel(ctx, db, reviewReq.RequestID, 4, true, "", testBase.Add(3*time.Minute))
		},
		"RefundNotify": func() error {
			_, e := HandleRefundNotify(ctx, db, refundNoByID(t, db, refundRes.RefundID), true, "", testBase.Add(3*time.Minute))
			return e
		},
		"RetryRefund": func() error {
			_, e := RetryRefund(ctx, db, refundRes.RefundID, 4, testBase.Add(3*time.Minute))
			return e
		},
		"PayNotify": func() error {
			_, e := HandlePayNotify(ctx, db, "txn-fail-audit", notifyPay.OrderNo, true, testBase.Add(time.Minute))
			return e
		},
	}
	for name, fn := range ops {
		if err := fn(); err == nil {
			t.Errorf("%s 在审计写入失败时应返回错误", name)
		}
	}

	// 关键业务状态未被部分写入（回滚生效）
	if st, _ := reservationStatus(t, db, checkinTarget.ID); st != model.ResConfirmed {
		t.Fatalf("核销失败后预约应保持已确认，得到 %s", st)
	}
	if st, _ := reservationStatus(t, db, autoCancelTarget.ID); st != model.ResConfirmed {
		t.Fatalf("取消失败后预约应保持已确认，得到 %s", st)
	}
	if st, _ := reservationStatus(t, db, reviewTarget.ID); st != model.ResCancellationReview {
		t.Fatalf("审核失败后预约状态不应变化，得到 %s", st)
	}
	if _, status, _, _ := refundOf(t, db, refundTarget.ID); status != model.RefundFailed {
		t.Fatalf("退款回调失败后应保持原状态，得到 %s", status)
	}
}

// 预约表更新失败：核销/开始/完成/未到店/未付款取消返回错误。
func TestReservationUpdateFailurePropagates(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	addPets(t, db, 41, 45, 1)

	checkinTarget := confirmedReservation(t, db, 3*time.Hour, 1, 41)
	startTarget := confirmedReservation(t, db, 4*time.Hour, 1, 42)
	if _, err := CheckIn(ctx, db, startTarget.ID, 2, testBase.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	completeTarget := confirmedReservation(t, db, 5*time.Hour, 1, 43)
	if _, err := CheckIn(ctx, db, completeTarget.ID, 2, testBase.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := StartService(ctx, db, completeTarget.ID, 3, testBase.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	noShowTarget := confirmedReservation(t, db, time.Hour, 1, 44)
	pending := mustCreateReservation(t, db, mustCreateSlot(t, db, 6*time.Hour, time.Hour, 3), 1, 45, testBase)

	if _, err := db.Exec(`CREATE TRIGGER fail_res_update BEFORE UPDATE ON reservation BEGIN SELECT RAISE(ABORT, 'res down'); END`); err != nil {
		t.Fatal(err)
	}
	checks := map[string]func() error{
		"CheckIn":      func() error { _, e := CheckIn(ctx, db, checkinTarget.ID, 2, testBase.Add(time.Minute)); return e },
		"StartService": func() error { _, e := StartService(ctx, db, startTarget.ID, 3, testBase.Add(2*time.Minute)); return e },
		"CompleteService": func() error {
			_, e := CompleteService(ctx, db, completeTarget.ID, 3, testBase.Add(3*time.Minute))
			return e
		},
		"MarkNoShow":   func() error { return MarkNoShow(ctx, db, noShowTarget.ID, 1, "未到店", testBase.Add(3*time.Hour)) },
		"CancelUnpaid": func() error { return CancelUnpaid(ctx, db, pending.ID, 1, testBase.Add(time.Minute)) },
	}
	for name, fn := range checks {
		if err := fn(); err == nil {
			t.Errorf("%s 在预约更新失败时应返回错误", name)
		}
	}
}

// 并发败者分支：条件更新影响 0 行时必须给出明确业务码（R6.1）。
// 用 RAISE(IGNORE) 让状态更新“静默跳过”，模拟对手先改了状态。
func TestConditionalUpdateLosesRace(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	addPets(t, db, 61, 63, 1)

	checkinTarget := confirmedReservation(t, db, 3*time.Hour, 1, 61)
	startTarget := confirmedReservation(t, db, 4*time.Hour, 1, 62)
	if _, err := CheckIn(ctx, db, startTarget.ID, 2, testBase.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	completeTarget := confirmedReservation(t, db, 5*time.Hour, 1, 63)
	if _, err := CheckIn(ctx, db, completeTarget.ID, 2, testBase.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := StartService(ctx, db, completeTarget.ID, 3, testBase.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(`CREATE TRIGGER skip_checkin BEFORE UPDATE ON reservation
		WHEN NEW.status='CHECKED_IN' BEGIN SELECT RAISE(IGNORE); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER skip_start BEFORE UPDATE ON reservation
		WHEN NEW.status='IN_SERVICE' BEGIN SELECT RAISE(IGNORE); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER skip_complete BEFORE UPDATE ON reservation
		WHEN NEW.status='COMPLETED' BEGIN SELECT RAISE(IGNORE); END`); err != nil {
		t.Fatal(err)
	}

	_, err := CheckIn(ctx, db, checkinTarget.ID, 2, testBase.Add(2*time.Minute))
	wantBizCode(t, err, CodeCheckinNotAllow)
	_, err = StartService(ctx, db, startTarget.ID, 3, testBase.Add(2*time.Minute))
	wantBizCode(t, err, CodeStateInvalid)
	_, err = CompleteService(ctx, db, completeTarget.ID, 3, testBase.Add(3*time.Minute))
	wantBizCode(t, err, CodeStateInvalid)
}

// 已存在另一条成功支付时，旧流水的成功回调不得再记收款（R2.5/I13）。
func TestPayNotifyWhenAnotherPaymentAlreadySucceeded(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	p := mustCreatePayment(t, db, r.ID, 1, testBase)
	if _, err := db.Exec(`INSERT INTO payment(reservation_id, order_no, amount_cents, channel, status, created_at)
		VALUES(?, 'OTHER-OK', 9900, 'mock', 'SUCCESS', ?)`, r.ID, fmtTime(testBase)); err != nil {
		t.Fatal(err)
	}
	res, err := HandlePayNotify(ctx, db, "txn-other", p.OrderNo, true, testBase.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if res.Result != model.NotifyDuplicate {
		t.Fatalf("已有成功支付时应幂等忽略，得到 %s", res.Result)
	}
	if n := paymentSuccessCount(t, db, r.ID); n != 1 {
		t.Fatalf("成功支付仍应只有 1 条，得到 %d", n)
	}
}

// 时段表更新失败：名额释放阶段报错并回滚（I5：名额不得出现负数或双写）。
func TestSlotUpdateFailurePropagates(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	addPets(t, db, 51, 55, 1)

	autoTarget := confirmedReservation(t, db, 8*time.Hour, 1, 51)
	reviewTarget := confirmedReservation(t, db, time.Hour, 1, 52)
	req, err := CancelReservation(ctx, db, reviewTarget.ID, 1, "临时有事", testBase.Add(time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	noShowTarget := confirmedReservation(t, db, time.Hour, 1, 53)
	expireTarget := mustCreateReservation(t, db, mustCreateSlot(t, db, 9*time.Hour, time.Hour, 3), 1, 54, testBase)
	_ = expireTarget

	if _, err := db.Exec(`CREATE TRIGGER fail_slot_update BEFORE UPDATE ON slot BEGIN SELECT RAISE(ABORT, 'slot down'); END`); err != nil {
		t.Fatal(err)
	}
	checks := map[string]func() error{
		"CancelAuto": func() error {
			_, e := CancelReservation(ctx, db, autoTarget.ID, 1, "", testBase.Add(time.Minute), 2*time.Hour)
			return e
		},
		"ReviewApprove": func() error { return ReviewCancel(ctx, db, req.RequestID, 4, true, "", testBase.Add(3*time.Minute)) },
		"MarkNoShow":    func() error { return MarkNoShow(ctx, db, noShowTarget.ID, 1, "未到店", testBase.Add(3*time.Hour)) },
		"SweepExpired":  func() error { _, e := SweepExpired(ctx, db, testBase.Add(6*time.Minute)); return e },
	}
	for name, fn := range checks {
		if err := fn(); err == nil {
			t.Errorf("%s 在名额更新失败时应返回错误", name)
		}
	}
}
