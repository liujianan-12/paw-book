package biz

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"paw-book/internal/model"
	"paw-book/internal/store"
)

// —— 测试基础设施（D8：SQLite 临时库，不用 Docker/MySQL） ——————————

var testBase = time.Date(2026, 11, 14, 6, 0, 0, 0, time.UTC) // 教学时钟基点

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("打开测试库: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() }) // Windows 上必须关闭，否则临时目录清理失败
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatalf("建表: %v", err)
	}
	seed(t, db)
	return db
}

func seed(t *testing.T, db *sql.DB) {
	t.Helper()
	// 预置数据与 migrations/002_seed.sql 一致（E6：1 门店/1 项目/角色齐全）
	// 002_seed.sql 已预置 store1/staff1-4/item1/user1-3/pet1-3，这里只补测试所需数据
	stmts := []string{
		`INSERT OR IGNORE INTO store(id, name) VALUES(1, '宠物护理门店'), (2, '别家门店')`,
		`INSERT OR IGNORE INTO service_item(id, store_id, name, price_cents, active) VALUES(1, 1, '洗护', 9900, 1)`,
		`INSERT INTO staff(id, store_id, name, role, account, password_hash) VALUES
			(5, 1, '前台周', 'reception', 'rec5', 'x'),
			(6, 1, '服务吴', 'server', 'srv6', 'x'),
			(9, 2, '别家客服', 'cs', 'cs9', 'x'),
			(8, 2, '别家店长', 'manager', 'mgr9', 'x'),
			(10, 2, '别家服务', 'server', 'srv10', 'x')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("预置数据 %q: %v", s, err)
		}
	}
}

func mustCreateSlot(t *testing.T, db *sql.DB, startIn, dur time.Duration, capacity int) int64 {
	t.Helper()
	id, err := CreateSlot(context.Background(), db, CreateSlotParams{
		ActorID: 1, StoreID: 1, ItemID: 1,
		StartAt: testBase.Add(startIn), EndAt: testBase.Add(startIn + dur), Capacity: capacity,
	}, testBase)
	if err != nil {
		t.Fatalf("创建时段失败: %v", err)
	}
	return id
}

func mustCreateReservation(t *testing.T, db *sql.DB, slotID, userID, petID int64, now time.Time) *model.Reservation {
	t.Helper()
	r, err := CreateReservation(context.Background(), db,
		CreateReservationParams{SlotID: slotID, UserID: userID, PetID: petID}, now, 5*time.Minute)
	if err != nil {
		t.Fatalf("创建预约失败: %v", err)
	}
	return r
}

func wantBizCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("期望错误码 %s，但操作成功了", code)
	}
	var be *BizError
	if !errors.As(err, &be) {
		t.Fatalf("应返回 BizError，得到 %T: %v", err, err)
	}
	if be.Code != code {
		t.Fatalf("错误码应为 %s，得到 %s（%s）", code, be.Code, be.Msg)
	}
}

// reservationStatus 读取预约状态与取消类型（测试断言用）。
func reservationStatus(t *testing.T, db *sql.DB, id int64) (string, string) {
	t.Helper()
	var status string
	var cancelType sql.NullString
	if err := db.QueryRow(`SELECT status, cancel_type FROM reservation WHERE id=?`, id).Scan(&status, &cancelType); err != nil {
		t.Fatalf("查询预约 %d: %v", id, err)
	}
	if cancelType.Valid {
		return status, cancelType.String
	}
	return status, ""
}

// activeCount 按 I1 口径统计时段占用数（待支付未过期/已确认/审核中/已到店/服务中/已完成）。
func activeCount(t *testing.T, db *sql.DB, slotID int64, now time.Time) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM reservation
		WHERE slot_id=? AND (
		  status IN ('CONFIRMED','CANCELLATION_REVIEW','CHECKED_IN','IN_SERVICE','COMPLETED')
		  OR (status='PENDING_PAYMENT' AND pay_expire_at>?)
		)`, slotID, fmtTime(now)).Scan(&n); err != nil {
		t.Fatalf("统计有效预约: %v", err)
	}
	return n
}

