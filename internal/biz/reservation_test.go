package biz

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"paw-book/internal/model"
)

// —— R1.2/R2.1 创建预约正常流程 ————————————————

func TestCreateReservationOK(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)

	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	if r.Status != model.ResPendingPayment {
		t.Fatalf("初始状态应为待支付，得到 %s", r.Status)
	}
	if r.PriceCents != 9900 {
		t.Fatalf("预约款应为 9900 分，得到 %d", r.PriceCents)
	}
	if !r.PayExpireAt.Equal(testBase.Add(5 * time.Minute)) {
		t.Fatalf("占位截止应为 5 分钟后，得到 %v", r.PayExpireAt)
	}
	if got := reservedCount(t, db, slotID); got != 1 {
		t.Fatalf("名额账应 +1，得到 %d", got)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM operation_log WHERE action='RESERVATION_CREATE'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("应记录 1 条审计，得到 %d err=%v", n, err)
	}
}

// —— R1.2/P1 越权：替他人宠物预约 ————————————————

func TestCreateReservationNotPetOwner(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)

	_, err := CreateReservation(context.Background(), db,
		CreateReservationParams{SlotID: slotID, UserID: 1, PetID: 3}, testBase, 5*time.Minute)
	wantBizCode(t, err, CodePetNotOwned)

	_, err = CreateReservation(context.Background(), db,
		CreateReservationParams{SlotID: slotID, UserID: 1, PetID: 99}, testBase, 5*time.Minute)
	wantBizCode(t, err, CodePetNotFound)
}

// —— R1.2 同宠物同时段唯一 ———————————————————————

func TestCreateReservationPetConflict(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)

	mustCreateReservation(t, db, slotID, 1, 1, testBase)
	_, err := CreateReservation(context.Background(), db,
		CreateReservationParams{SlotID: slotID, UserID: 1, PetID: 1}, testBase, 5*time.Minute)
	wantBizCode(t, err, CodeDuplicateActive)

	// 同一用户的另一只宠物可以约
	mustCreateReservation(t, db, slotID, 1, 2, testBase)
	// 其他用户的宠物也可以约
	mustCreateReservation(t, db, slotID, 2, 3, testBase)
}

// —— R1.3 名额已满 ————————————————————————————

func TestCreateReservationSlotFull(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)

	if _, err := db.Exec(`INSERT INTO pet(id, user_id, name, breed) VALUES(4, 3, '旺财', '柴犬')`); err != nil {
		t.Fatal(err)
	}
	mustCreateReservation(t, db, slotID, 1, 1, testBase)
	mustCreateReservation(t, db, slotID, 1, 2, testBase)
	mustCreateReservation(t, db, slotID, 2, 3, testBase)

	_, err := CreateReservation(ctx, db, CreateReservationParams{SlotID: slotID, UserID: 3, PetID: 4}, testBase, 5*time.Minute)
	wantBizCode(t, err, CodeSlotFull)
	if got := reservedCount(t, db, slotID); got != 3 {
		t.Fatalf("名额账应保持 3，得到 %d", got)
	}
}

// —— R1.4 时段已开始不可预约 ————————————————————

func TestCreateReservationSlotStarted(t *testing.T) {
	db := testDB(t)
	_, err := db.Exec(`INSERT INTO slot(id, store_id, item_id, start_at, end_at, capacity, reserved_count, created_by, created_at)
		VALUES(7, 1, 1, ?, ?, 3, 0, 1, ?)`,
		testBase.Add(-30*time.Minute).UTC().Format(time.RFC3339),
		testBase.Add(30*time.Minute).UTC().Format(time.RFC3339),
		testBase.UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	_, err = CreateReservation(context.Background(), db,
		CreateReservationParams{SlotID: 7, UserID: 1, PetID: 1}, testBase, 5*time.Minute)
	wantBizCode(t, err, CodeSlotStarted)

	// 时段不存在
	_, err = CreateReservation(context.Background(), db,
		CreateReservationParams{SlotID: 999, UserID: 1, PetID: 1}, testBase, 5*time.Minute)
	wantBizCode(t, err, CodeSlotNotFound)
}

// —— R2.2 过期占位懒清理并释放名额 ————————————————

func TestCreateReservationExpiredPendingReleasesSlot(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 4*time.Hour, time.Hour, 3)

	r1 := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	later := testBase.Add(6 * time.Minute)
	r2 := mustCreateReservation(t, db, slotID, 1, 1, later)
	if r2.ID == r1.ID {
		t.Fatal("应生成新预约")
	}
	if status, ct := reservationStatus(t, db, r1.ID); status != model.ResExpired || ct != model.CancelExpired {
		t.Fatalf("旧预约应已超时取消，得到 %s/%s", status, ct)
	}
	if got := reservedCount(t, db, slotID); got != 1 {
		t.Fatalf("懒清理后名额账应为 1，得到 %d", got)
	}
}

