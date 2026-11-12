package biz

import (
	"context"
	"sync"
	"testing"
	"time"

	"paw-book/internal/model"
)

// —— 核销正常流程与条件校验（R16/R18/R20） ————————————

func TestCheckInOK(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := confirmedReservation(t, db, 2*time.Hour, 1, 1)

	if err := CheckIn(ctx, db, r.ID, 2, testBase.Add(90*time.Minute)); err != nil {
		t.Fatalf("本店前台核销应成功: %v", err)
	}
	if s, _ := reservationStatus(t, db, r.ID); s != model.ResCheckedIn {
		t.Fatalf("应为已核销，得到 %s", s)
	}
	var checkedAt string
	if err := db.QueryRow(`SELECT checked_in_at FROM reservations WHERE id=?`, r.ID).Scan(&checkedAt); err != nil || checkedAt == "" {
		t.Fatalf("应记录核销时间: %q err=%v", checkedAt, err)
	}
}

func TestCheckInRoleAndStore(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := confirmedReservation(t, db, 2*time.Hour, 1, 1)

	// 非前台角色：店长、服务人员、客服都不能核销（R16）
	for _, empID := range []int64{1, 3, 4} {
		if err := CheckIn(ctx, db, r.ID, empID, testBase); err == nil {
			t.Fatalf("员工 %d 不应能核销", empID)
		}
	}
	// 非本店前台（R20）
	if _, err := db.Exec(`INSERT INTO stores(id, name) VALUES(2, '别家门店')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO employees(id, store_id, name, role) VALUES(8, 2, '别家前台', 'front')`); err != nil {
		t.Fatal(err)
	}
	if err := CheckIn(ctx, db, r.ID, 8, testBase); err == nil {
		t.Fatal("非本店前台不应能核销")
	}
	// 本店前台成功
	if err := CheckIn(ctx, db, r.ID, 2, testBase); err != nil {
		t.Fatal(err)
	}
}

func TestCheckInInvalidStates(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// 待支付不可核销
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	pending := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	err := CheckIn(ctx, db, pending.ID, 2, testBase)
	wantBizCode(t, err, CodeInvalidState)

	// 取消审核中不可核销（R11：审核期间禁止核销）
	reviewed := confirmedReservation(t, db, time.Hour, 1, 1)
	if _, err := CancelReservation(ctx, db, reviewed.ID, 1, "临时有事", testBase.Add(time.Minute), 2*time.Hour); err != nil {
		t.Fatal(err)
	}
	err = CheckIn(ctx, db, reviewed.ID, 2, testBase.Add(2*time.Minute))
	wantBizCode(t, err, CodeInvalidState)

	// 已取消不可核销
	cancelled := confirmedReservation(t, db, 4*time.Hour, 1, 1)
	if _, err := CancelReservation(ctx, db, cancelled.ID, 1, "", testBase.Add(time.Minute), 2*time.Hour); err != nil {
		t.Fatal(err)
	}
	err = CheckIn(ctx, db, cancelled.ID, 2, testBase.Add(2*time.Minute))
	wantBizCode(t, err, CodeInvalidState)

	// 重复核销：第二次无效（R18）
	r := confirmedReservation(t, db, 2*time.Hour, 1, 1)
	if err := CheckIn(ctx, db, r.ID, 2, testBase); err != nil {
		t.Fatal(err)
	}
	err = CheckIn(ctx, db, r.ID, 2, testBase.Add(time.Minute))
	wantBizCode(t, err, CodeInvalidState)
}

// —— 服务开始/完成（R17/R18） ————————————————————

func TestServiceProgression(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := confirmedReservation(t, db, 2*time.Hour, 1, 1)

	if err := CheckIn(ctx, db, r.ID, 2, testBase); err != nil {
		t.Fatal(err)
	}
	// 前台不能登记服务开始（R17：服务人员职责）
	err := StartService(ctx, db, r.ID, 2, testBase.Add(time.Minute))
	wantBizCode(t, err, CodePermission)

	if err := StartService(ctx, db, r.ID, 3, testBase.Add(time.Minute)); err != nil {
		t.Fatalf("服务人员开始服务应成功: %v", err)
	}
	if s, _ := reservationStatus(t, db, r.ID); s != model.ResInService {
		t.Fatalf("应为服务中，得到 %s", s)
	}
	// 重复开始无效
	err = StartService(ctx, db, r.ID, 3, testBase.Add(2*time.Minute))
	wantBizCode(t, err, CodeInvalidState)

	if err := CompleteService(ctx, db, r.ID, 3, testBase.Add(30*time.Minute)); err != nil {
		t.Fatalf("完成服务应成功: %v", err)
	}
	if s, _ := reservationStatus(t, db, r.ID); s != model.ResCompleted {
		t.Fatalf("应为已完成，得到 %s", s)
	}
	// 重复完成无效：不重复记收入（R18）
	err = CompleteService(ctx, db, r.ID, 3, testBase.Add(31*time.Minute))
	wantBizCode(t, err, CodeInvalidState)
	// 已完成后不能再取消（R15）
	_, err = CancelReservation(ctx, db, r.ID, 1, "太晚了", testBase.Add(32*time.Minute), 2*time.Hour)
	wantBizCode(t, err, CodeInvalidState)
}

// —— 核销与取消并发：恰有一个成功（R19/I5） ————————————