func reservedCount(t *testing.T, db *sql.DB, slotID int64) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT reserved_count FROM slot WHERE id=?`, slotID).Scan(&n); err != nil {
		t.Fatalf("查询名额账: %v", err)
	}
	return n
}

// —— R1.1 时段创建校验 + 权限 ————————————————————

func TestCreateSlotValidations(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	cases := []struct {
		name string
		p    CreateSlotParams
		code string
	}{
		{"结束不晚于开始", CreateSlotParams{ActorID: 1, StoreID: 1, ItemID: 1, StartAt: testBase.Add(2 * time.Hour), EndAt: testBase.Add(2 * time.Hour), Capacity: 3}, CodeInvalidParam},
		{"开始时间在过去", CreateSlotParams{ActorID: 1, StoreID: 1, ItemID: 1, StartAt: testBase.Add(-time.Hour), EndAt: testBase.Add(time.Hour), Capacity: 3}, CodeInvalidParam},
		{"容量超上限", CreateSlotParams{ActorID: 1, StoreID: 1, ItemID: 1, StartAt: testBase.Add(2 * time.Hour), EndAt: testBase.Add(3 * time.Hour), Capacity: 4}, CodeInvalidParam},
		{"容量为零", CreateSlotParams{ActorID: 1, StoreID: 1, ItemID: 1, StartAt: testBase.Add(2 * time.Hour), EndAt: testBase.Add(3 * time.Hour), Capacity: 0}, CodeInvalidParam},
		{"非店长创建", CreateSlotParams{ActorID: 2, StoreID: 1, ItemID: 1, StartAt: testBase.Add(2 * time.Hour), EndAt: testBase.Add(3 * time.Hour), Capacity: 3}, CodeRoleForbidden},
		{"店长跨店创建", CreateSlotParams{ActorID: 8, StoreID: 1, ItemID: 1, StartAt: testBase.Add(2 * time.Hour), EndAt: testBase.Add(3 * time.Hour), Capacity: 3}, CodeStoreMismatch},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := CreateSlot(ctx, db, c.p, testBase)
			wantBizCode(t, err, c.code)
		})
	}
}

func TestCreateSlotOK(t *testing.T) {
	db := testDB(t)
	id, err := CreateSlot(context.Background(), db, CreateSlotParams{
		ActorID: 1, StoreID: 1, ItemID: 1,
		StartAt: testBase.Add(3 * time.Hour), EndAt: testBase.Add(4 * time.Hour), Capacity: 3,
	}, testBase)
	if err != nil || id <= 0 {
		t.Fatalf("合法时段应创建成功: id=%d err=%v", id, err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM operation_log WHERE action='SLOT_CREATE'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("应记录 1 条审计，得到 %d err=%v", n, err)
	}
}

// —— R1.3/R2.1 可约列表名额统计 ——————————————————

func TestListAvailableSlotsRemaining(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)

	mustCreateReservation(t, db, slotID, 1, 1, testBase)
	mustCreateReservation(t, db, slotID, 2, 3, testBase)

	slots, err := ListAvailableSlots(ctx, db, 1, testBase)
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 1 {
		t.Fatalf("应返回 1 个时段，得到 %d", len(slots))
	}
	if slots[0].Remaining != 1 || slots[0].ReservedCount != 2 {
		t.Fatalf("应剩 1 位（占用 2），得到 剩 %d 占 %d", slots[0].Remaining, slots[0].ReservedCount)
	}
}

func TestListAvailableSlotsExcludesStarted(t *testing.T) {
	db := testDB(t)
	// 直接插入已开始时段（绕过 CreateSlot 的未来校验）
	_, err := db.Exec(`INSERT INTO slot(id, store_id, item_id, start_at, end_at, capacity, reserved_count, created_by, created_at)
		VALUES(9, 1, 1, ?, ?, 3, 0, 1, ?)`,
		testBase.Add(-2*time.Hour).UTC().Format(time.RFC3339),
		testBase.Add(-time.Hour).UTC().Format(time.RFC3339),
		testBase.UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)

	slots, err := ListAvailableSlots(context.Background(), db, 1, testBase)
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 1 || slots[0].ID == 9 {
		t.Fatalf("已开始时段不应出现，得到 %d 个时段", len(slots))
	}
}
