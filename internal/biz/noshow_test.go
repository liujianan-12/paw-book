package biz

import (
	"context"
	"testing"
	"time"

	"paw-book/internal/model"
)

func TestMarkNoShowOK(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := confirmedReservation(t, db, 2*time.Hour, 1, 1) // 时段 08:00-09:00

	// 时段结束后（10:00）店长标记
	if err := MarkNoShow(ctx, db, r.ID, 1, "未到店且电话未接通", testBase.Add(4*time.Hour)); err != nil {
		t.Fatalf("时段结束后应可标记未到店: %v", err)
	}
	if s, _ := reservationStatus(t, db, r.ID); s != model.ResNoShow {
		t.Fatalf("应为未到店，得到 %s", s)
	}
	// 原因已持久化，用户可查看（R22）
	var reason string
	if err := db.QueryRow(`SELECT no_show_reason FROM reservations WHERE id=?`, r.ID).Scan(&reason); err != nil || reason != "未到店且电话未接通" {
		t.Fatalf("未到店原因应为填写值，得到 %q err=%v", reason, err)
	}
	// 本题约定：不自动退款（R22）
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM refunds WHERE reservation_id=?`, r.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("未到店不应自动退款，退款单数 %d err=%v", n, err)
	}
	// 终态不可逆：不能再次标记
	if err := MarkNoShow(ctx, db, r.ID, 1, "再标一次", testBase.Add(5*time.Hour)); err == nil {
		t.Fatal("重复标记应失败")
	}
}

func TestMarkNoShowValidations(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := confirmedReservation(t, db, 2*time.Hour, 1, 1)

	// 未填写原因
	err := MarkNoShow(ctx, db, r.ID, 1, "   ", testBase.Add(4*time.Hour))
	wantBizCode(t, err, CodeInvalidParam)

	// 时段尚未结束
	err = MarkNoShow(ctx, db, r.ID, 1, "还没到时间", testBase.Add(time.Hour))
	wantBizCode(t, err, CodeInvalidState)

	// 非店长角色
	for _, emp := range []int64{2, 3, 4} {
		err = MarkNoShow(ctx, db, r.ID, emp, "越权标记", testBase.Add(4*time.Hour))
		wantBizCode(t, err, CodePermission)
	}

	// 非本店店长
	if _, err := db.Exec(`INSERT INTO stores(id, name) VALUES(2, '别家门店')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO employees(id, store_id, name, role) VALUES(9, 2, '别家店长', 'manager')`); err != nil {
		t.Fatal(err)
	}
	err = MarkNoShow(ctx, db, r.ID, 9, "别家店长标记", testBase.Add(4*time.Hour))
	wantBizCode(t, err, CodePermission)

	// 已完成的预约不能标记未到店
	done := confirmedReservation(t, db, 2*time.Hour, 1, 1)
	if err := CheckIn(ctx, db, done.ID, 2, testBase); err != nil {
		t.Fatal(err)
	}
	if err := StartService(ctx, db, done.ID, 3, testBase.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := CompleteService(ctx, db, done.ID, 3, testBase.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	err = MarkNoShow(ctx, db, done.ID, 1, "已完成还想标未到店", testBase.Add(4*time.Hour))
	wantBizCode(t, err, CodeInvalidState)
}