// —— R1.3/R6.1 并发抢位：50 抢 3 恰好 3 成功 ————————

func TestConcurrentCreateNoOversell(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)

	for i := 100; i < 150; i++ {
		uid := int64(i%3 + 1)
		if _, err := db.Exec(`INSERT INTO pet(id, user_id, name, breed) VALUES(?, ?, ?, '串串')`,
			i, uid, fmt.Sprintf("宠物%d", i)); err != nil {
			t.Fatal(err)
		}
	}

	var success atomic.Int32
	var wg sync.WaitGroup
	for i := 100; i < 150; i++ {
		wg.Add(1)
		go func(petID int64) {
			defer wg.Done()
			_, err := CreateReservation(context.Background(), db,
				CreateReservationParams{SlotID: slotID, UserID: petID%3 + 1, PetID: petID}, testBase, 5*time.Minute)
			if err == nil {
				success.Add(1)
			}
		}(int64(i))
	}
	wg.Wait()

	if got := success.Load(); got != 3 {
		t.Fatalf("并发抢 3 个名额，成功数应恰为 3，得到 %d", got)
	}
	if n := activeCount(t, db, slotID, testBase); n != 3 {
		t.Fatalf("数据库有效预约应恰为 3，得到 %d", n)
	}
	if n := reservedCount(t, db, slotID); n != 3 {
		t.Fatalf("名额账 reserved_count 应为 3，得到 %d", n)
	}
}

// —— R2.3 用户主动取消待支付预约 ————————————————

func TestCancelPendingReservation(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 1)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)

	if err := CancelUnpaid(ctx, db, r.ID, 2, testBase); err == nil {
		t.Fatal("非本人取消应失败")
	} else {
		wantBizCode(t, err, CodeNotFound)
	}
	if err := CancelUnpaid(ctx, db, r.ID, 1, testBase.Add(time.Minute)); err != nil {
		t.Fatalf("本人取消待支付应成功: %v", err)
	}
	if status, ct := reservationStatus(t, db, r.ID); status != model.ResCancelled || ct != model.CancelUserUnpaid {
		t.Fatalf("取消后应为 CANCELLED/USER_UNPAID，得到 %s/%s", status, ct)
	}
	if n := reservedCount(t, db, slotID); n != 0 {
		t.Fatalf("名额应释放为 0，得到 %d", n)
	}
	// 退款单不应产生（R2.3）
	var refunds int
	if err := db.QueryRow(`SELECT COUNT(*) FROM refund WHERE reservation_id=?`, r.ID).Scan(&refunds); err != nil || refunds != 0 {
		t.Fatalf("未付款取消不得产生退款，得到 %d err=%v", refunds, err)
	}
	// 名额已释放：其他宠物可约
	mustCreateReservation(t, db, slotID, 2, 3, testBase.Add(2*time.Minute))
	// 重复取消：状态不符
	wantBizCode(t, CancelUnpaid(ctx, db, r.ID, 1, testBase.Add(3*time.Minute)), CodeStateInvalid)
	// 不存在的预约
	wantBizCode(t, CancelUnpaid(ctx, db, 999, 1, testBase), CodeNotFound)
}

// —— R2.2 占位截止边界：恰在截止时刻视为过期 ————————————

func TestPayDeadlineBoundary(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 4*time.Hour, time.Hour, 3)
	r1 := mustCreateReservation(t, db, slotID, 1, 1, testBase)

	// 06:04:59 未到期：同宠物再约冲突
	_, err := CreateReservation(context.Background(), db,
		CreateReservationParams{SlotID: slotID, UserID: 1, PetID: 1},
		testBase.Add(5*time.Minute-time.Second), 5*time.Minute)
	wantBizCode(t, err, CodeDuplicateActive)

	// 06:05:00 恰到期：旧单被清理，同宠物可再约
	r2, err := CreateReservation(context.Background(), db,
		CreateReservationParams{SlotID: slotID, UserID: 1, PetID: 1}, testBase.Add(5*time.Minute), 5*time.Minute)
	if err != nil {
		t.Fatalf("到期后同宠物应可重新预约: %v", err)
	}
	if r2.ID == r1.ID {
		t.Fatal("应生成新预约")
	}
	if status, _ := reservationStatus(t, db, r1.ID); status != model.ResExpired {
		t.Fatalf("到期预约应被清理为过期，得到 %s", status)
	}
}

// —— 预约详情与列表（A7/A8） ————————————————————

func TestGetReservationAndList(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)

	d, err := GetReservation(ctx, db, r.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if d.PetName != "豆豆" || d.UserName != "张三" || d.SlotStart == "" {
		t.Fatalf("详情联表字段不完整: %+v", d)
	}
	// P2：他人不可见
	wantBizCode(t, func() error { _, e := GetReservation(ctx, db, r.ID, 2); return e }(), CodeNotFound)

	list, err := ListMyReservations(ctx, db, 1, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("应返回 1 条我的预约，得到 %d err=%v", len(list), err)
	}
}
