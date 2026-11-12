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

// —— 创建预约正常流程（R2/R4） ————————————————————

func TestCreateReservationOK(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)

	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	if r.Status != model.ResPendingPayment {
		t.Fatalf("初始状态应为待支付，得到 %s", r.Status)
	}
	if r.AmountCents != 9900 {
		t.Fatalf("预约款应为 9900 分，得到 %d", r.AmountCents)
	}
	if !r.PayDeadline.Equal(testBase.Add(5 * time.Minute)) {
		t.Fatalf("占位截止应为 5 分钟后，得到 %v", r.PayDeadline)
	}
	// 审计日志已记录
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action='RESERVATION_CREATE'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("应记录 1 条审计，得到 %d err=%v", n, err)
	}
}

// —— 越权：替他人宠物预约（R2/权限） ——————————————

func TestCreateReservationNotPetOwner(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)

	// 宠物 3 属于用户 2，用户 1 试图用它预约
	_, err := CreateReservation(context.Background(), db, CreateReservationParams{SlotID: slotID, UserID: 1, PetID: 3}, testBase, 5*time.Minute)
	wantBizCode(t, err, CodeNotPetOwner)

	// 宠物不存在
	_, err = CreateReservation(context.Background(), db, CreateReservationParams{SlotID: slotID, UserID: 1, PetID: 99}, testBase, 5*time.Minute)
	wantBizCode(t, err, CodeNotPetOwner)
}

// —— 同宠物同时段唯一（R2） ———————————————————————

func TestCreateReservationPetConflict(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)

	mustCreateReservation(t, db, slotID, 1, 1, testBase)
	_, err := CreateReservation(context.Background(), db, CreateReservationParams{SlotID: slotID, UserID: 1, PetID: 1}, testBase, 5*time.Minute)
	wantBizCode(t, err, CodePetConflict)

	// 同一用户的另一只宠物可以约
	mustCreateReservation(t, db, slotID, 1, 2, testBase)
	// 其他用户的宠物也可以约
	mustCreateReservation(t, db, slotID, 2, 3, testBase)
}

// —— 名额已满（R3/R5） ——————————————————————————

func TestCreateReservationSlotFull(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)

	mustCreateReservation(t, db, slotID, 1, 1, testBase)
	mustCreateReservation(t, db, slotID, 1, 2, testBase)
	mustCreateReservation(t, db, slotID, 2, 3, testBase)

	_, err := CreateReservation(context.Background(), db, CreateReservationParams{SlotID: slotID, UserID: 3, PetID: 4}, testBase, 5*time.Minute)
	// 宠物 4 不存在，先准备它
	wantBizCode(t, err, CodeNotPetOwner)

	if _, err := db.Exec(`INSERT INTO pets(id, user_id, name, breed) VALUES(4, 3, '旺财', '柴犬')`); err != nil {
		t.Fatal(err)
	}
	_, err = CreateReservation(context.Background(), db, CreateReservationParams{SlotID: slotID, UserID: 3, PetID: 4}, testBase, 5*time.Minute)
	wantBizCode(t, err, CodeSlotFull)
}

// —— 时段已开始不可预约（R3） ————————————————————

func TestCreateReservationSlotStarted(t *testing.T) {
	db := testDB(t)
	// 插入一个已开始时段（绕过 CreateSlot 未来校验）
	_, err := db.Exec(`INSERT INTO slots(id, store_id, service_id, start_at, end_at, capacity) VALUES(7, 1, 1, ?, ?, 3)`,
		testBase.Add(-30*time.Minute).UTC().Format(time.RFC3339), testBase.Add(30*time.Minute).UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	_, err = CreateReservation(context.Background(), db, CreateReservationParams{SlotID: 7, UserID: 1, PetID: 1}, testBase, 5*time.Minute)
	wantBizCode(t, err, CodeSlotStarted)
}

// —— 过期待支付：懒取消并释放名额（R4/R13/I4） ————————

func TestCreateReservationExpiredPendingReleasesSlot(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 4*time.Hour, time.Hour, 3)

	// 用户 1 宠物 1 预约但未付款
	r1 := mustCreateReservation(t, db, slotID, 1, 1, testBase)

	// 6 分钟后（超过 5 分钟占位），同一宠物再来预约
	later := testBase.Add(6 * time.Minute)
	r2 := mustCreateReservation(t, db, slotID, 1, 1, later)

	if r2.ID == r1.ID {
		t.Fatal("应生成新预约")
	}
	// 旧预约被懒清理为超时取消
	if status, ct := reservationStatus(t, db, r1.ID); status != model.ResCancelled || ct != model.CancelTimeout {
		t.Fatalf("旧预约应已超时取消，得到 %s/%s", status, ct)
	}
	// 名额占用应为 1（新预约），可约列表剩 2
	slots, err := ListAvailableSlots(context.Background(), db, 1, later)
	if err != nil {
		t.Fatal(err)
	}
	if slots[0].ActiveCount != 1 || slots[0].Remaining != 2 {
		t.Fatalf("懒清理后应剩 2 位，得到 剩 %d 占 %d", slots[0].Remaining, slots[0].ActiveCount)
	}
}

