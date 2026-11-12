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

// —— 测试基础设施 ——————————————————————————————

var testBase = time.Date(2026, 11, 14, 6, 0, 0, 0, time.UTC) // "当前时间"基点

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("打开测试库: %v", err)
	}
	// Windows 上必须显式关闭连接，否则临时目录清理时文件仍被占用
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatalf("建表: %v", err)
	}
	seed(t, db)
	return db
}

func seed(t *testing.T, db *sql.DB) {
	t.Helper()
	stmts := []string{
		`INSERT INTO stores(id, name) VALUES(1, '宠物护理门店')`,
		`INSERT INTO services(id, store_id, name, price_cents, active) VALUES(1, 1, '洗护', 9900, 1)`,
		`INSERT INTO users(id, name) VALUES(1, '张三'), (2, '李四'), (3, '王五')`,
		`INSERT INTO pets(id, user_id, name, breed) VALUES(1, 1, '豆豆', '金毛'), (2, 1, '咪咪', '布偶'), (3, 2, '乐乐', '柯基')`,
		`INSERT INTO employees(id, store_id, name, role) VALUES(1, 1, '店长甲', 'manager'), (2, 1, '前台乙', 'front'), (3, 1, '服务员丙', 'staff'), (4, 1, '客服丁', 'customerservice')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("预置数据 %q: %v", s, err)
		}
	}
}

func fixedClock(t time.Time) Clock { return func() time.Time { return t } }

func mustCreateSlot(t *testing.T, db *sql.DB, startIn, dur time.Duration, capacity int) int64 {
	t.Helper()
	id, err := CreateSlot(context.Background(), db, CreateSlotParams{
		ActorID:   1,
		StoreID:   1,
		ServiceID: 1,
		StartAt:   testBase.Add(startIn),
		EndAt:     testBase.Add(startIn + dur),
		Capacity:  capacity,
	}, testBase)
	if err != nil {
		t.Fatalf("创建时段失败: %v", err)
	}
	return id
}

func mustCreateReservation(t *testing.T, db *sql.DB, slotID, userID, petID int64, now time.Time) *model.Reservation {
	t.Helper()
	r, err := CreateReservation(context.Background(), db, CreateReservationParams{SlotID: slotID, UserID: userID, PetID: petID}, now, 5*time.Minute)
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

// 读取预约当前持久化状态（测试断言用）。
func reservationStatus(t *testing.T, db *sql.DB, id int64) (string, string) {
	t.Helper()
	var status, cancelType sql.NullString
	if err := db.QueryRow(`SELECT status, cancel_type FROM reservations WHERE id=?`, id).Scan(&status, &cancelType); err != nil {
		t.Fatalf("查询预约 %d: %v", id, err)
	}
	if !status.Valid {
		t.Fatalf("预约 %d 无状态", id)
	}
	ct := ""
	if cancelType.Valid {
		ct = cancelType.String
	}
	return status.String, ct
}

// —— CreateSlot 校验（R1） ————————————————————————

func TestCreateSlotValidations(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	cases := []struct {
		name string
		p    CreateSlotParams
		code string
	}{
		{"结束不晚于开始", CreateSlotParams{ActorID: 1, StoreID: 1, ServiceID: 1, StartAt: testBase.Add(2 * time.Hour), EndAt: testBase.Add(2 * time.Hour), Capacity: 3}, CodeInvalidParam},
		{"开始时间在过去", CreateSlotParams{ActorID: 1, StoreID: 1, ServiceID: 1, StartAt: testBase.Add(-time.Hour), EndAt: testBase.Add(time.Hour), Capacity: 3}, CodeInvalidParam},
		{"容量超上限", CreateSlotParams{ActorID: 1, StoreID: 1, ServiceID: 1, StartAt: testBase.Add(2 * time.Hour), EndAt: testBase.Add(3 * time.Hour), Capacity: 4}, CodeInvalidParam},
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
		ActorID: 1, StoreID: 1, ServiceID: 1,
		StartAt: testBase.Add(3 * time.Hour), EndAt: testBase.Add(4 * time.Hour), Capacity: 3,
	}, testBase)
	if err != nil || id <= 0 {
		t.Fatalf("合法时段应创建成功: id=%d err=%v", id, err)
	}
}

// —— 可约列表名额统计（R3/R4） ————————————————————

func TestListAvailableSlotsRemaining(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)

	// 两个不同用户各占一个名额
	mustCreateReservation(t, db, slotID, 1, 1, testBase)
	mustCreateReservation(t, db, slotID, 2, 3, testBase)

	slots, err := ListAvailableSlots(ctx, db, 1, testBase)
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 1 {
		t.Fatalf("应返回 1 个时段，得到 %d", len(slots))
	}
	if slots[0].Remaining != 1 || slots[0].ActiveCount != 2 {
		t.Fatalf("应剩 1 位（占用 2），得到 剩 %d 占 %d", slots[0].Remaining, slots[0].ActiveCount)
	}
}

// 已开始的时段不出现在可约列表（R3）。
func TestListAvailableSlotsExcludesStarted(t *testing.T) {
	db := testDB(t)
	// 直接插入一个已过去的时段（绕过 CreateSlot 的未来校验）
	_, err := db.Exec(`INSERT INTO slots(id, store_id, service_id, start_at, end_at, capacity) VALUES(9, 1, 1, ?, ?, 3)`,
		testBase.Add(-2*time.Hour).UTC().Format(time.RFC3339), testBase.Add(-time.Hour).UTC().Format(time.RFC3339))
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
