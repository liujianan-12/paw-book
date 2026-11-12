package biz

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"paw-book/internal/model"
)

// —— 测试辅助：生成一个已确认预约（已付款） ——————————

func confirmedReservation(t *testing.T, db *sql.DB, slotStartIn time.Duration, userID, petID int64) *model.Reservation {
	t.Helper()
	slotID := mustCreateSlot(t, db, slotStartIn, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, userID, petID, testBase)
	p := mustCreatePayment(t, db, r.ID, userID, testBase)
	payNotify(t, db, p.OutTradeNo, true, testBase.Add(time.Minute))
	if s, _ := reservationStatus(t, db, r.ID); s != model.ResConfirmed {
		t.Fatalf("前置失败：预约应已确认，得到 %s", s)
	}
	return r
}

// —— 提前取消：自动受理 + 全额退款 + 名额释放（R10） ————

func TestCancelEarlyAutoAccepted(t *testing.T) {
	db := testDB(t)
	r := confirmedReservation(t, db, 4*time.Hour, 1, 1) // 距开始 4h ≥ 2h

	res, err := CancelReservation(context.Background(), db, r.ID, 1, "", testBase.Add(time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatalf("提前取消应自动受理: %v", err)
	}
	if !res.AutoAccepted || res.RefundID == 0 {
		t.Fatalf("应返回自动受理与退款单: %+v", res)
	}
	if st, ct := reservationStatus(t, db, r.ID); st != model.ResCancelled || ct != model.CancelEarly {
		t.Fatalf("应为已取消/提前，得到 %s/%s", st, ct)
	}
	// 全额退款单已创建
	if st, _, _ := refundStatusOf(t, db, res.RefundID); st != model.RefundPending {
		t.Fatalf("退款应待处理，得到 %s", st)
	}
	// 名额已释放：同宠物可再次预约
	mustCreateReservation(t, db, r.SlotID, 1, 1, testBase.Add(2*time.Minute))
}

// —— 临近取消：必须填原因，进入审核（R11） ——————————

func TestCancelNearStartRequiresReasonAndReview(t *testing.T) {
	db := testDB(t)
	r := confirmedReservation(t, db, time.Hour, 1, 1) // 距开始 1h < 2h

	// 不填原因：拒绝
	_, err := CancelReservation(context.Background(), db, r.ID, 1, "  ", testBase.Add(time.Minute), 2*time.Hour)
	wantBizCode(t, err, CodeInvalidParam)

	// 填原因：进入审核
	res, err := CancelReservation(context.Background(), db, r.ID, 1, "宠物临时生病", testBase.Add(time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatalf("应进入审核: %v", err)
	}
	if res.AutoAccepted || res.ReviewID == 0 {
		t.Fatalf("应返回审核单: %+v", res)
	}
	if st, _ := reservationStatus(t, db, r.ID); st != model.ResPendingReview {
		t.Fatalf("预约应为取消审核中，得到 %s", st)
	}
	// 审核期间名额保留（R11）：同宠物再约应冲突
	_, err = CreateReservation(context.Background(), db, CreateReservationParams{SlotID: r.SlotID, UserID: 1, PetID: 1}, testBase.Add(2*time.Minute), 5*time.Minute)
	wantBizCode(t, err, CodePetConflict)
}

// —— 审核批准：取消 + 退款 + 名额释放（R12） ——————————

func TestReviewApprove(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := confirmedReservation(t, db, time.Hour, 1, 1)

	res, err := CancelReservation(ctx, db, r.ID, 1, "临时出差", testBase.Add(time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := ReviewCancel(ctx, db, res.ReviewID, 4, true, testBase.Add(10*time.Minute)); err != nil {
		t.Fatalf("客服批准应成功: %v", err)
	}
	if st, ct := reservationStatus(t, db, r.ID); st != model.ResCancelled || ct != model.CancelApproved {
		t.Fatalf("应为已取消/审核批准，得到 %s/%s", st, ct)
	}
	// 名额释放：同宠物可再约
	mustCreateReservation(t, db, r.SlotID, 1, 1, testBase.Add(15*time.Minute))
	// 审核单状态
	var reviewStatus string
	if err := db.QueryRow(`SELECT status FROM cancel_reviews WHERE id=?`, res.ReviewID).Scan(&reviewStatus); err != nil || reviewStatus != model.ReviewApproved {
		t.Fatalf("审核单应已批准，得到 %s err=%v", reviewStatus, err)
	}
}

// —— 审核拒绝：恢复已确认，名额不变（R12） ————————————

func TestReviewRejectRestoresConfirmed(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := confirmedReservation(t, db, time.Hour, 1, 1)

	res, err := CancelReservation(ctx, db, r.ID, 1, "不想去了", testBase.Add(time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := ReviewCancel(ctx, db, res.ReviewID, 4, false, testBase.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if st, _ := reservationStatus(t, db, r.ID); st != model.ResConfirmed {
		t.Fatalf("拒绝后应恢复已确认，得到 %s", st)
	}
	// 名额不变：同宠物再约仍冲突
	_, err = CreateReservation(ctx, db, CreateReservationParams{SlotID: r.SlotID, UserID: 1, PetID: 1}, testBase.Add(15*time.Minute), 5*time.Minute)
	wantBizCode(t, err, CodePetConflict)
	// 拒绝后用户可再次发起取消（重新走审核）
	_, err = CancelReservation(ctx, db, r.ID, 1, "再次申请", testBase.Add(20*time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatalf("拒绝后应可再次申请: %v", err)
	}
}

// —— 状态与权限校验（R15/权限） ————————————————————

func TestCancelWrongStateOrOwner(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// 待支付状态不能走已确认取消流程（应先主动取消）
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	pending := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	_, err := CancelReservation(ctx, db, pending.ID, 1, "", testBase, 2*time.Hour)
	wantBizCode(t, err, CodeInvalidState)

	// 非本人不能取消
	r := confirmedReservation(t, db, 4*time.Hour, 1, 1)
	_, err = CancelReservation(ctx, db, r.ID, 2, "冒名取消", testBase, 2*time.Hour)
	wantBizCode(t, err, CodePermission)
}

// —— 审核权限：角色与非本店员工（R20） ————————————————

func TestReviewPermission(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := confirmedReservation(t, db, time.Hour, 1, 1)
	res, err := CancelReservation(ctx, db, r.ID, 1, "宠物生病", testBase.Add(time.Minute), 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	// 非客服角色（前台）不能审核
	err = ReviewCancel(ctx, db, res.ReviewID, 2, true, testBase.Add(5*time.Minute))
	wantBizCode(t, err, CodePermission)

	// 非本店员工不能审核（预置第二家门店的客服）
	if _, err := db.Exec(`INSERT INTO stores(id, name) VALUES(2, '别家门店')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO employees(id, store_id, name, role) VALUES(9, 2, '别家客服', 'customerservice')`); err != nil {
		t.Fatal(err)
	}
	err = ReviewCancel(ctx, db, res.ReviewID, 9, true, testBase.Add(6*time.Minute))
	wantBizCode(t, err, CodePermission)

	// 本店客服处理成功
	if err := ReviewCancel(ctx, db, res.ReviewID, 4, true, testBase.Add(7*time.Minute)); err != nil {
		t.Fatal(err)
	}

	// 重复审核同一单：状态不符
	err = ReviewCancel(ctx, db, res.ReviewID, 4, false, testBase.Add(8*time.Minute))
	wantBizCode(t, err, CodeInvalidState)
}
