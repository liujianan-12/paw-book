package biz

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"paw-book/internal/model"
)

func cancelRequestStatus(t *testing.T, db *sql.DB, requestID int64) (string, string) {
	t.Helper()
	var status string
	var note sql.NullString
	if err := db.QueryRow(`SELECT status, decision_note FROM cancel_request WHERE id=?`, requestID).
		Scan(&status, &note); err != nil {
		t.Fatalf("查询取消申请 %d: %v", requestID, err)
	}
	return status, note.String
}

// —— R3.1 提前 ≥2h：自动受理 + 全额退款 + 释放名额 ————————

func TestCancelEarlyAutoAccepted(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := confirmedReservation(t, db, 4*time.Hour, 1, 1)

	res, err := CancelReservation(ctx, db, r.ID, 1, "", testBase.Add(time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatalf("提前取消应自动受理: %v", err)
	}
	if !res.AutoAccepted || res.RefundID == 0 {
		t.Fatalf("应返回自动受理与退款单: %+v", res)
	}
	if st, ct := reservationStatus(t, db, r.ID); st != model.ResCancelled || ct != model.CancelAutoBefore2H {
		t.Fatalf("应为已取消/自动受理，得到 %s/%s", st, ct)
	}
	if _, status, _, _ := refundOf(t, db, r.ID); status != model.RefundPending {
		t.Fatalf("退款应待处理，得到 %s", status)
	}
	// 退款金额 = 支付金额 9900（I4）
	var amount int64
	if err := db.QueryRow(`SELECT amount_cents FROM refund WHERE reservation_id=?`, r.ID).Scan(&amount); err != nil || amount != 9900 {
		t.Fatalf("退款金额应为 9900，得到 %d err=%v", amount, err)
	}
	// 名额已释放：同宠物可再次预约
	if n := reservedCount(t, db, r.SlotID); n != 0 {
		t.Fatalf("名额应释放为 0，得到 %d", n)
	}
	mustCreateReservation(t, db, r.SlotID, 1, 1, testBase.Add(2*time.Minute))
}

// —— R3.1 2h 边界：=2h 自动受理，2h-1s 转审核 ————————————

func TestCancelTwoHourBoundary(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// 开始时间 08:00，取消时间 06:00 恰好提前 2h → 自动受理
	r1 := confirmedReservation(t, db, 2*time.Hour, 1, 1)
	res1, err := CancelReservation(ctx, db, r1.ID, 1, "", testBase, 2*time.Hour)
	if err != nil || !res1.AutoAccepted {
		t.Fatalf("=2h 应自动受理: %+v err=%v", res1, err)
	}

	// 开始时间 07:59:59，提前 2h-1s → 需原因转审核
	r2 := confirmedReservation(t, db, 2*time.Hour-time.Second, 2, 3)
	res2, err := CancelReservation(ctx, db, r2.ID, 2, "临时有事", testBase, 2*time.Hour)
	if err != nil || res2.AutoAccepted || res2.RequestID == 0 {
		t.Fatalf("2h-1s 应转审核: %+v err=%v", res2, err)
	}
}

// —— R3.2/R3.3 临近取消：必填原因、进入审核、名额保留 ———————

func TestCancelNearStartRequiresReasonAndKeepsSeat(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := confirmedReservation(t, db, time.Hour, 1, 1)

	wantBizCode(t, func() error {
		_, err := CancelReservation(ctx, db, r.ID, 1, "  ", testBase.Add(time.Minute), 2*time.Hour)
		return err
	}(), CodeCancelReason)

	res, err := CancelReservation(ctx, db, r.ID, 1, "宠物临时生病", testBase.Add(time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatalf("应进入审核: %v", err)
	}
	if res.AutoAccepted || res.RequestID == 0 {
		t.Fatalf("应返回审核单: %+v", res)
	}
	if st, _ := reservationStatus(t, db, r.ID); st != model.ResCancellationReview {
		t.Fatalf("预约应为取消审核中，得到 %s", st)
	}
	if n := reservedCount(t, db, r.SlotID); n != 1 {
		t.Fatalf("审核期间名额必须保留，得到 %d", n)
	}
	// 待审期间不得核销（R3.3）
	_, err = CheckIn(ctx, db, r.ID, 2, testBase.Add(2*time.Minute))
	wantBizCode(t, err, CodeCheckinPending)
	// 同宠物再约冲突（名额仍占用）
	_, err = CreateReservation(ctx, db, CreateReservationParams{SlotID: r.SlotID, UserID: 1, PetID: 1},
		testBase.Add(2*time.Minute), 5*time.Minute)
	wantBizCode(t, err, CodeDuplicateActive)
	// 待审中重复提交（A6/A10）
	_, err = CancelReservation(ctx, db, r.ID, 1, "再提一次", testBase.Add(3*time.Minute), 2*time.Hour)
	wantBizCode(t, err, CodeCancelAlreadyPending)
}

// —— R4.1/R4.2 客服批准：取消 + 退款 + 只释放一次 ——————————

func TestReviewApprove(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := confirmedReservation(t, db, time.Hour, 1, 1)
	res, err := CancelReservation(ctx, db, r.ID, 1, "临时出差", testBase.Add(time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	if err := ReviewCancel(ctx, db, res.RequestID, 4, true, "同意", testBase.Add(10*time.Minute)); err != nil {
		t.Fatalf("客服批准应成功: %v", err)
	}
	if st, ct := reservationStatus(t, db, r.ID); st != model.ResCancelled || ct != model.CancelCSApproved {
		t.Fatalf("应为已取消/客服批准，得到 %s/%s", st, ct)
	}
	if st, note := cancelRequestStatus(t, db, res.RequestID); st != model.CancelReqApproved || note != "同意" {
		t.Fatalf("申请应为批准并记录备注，得到 %s/%s", st, note)
	}
	if n := reservedCount(t, db, r.SlotID); n != 0 {
		t.Fatalf("名额应释放为 0，得到 %d", n)
	}
	if _, status, _, _ := refundOf(t, db, r.ID); status != model.RefundPending {
		t.Fatalf("批准后应创建退款，得到 %s", status)
	}
	// 批准后不可核销且无核销记录（R4.1）
	_, err = CheckIn(ctx, db, r.ID, 2, testBase.Add(11*time.Minute))
	wantBizCode(t, err, CodeCheckinNotAllow)
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM checkin_record WHERE reservation_id=?`, r.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("不应有核销记录，得到 %d err=%v", n, err)
	}
	// 重复批准：状态不符，名额不再释放（R4.2）
	wantBizCode(t, ReviewCancel(ctx, db, res.RequestID, 4, true, "", testBase.Add(12*time.Minute)), CodeCancelNotPend)
	if n := reservedCount(t, db, r.SlotID); n != 0 {
		t.Fatalf("名额不得二次释放，得到 %d", n)
	}
}

// —— R3.3/D3 客服拒绝：恢复已确认、名额不变、可重提 ——————————

func TestReviewRejectRestoresConfirmed(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := confirmedReservation(t, db, time.Hour, 1, 1)

	res, err := CancelReservation(ctx, db, r.ID, 1, "不想去了", testBase.Add(time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := ReviewCancel(ctx, db, res.RequestID, 4, false, "时段已排班", testBase.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if st, _ := reservationStatus(t, db, r.ID); st != model.ResConfirmed {
		t.Fatalf("拒绝后应恢复已确认，得到 %s", st)
	}
	if st, _ := cancelRequestStatus(t, db, res.RequestID); st != model.CancelReqRejected {
		t.Fatalf("申请应为拒绝，得到 %s", st)
	}
	if n := reservedCount(t, db, r.SlotID); n != 1 {
		t.Fatalf("拒绝不得释放名额，得到 %d", n)
	}
	// 拒绝后可再次提交（D3），历史申请保留
	res2, err := CancelReservation(ctx, db, r.ID, 1, "再次申请", testBase.Add(20*time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatalf("拒绝后应可再次申请: %v", err)
	}
	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cancel_request WHERE reservation_id=?`, r.ID).Scan(&total); err != nil || total != 2 {
		t.Fatalf("历史申请应保留 2 条，得到 %d err=%v", total, err)
	}
	if st, _ := cancelRequestStatus(t, db, res2.RequestID); st != model.CancelReqPending {
		t.Fatalf("新申请应为待处理，得到 %s", st)
	}
}

// —— R6.2 已核销/服务中/已完成不可取消 ——————————————

func TestCancelRejectedForCheckedInAndCompleted(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// 已核销
	r1 := confirmedReservation(t, db, time.Hour, 1, 1)
	if _, err := CheckIn(ctx, db, r1.ID, 2, testBase.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	_, err := CancelReservation(ctx, db, r1.ID, 1, "", testBase.Add(3*time.Minute), 2*time.Hour)
	wantBizCode(t, err, CodeCancelNotAllow)

	// 服务中
	r2 := confirmedReservation(t, db, time.Hour, 1, 2)
	if _, err := CheckIn(ctx, db, r2.ID, 2, testBase.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := StartService(ctx, db, r2.ID, 3, testBase.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	_, err = CancelReservation(ctx, db, r2.ID, 1, "", testBase.Add(4*time.Minute), 2*time.Hour)
	wantBizCode(t, err, CodeCancelNotAllow)

	// 已完成
	if _, err := CompleteService(ctx, db, r2.ID, 3, testBase.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	_, err = CancelReservation(ctx, db, r2.ID, 1, "", testBase.Add(6*time.Minute), 2*time.Hour)
	wantBizCode(t, err, CodeCancelNotAllow)

	// 均不产生退款
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM refund`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("三状态取消不得产生退款，得到 %d err=%v", n, err)
	}
}

// —— 权限：非本人、非客服、跨店、重复处理 ——————————————

func TestCancelAndReviewPermissions(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// 待支付预约不能走已确认取消流程
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	pending := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	_, err := CancelReservation(ctx, db, pending.ID, 1, "", testBase, 2*time.Hour)
	wantBizCode(t, err, CodeCancelNotAllow)

	// 非本人取消：按不存在处理，不泄露存在性（P2）
	r := confirmedReservation(t, db, 4*time.Hour, 1, 1)
	_, err = CancelReservation(ctx, db, r.ID, 2, "冒名取消", testBase, 2*time.Hour)
	wantBizCode(t, err, CodeNotFound)

	// 审核权限
	r2 := confirmedReservation(t, db, time.Hour, 2, 3)
	res, err := CancelReservation(ctx, db, r2.ID, 2, "宠物生病", testBase.Add(time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// 前台不能审核（角色不符）
	wantBizCode(t, ReviewCancel(ctx, db, res.RequestID, 2, true, "", testBase.Add(2*time.Minute)), CodeRoleForbidden)
	// 别店客服不能审核（门店不符）
	wantBizCode(t, ReviewCancel(ctx, db, res.RequestID, 9, true, "", testBase.Add(2*time.Minute)), CodeStoreMismatch)
	// 申请不存在
	wantBizCode(t, ReviewCancel(ctx, db, 9999, 4, true, "", testBase.Add(2*time.Minute)), CodeNotFound)
	// 本店客服可处理
	if err := ReviewCancel(ctx, db, res.RequestID, 4, true, "", testBase.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}

	// A12 列表：本店 + 状态过滤
	list, err := ListCancelRequests(ctx, db, 1, model.CancelReqApproved)
	if err != nil || len(list) != 1 {
		t.Fatalf("应返回 1 条已批准申请，得到 %d err=%v", len(list), err)
	}
	if other, err := ListCancelRequests(ctx, db, 2, ""); err != nil || len(other) != 0 {
		t.Fatalf("别店不应看到本店申请，得到 %d err=%v", len(other), err)
	}
}
