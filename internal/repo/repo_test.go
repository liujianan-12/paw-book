package repo

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"paw-book/internal/domain"
	"paw-book/internal/store"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "repo.db"))
	if err != nil {
		t.Fatalf("打开测试库: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatalf("建表: %v", err)
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO store(id, name) VALUES(1, '门店')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO staff(id, store_id, name, role, account, password_hash) VALUES
		(1, 1, '店长', 'manager', 'mgr', 'x'), (2, 1, '客服', 'cs', 'cs1', 'x')`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestTimeHelpers(t *testing.T) {
	now := time.Date(2026, 11, 14, 6, 0, 0, 0, time.UTC)
	s := FmtTime(now)
	if s != "2026-11-14T06:00:00Z" {
		t.Fatalf("时间格式不符: %s", s)
	}
	got, err := ParseTime(s)
	if err != nil || !got.Equal(now) {
		t.Fatalf("解析失败: %v %v", got, err)
	}
	if _, err := ParseTime("not-a-time"); err == nil {
		t.Fatal("非法时间应返回错误")
	}
	if RealClock()().IsZero() {
		t.Fatal("RealClock 不应为零值")
	}
}

func TestCheckStaff(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	storeID, err := CheckStaff(ctx, db, 1, domain.RoleManager)
	if err != nil || storeID != 1 {
		t.Fatalf("店长校验失败: %d %v", storeID, err)
	}
	_, err = CheckStaff(ctx, db, 1, domain.RoleCS)
	var be *domain.BizError
	if !errors.As(err, &be) || be.Code != domain.CodeRoleForbidden {
		t.Fatalf("角色不符应返回 ROLE_FORBIDDEN: %v", err)
	}
	_, err = CheckStaff(ctx, db, 999, domain.RoleManager)
	if !errors.As(err, &be) || be.Code != domain.CodeNotFound {
		t.Fatalf("员工不存在应返回 NOT_FOUND: %v", err)
	}
}

func TestBeginTxUnsupported(t *testing.T) {
	type noTx struct{ DBTX }
	_, err := BeginTx(context.Background(), noTx{})
	var be *domain.BizError
	if !errors.As(err, &be) || be.Code != domain.CodeInvalidParam {
		t.Fatalf("不支持事务应返回 INVALID_PARAM: %v", err)
	}
}

func TestBeginTxAndAudit(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	tx, err := BeginTx(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 14, 6, 0, 0, 0, time.UTC)
	if err := Audit(ctx, tx, 1, domain.RoleManager, 1, "TEST_ACTION", "reservation", 7, "ok", "detail", now); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var action, targetID string
	if err := db.QueryRow(`SELECT action, target_id FROM operation_log WHERE action='TEST_ACTION'`).
		Scan(&action, &targetID); err != nil || targetID != "7" {
		t.Fatalf("审计记录不符: %s/%s err=%v", action, targetID, err)
	}
}

func TestIsConstraintViolation(t *testing.T) {
	cases := []struct {
		err   error
		table string
		cols  []string
		want  bool
	}{
		{nil, "reservation", []string{"slot_id"}, false},
		{errors.New("UNIQUE constraint failed: reservation.slot_id, reservation.pet_id"), "reservation", []string{"slot_id", "pet_id"}, true},
		{errors.New("UNIQUE constraint failed: reservation.slot_id, reservation.pet_id"), "payment", []string{"slot_id"}, false},
		{errors.New("UNIQUE constraint failed: reservation.slot_id, reservation.pet_id"), "reservation", []string{"order_no"}, false},
		{errors.New("foreign key constraint failed"), "reservation", nil, false},
	}
	for i, c := range cases {
		if got := IsConstraintViolation(c.err, c.table, c.cols...); got != c.want {
			t.Errorf("case %d: want %v got %v", i, c.want, got)
		}
	}
}