// —— 并发抢位：50 抢 3 恰好 3 成功（R5/I1） ————————————

func TestConcurrentCreateNoOversell(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)

	// 预置 50 只宠物（分属 3 个用户）
	for i := 100; i < 150; i++ {
		uid := int64(i%3 + 1)
		if _, err := db.Exec(`INSERT INTO pets(id, user_id, name, breed) VALUES(?, ?, ?, '串串')`, i, uid, fmt.Sprintf("宠物%d", i)); err != nil {
			t.Fatal(err)
		}
	}

	var success atomic.Int32
	var wg sync.WaitGroup
	for i := 100; i < 150; i++ {
		wg.Add(1)
		go func(petID int64) {
			defer wg.Done()
			_, err := CreateReservation(context.Background(), db, CreateReservationParams{SlotID: slotID, UserID: petID%3 + 1, PetID: petID}, testBase, 5*time.Minute)
			if err == nil {
				success.Add(1)
			}
		}(int64(i))
	}
	wg.Wait()

	if got := success.Load(); got != 3 {
		t.Fatalf("并发抢 3 个名额，成功数应恰为 3，得到 %d", got)
	}
	// 数据库口径复核：有效预约恰好 3 条
	n, err := countActive(context.Background(), db, slotID, testBase)
	if err != nil || n != 3 {
		t.Fatalf("数据库有效预约应恰为 3，得到 %d err=%v", n, err)
	}
}

// —— 用户主动取消待支付预约（R4） ——————————————————

func TestCancelPendingReservation(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 1)

	r := mustCreateReservation(t, db, slotID, 1, 1, testBase)

	// 非本人不能取消
	if err := CancelPendingReservation(ctx, db, r.ID, 2, testBase); err == nil {
		t.Fatal("非本人取消应失败")
	}
	// 本人取消成功
	if err := CancelPendingReservation(ctx, db, r.ID, 1, testBase.Add(time.Minute)); err != nil {
		t.Fatalf("本人取消待支付应成功: %v", err)
	}
	if status, ct := reservationStatus(t, db, r.ID); status != model.ResCancelled || ct != model.CancelUser {
		t.Fatalf("取消后应为 CANCELLED/USER，得到 %s/%s", status, ct)
	}

	// 名额已释放：其他宠物可约
	r2 := mustCreateReservation(t, db, slotID, 2, 3, testBase.Add(2*time.Minute))
	if r2.ID == r.ID {
		t.Fatal("应能再次预约释放的名额")
	}

	// 重复取消同一预约：状态不符
	if err := CancelPendingReservation(ctx, db, r.ID, 1, testBase.Add(3*time.Minute)); err == nil {
		t.Fatal("重复取消应失败（名额只能释放一次）")
	}
	// 不存在的预约
	if err := CancelPendingReservation(ctx, db, 999, 1, testBase); err == nil {
		t.Fatal("取消不存在的预约应失败")
	}
}

// —— 占位截止边界：恰在截止时刻仍视为过期 ————————————

func TestPayDeadlineBoundary(t *testing.T) {
	db := testDB(t)
	slotID := mustCreateSlot(t, db, 4*time.Hour, time.Hour, 3)

	// 06:00 创建，占位截止 06:05
	r1 := mustCreateReservation(t, db, slotID, 1, 1, testBase)

	// 06:04:59（未到期）：同宠物再约应冲突——占位仍有效
	_, err := CreateReservation(context.Background(), db, CreateReservationParams{SlotID: slotID, UserID: 1, PetID: 1}, testBase.Add(5*time.Minute-time.Second), 5*time.Minute)
	wantBizCode(t, err, CodePetConflict)

	// 06:05:00（恰到期）：同宠物可再约，旧单被懒清理取消
	r2, err := CreateReservation(context.Background(), db, CreateReservationParams{SlotID: slotID, UserID: 1, PetID: 1}, testBase.Add(5*time.Minute), 5*time.Minute)
	if err != nil {
		t.Fatalf("到期后同宠物应可重新预约: %v", err)
	}
	if r2.ID == r1.ID {
		t.Fatal("应生成新预约")
	}
	if status, ct := reservationStatus(t, db, r1.ID); status != model.ResCancelled || ct != model.CancelTimeout {
		t.Fatalf("到期预约应被懒清理为超时取消，得到 %s/%s", status, ct)
	}
}
