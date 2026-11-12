package biz

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"paw-book/internal/model"
)

func refundOf(t *testing.T, db *sql.DB, reservationID int64) (int64, string, int, string) {
	t.Helper()
	var id int64
	var status, lastErr string
	var retry int
	if err := db.QueryRow(`SELECT id, status, retry_count, COALESCE(last_error,'') FROM refund WHERE reservation_id=?`,
		reservationID).Scan(&id, &status, &retry, &lastErr); err != nil {
		t.Fatalf("查询退款单（预约 %d）: %v", reservationID, err)
	}
	return id, status, retry, lastErr
}

func refundNoByID(t *testing.T, db *sql.DB, id int64) string {
	t.Helper()
	var no string
	if err := db.QueryRow(`SELECT refund_no FROM refund WHERE id=?`, id).Scan(&no); err != nil {
		t.Fatalf("查询退款单号: %v", err)
	}
	return no
}

// —— R4.3/R4.4 退款状态独立、失败可重试且不影响预约 ————————

func TestRefundNotifyAndRetryLifecycle(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := confirmedReservation(t, db, 4*time.Hour, 1, 1)

	cres, err := CancelReservation(ctx, db, r.ID, 1, "", testBase.Add(time.Minute), 2*time.Hour)
	if err != nil || !cres.AutoAccepted || cres.RefundID == 0 {
		t.Fatalf("提前取消应自动受理并创建退款: %+v err=%v", cres, err)
	}
	refundID := cres.RefundID

	// 第一次退款回调：渠道超时 → FAILED，预约保持已取消（I6）
	res, err := HandleRefundNotify(ctx, db, refundNoByID(t, db, refundID), false, "渠道超时", testBase.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if res.Result != model.RefundFailed {
		t.Fatalf("退款回调失败结果应为 FAILED，得到 %s", res.Result)
	}
	id, status, retry, lastErr := refundOf(t, db, r.ID)
	if id != refundID || status != model.RefundFailed || retry != 0 || lastErr != "渠道超时" {
		t.Fatalf("退款应记失败与原因、重试次数不变: id=%d status=%s retry=%d err=%s", id, status, retry, lastErr)
	}
	if st, _ := reservationStatus(t, db, r.ID); st != model.ResCancelled {
		t.Fatalf("退款失败不得改变预约状态，得到 %s", st)
	}

	// 客服重试：FAILED → PROCESSING，retry_count+1（D4 口径：客服主动重试次数）
	no, err := RetryRefund(ctx, db, refundID, 4, testBase.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("客服重试应成功: %v", err)
	}
	if no != refundNoByID(t, db, refundID) {
		t.Fatalf("应返回退款单号")
	}
	if _, status, retry, _ := refundOf(t, db, r.ID); status != model.RefundProcessing || retry != 1 {
		t.Fatalf("重试后应为 PROCESSING/retry=1，得到 %s/%d", status, retry)
	}

	// 渠道回调成功 → SUCCESS
	res, err = HandleRefundNotify(ctx, db, no, true, "", testBase.Add(4*time.Minute))
	if err != nil || res.Result != model.RefundSuccess {
		t.Fatalf("退款成功回调失败: %+v err=%v", res, err)
	}
	if _, status, _, lastErr := refundOf(t, db, r.ID); status != model.RefundSuccess || lastErr != "" {
		t.Fatalf("退款应为 SUCCESS 且清空错误，得到 %s/%s", status, lastErr)
	}
	// 重复成功通知幂等
	dup, err := HandleRefundNotify(ctx, db, no, true, "", testBase.Add(5*time.Minute))
	if err != nil || dup.Result != model.NotifyDuplicate {
		t.Fatalf("重复退款成功通知应幂等: %+v err=%v", dup, err)
	}
	// 已取消预约不可核销（R4.4）
	_, err = CheckIn(ctx, db, r.ID, 2, testBase.Add(6*time.Minute))
	wantBizCode(t, err, CodeCheckinNotAllow)
}

func TestRetryRefundPermissions(t *testing.T) {
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

	// 前台不能重试退款
	_, err = RetryRefund(ctx, db, cres.RefundID, 2, testBase.Add(3*time.Minute))
	wantBizCode(t, err, CodeRoleForbidden)
	// 别店客服不能重试
	_, err = RetryRefund(ctx, db, cres.RefundID, 9, testBase.Add(3*time.Minute))
	wantBizCode(t, err, CodeStoreMismatch)
	// 退款单不存在
	_, err = RetryRefund(ctx, db, 9999, 4, testBase.Add(3*time.Minute))
	wantBizCode(t, err, CodeNotFound)
	// 重试成功后不能再重试（PROCESSING 不在失败/待处理之列）
	if _, err := RetryRefund(ctx, db, cres.RefundID, 4, testBase.Add(3*time.Minute)); err != nil {
		t.Fatalf("首次重试应成功: %v", err)
	}
	_, err = RetryRefund(ctx, db, cres.RefundID, 4, testBase.Add(4*time.Minute))
	wantBizCode(t, err, CodeRefundNotRetry)
}

func TestListRefundsScopeAndFilter(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	// 两个已取消预约，一个退款失败、一个退款成功
	r1 := confirmedReservation(t, db, 4*time.Hour, 1, 1)
	c1, err := CancelReservation(ctx, db, r1.ID, 1, "", testBase.Add(time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := HandleRefundNotify(ctx, db, refundNoByID(t, db, c1.RefundID), false, "", testBase.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	r2 := confirmedReservation(t, db, 6*time.Hour, 1, 2)
	c2, err := CancelReservation(ctx, db, r2.ID, 1, "", testBase.Add(time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := HandleRefundNotify(ctx, db, refundNoByID(t, db, c2.RefundID), true, "", testBase.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	all, err := ListRefunds(ctx, db, 1, false)
	if err != nil || len(all) != 2 {
		t.Fatalf("本店应有 2 条退款，得到 %d err=%v", len(all), err)
	}
	unfinished, err := ListRefunds(ctx, db, 1, true)
	if err != nil || len(unfinished) != 1 || unfinished[0].Status != model.RefundFailed {
		t.Fatalf("未完成退款应只有 1 条失败单: %+v err=%v", unfinished, err)
	}
	other, err := ListRefunds(ctx, db, 2, false)
	if err != nil || len(other) != 0 {
		t.Fatalf("别店不应看到本店退款，得到 %d err=%v", len(other), err)
	}
}