func TestConcurrentCheckInVsCancel(t *testing.T) {
	const rounds = 10
	for i := 0; i < rounds; i++ {
		db := testDB(t)
		ctx := context.Background()
		r := confirmedReservation(t, db, 4*time.Hour, 1, 1) // ≥2h：取消走自动受理

		var wg sync.WaitGroup
		var checkInWin, cancelWin bool
		var mu sync.Mutex
		wg.Add(2)
		go func() {
			defer wg.Done()
			err := CheckIn(ctx, db, r.ID, 2, testBase)
			mu.Lock()
			checkInWin = err == nil
			mu.Unlock()
		}()
		go func() {
			defer wg.Done()
			_, err := CancelReservation(ctx, db, r.ID, 1, "", testBase, 2*time.Hour)
			mu.Lock()
			cancelWin = err == nil
			mu.Unlock()
		}()
		wg.Wait()

		if checkInWin == cancelWin {
			t.Fatalf("第 %d 轮：核销与取消应恰有一个成功（核销=%v 取消=%v）", i, checkInWin, cancelWin)
		}
		st, _ := reservationStatus(t, db, r.ID)
		switch st {
		case model.ResCheckedIn:
			if !checkInWin {
				t.Fatalf("第 %d 轮：状态为已核销但赢方记录不符", i)
			}
		case model.ResCancelled:
			if !cancelWin {
				t.Fatalf("第 %d 轮：状态为已取消但赢方记录不符", i)
			}
			// 取消获胜时应有退款单，金额记录未被破坏
			var n int
			if err := db.QueryRow(`SELECT COUNT(*) FROM refunds WHERE reservation_id=? AND amount_cents=?`, r.ID, 9900).Scan(&n); err != nil || n != 1 {
				t.Fatalf("第 %d 轮：取消获胜应有 1 笔 9900 分退款，得到 %d err=%v", i, n, err)
			}
		default:
			t.Fatalf("第 %d 轮：终态应为已核销或已取消，得到 %s", i, st)
		}
		db.Close()
	}
}

// —— 审核请求与核销：先到先得 ————————————————————

func TestReviewRequestBlocksCheckIn(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := confirmedReservation(t, db, time.Hour, 1, 1)

	// 用户先提交取消申请 → 审核中
	if _, err := CancelReservation(ctx, db, r.ID, 1, "临时有事", testBase.Add(time.Minute), 2*time.Hour); err != nil {
		t.Fatal(err)
	}
	// 前台核销被拒（R11）
	err := CheckIn(ctx, db, r.ID, 2, testBase.Add(2*time.Minute))
	wantBizCode(t, err, CodeInvalidState)

	// 客服拒绝后核销恢复可用（R12）
	var reviewID int64
	if err := db.QueryRow(`SELECT id FROM cancel_reviews WHERE reservation_id=?`, r.ID).Scan(&reviewID); err != nil {
		t.Fatal(err)
	}
	if err := ReviewCancel(ctx, db, reviewID, 4, false, testBase.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := CheckIn(ctx, db, r.ID, 2, testBase.Add(4*time.Minute)); err != nil {
		t.Fatalf("拒绝后应可核销: %v", err)
	}
}

// —— 退款重试（T8，R14/A3） ——————————————————————

func TestRetryRefund(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := confirmedReservation(t, db, 4*time.Hour, 1, 1)

	// 提前取消产生退款
	cres, err := CancelReservation(ctx, db, r.ID, 1, "", testBase.Add(time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	refundID := cres.RefundID
	refundNo := refundNoOf(t, db, refundID)

	// 渠道退款失败
	if _, err := HandleRefundNotify(ctx, db, refundNo, false, "渠道超时", testBase.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if st, _, retry := refundStatusOf(t, db, refundID); st != model.RefundFailed || retry != 1 {
		t.Fatalf("退款应失败且重试计数 1，得到 %s/%d", st, retry)
	}

	// 非客服不能重试
	_, err = RetryRefund(ctx, db, refundID, 3, testBase.Add(3*time.Minute))
	wantBizCode(t, err, CodePermission)

	// 客服重试：置回处理中
	if _, err := RetryRefund(ctx, db, refundID, 4, testBase.Add(4*time.Minute)); err != nil {
		t.Fatalf("客服重试应成功: %v", err)
	}
	// 重试后成功回调
	if _, err := HandleRefundNotify(ctx, db, refundNo, true, "", testBase.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if st, _, _ := refundStatusOf(t, db, refundID); st != model.RefundSuccess {
		t.Fatalf("重试后应成功，得到 %s", st)
	}
	// 已成功的退款不能再重试
	_, err = RetryRefund(ctx, db, refundID, 4, testBase.Add(6*time.Minute))
	wantBizCode(t, err, CodeInvalidState)
	// 退款成功不影响预约终态（I6）：预约仍是已取消
	if s, _ := reservationStatus(t, db, r.ID); s != model.ResCancelled {
		t.Fatalf("预约应保持已取消，得到 %s", s)
	}
}

func TestListFailedRefunds(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r1 := confirmedReservation(t, db, 4*time.Hour, 1, 1)
	r2 := confirmedReservation(t, db, 5*time.Hour, 2, 3)

	// r1：取消 + 退款失败（待跟进）
	c1, err := CancelReservation(ctx, db, r1.ID, 1, "", testBase.Add(time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	no1 := refundNoOf(t, db, c1.RefundID)
	if _, err := HandleRefundNotify(ctx, db, no1, false, "渠道超时", testBase.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	// r2：取消 + 退款成功
	c2, err := CancelReservation(ctx, db, r2.ID, 2, "", testBase.Add(time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	no2 := refundNoOf(t, db, c2.RefundID)
	if _, err := HandleRefundNotify(ctx, db, no2, true, "", testBase.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	list, err := ListFailedRefunds(ctx, db, 1)
	if err != nil {
		t.Fatal(err)
	}
	// 待跟进 = 失败的 r1；成功的 r2 不在列表
	if len(list) != 1 || list[0].ReservationID != r1.ID || list[0].FailReason != "渠道超时" {
		t.Fatalf("应恰有 1 条待跟进退款（r1），得到 %+v", list)
	}
}
