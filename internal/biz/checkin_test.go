package biz

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"paw-book/internal/model"
)

func recordCount(t *testing.T, db *sql.DB, table string, reservationID int64) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE reservation_id=?`, reservationID).Scan(&n); err != nil {
		t.Fatalf("统计 %s: %v", table, err)
	}
	return n
}

// —— R5.1/R5.3 核销：状态、越权与幂等 ————————————————

func TestCheckInRulesAndIdempotency(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// 未付款不可核销
	slotID := mustCreateSlot(t, db, 2*time.Hour, time.Hour, 3)
	pending := mustCreateReservation(t, db, slotID, 1, 1, testBase)
	_, err := CheckIn(ctx, db, pending.ID, 2, testBase.Add(time.Minute))
	wantBizCode(t, err, CodeCheckinNotAllow)

	// 已确认可核销；重复核销幂等（同员工 / 不同员工）
	r := confirmedReservation(t, db, 2*time.Hour, 2, 3)
	already, err := CheckIn(ctx, db, r.ID, 2, testBase.Add(2*time.Minute))
	if err != nil || already {
		t.Fatalf("首次核销应成功且 alreadyDone=false: already=%v err=%v", already, err)
	}
	if st, _ := reservationStatus(t, db, r.ID); st != model.ResCheckedIn {
		t.Fatalf("应为已到店，得到 %s", st)
	}
	if n := recordCount(t, db, "checkin_record", r.ID); n != 1 {
		t.Fatalf("核销记录应恰有 1 条，得到 %d", n)
	}
	already, err = CheckIn(ctx, db, r.ID, 2, testBase.Add(3*time.Minute))
	if err != nil || !already {
		t.Fatalf("同员工重复核销应幂等成功: already=%v err=%v", already, err)
	}
	already, err = CheckIn(ctx, db, r.ID, 5, testBase.Add(4*time.Minute))
	if err != nil || !already {
		t.Fatalf("不同员工重复核销应幂等成功: already=%v err=%v", already, err)
	}
	if n := recordCount(t, db, "checkin_record", r.ID); n != 1 {
		t.Fatalf("重复核销不得新增记录，得到 %d", n)
	}

	// 角色/门店校验
	_, err = CheckIn(ctx, db, r.ID, 3, testBase.Add(5*time.Minute)) // 服务人员
	wantBizCode(t, err, CodeRoleForbidden)
	_, err = CheckIn(ctx, db, r.ID, 10, testBase.Add(5*time.Minute)) // 别店服务（角色先不符）
	wantBizCode(t, err, CodeRoleForbidden)
	_, err = CheckIn(ctx, db, 9999, 2, testBase.Add(5*time.Minute))
	wantBizCode(t, err, CodeNotFound)

	// 已取消不可核销
	r2 := confirmedReservation(t, db, 4*time.Hour, 1, 1)
	if _, err := CancelReservation(ctx, db, r2.ID, 1, "", testBase.Add(time.Minute), 2*time.Hour); err != nil {
		t.Fatal(err)
	}
	_, err = CheckIn(ctx, db, r2.ID, 2, testBase.Add(6*time.Minute))
	wantBizCode(t, err, CodeCheckinNotAllow)
}

// —— R5.2/R5.3 开始与完成：顺序、越级、幂等 ————————————

func TestServiceStartAndComplete(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := confirmedReservation(t, db, 2*time.Hour, 1, 1)

	// 未核销不能开始
	_, err := StartService(ctx, db, r.ID, 3, testBase.Add(time.Minute))
	wantBizCode(t, err, CodeServiceNotIn)
	// 未开始不能完成
	_, err = CompleteService(ctx, db, r.ID, 3, testBase.Add(time.Minute))
	wantBizCode(t, err, CodeServiceNotStart)

	if _, err := CheckIn(ctx, db, r.ID, 2, testBase.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	already, err := StartService(ctx, db, r.ID, 3, testBase.Add(3*time.Minute))
	if err != nil || already {
		t.Fatalf("开始服务应成功: already=%v err=%v", already, err)
	}
	if st, _ := reservationStatus(t, db, r.ID); st != model.ResInService {
		t.Fatalf("应为服务中，得到 %s", st)
	}
	// 重复开始幂等
	already, err = StartService(ctx, db, r.ID, 6, testBase.Add(4*time.Minute))
	if err != nil || !already {
		t.Fatalf("重复开始应幂等: already=%v err=%v", already, err)
	}

	already, err = CompleteService(ctx, db, r.ID, 3, testBase.Add(5*time.Minute))
	if err != nil || already {
		t.Fatalf("完成服务应成功: already=%v err=%v", already, err)
	}
	if st, _ := reservationStatus(t, db, r.ID); st != model.ResCompleted {
		t.Fatalf("应为已完成，得到 %s", st)
	}
	already, err = CompleteService(ctx, db, r.ID, 6, testBase.Add(6*time.Minute))
	if err != nil || !already {
		t.Fatalf("重复完成应幂等: already=%v err=%v", already, err)
	}
	if n := recordCount(t, db, "service_record", r.ID); n != 1 {
		t.Fatalf("服务记录应恰有 1 条，得到 %d", n)
	}
	var started, completed sql.NullString
	if err := db.QueryRow(`SELECT started_at, completed_at FROM service_record WHERE reservation_id=?`, r.ID).
		Scan(&started, &completed); err != nil || !started.Valid || !completed.Valid {
		t.Fatalf("服务记录应含开始与完成时间: err=%v", err)
	}

	// 角色与门店
	_, err = CheckIn(ctx, db, r.ID, 3, testBase.Add(7*time.Minute))
	wantBizCode(t, err, CodeRoleForbidden) // 服务人员不能核销
	r2 := confirmedReservation(t, db, 3*time.Hour, 1, 2)
	if _, err := CheckIn(ctx, db, r2.ID, 2, testBase.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	_, err = StartService(ctx, db, r2.ID, 10, testBase.Add(3*time.Minute)) // 别店服务
	wantBizCode(t, err, CodeStoreMismatch)
}

// —— R7.1/R7.2 未到店：时段结束、必填原因、释放名额、不退款 ——————

func TestMarkNoShow(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := confirmedReservation(t, db, time.Hour, 1, 1) // 开始 07:00，结束 08:00

	// 时段未结束
	wantBizCode(t, MarkNoShow(ctx, db, r.ID, 1, "未到店", testBase.Add(30*time.Minute)), CodeNoShowTooEarly)
	// 无原因
	wantBizCode(t, MarkNoShow(ctx, db, r.ID, 1, "   ", testBase.Add(3*time.Hour)), CodeNoShowNoReason)
	// 角色/门店
	wantBizCode(t, MarkNoShow(ctx, db, r.ID, 2, "未到店", testBase.Add(3*time.Hour)), CodeRoleForbidden)
	wantBizCode(t, MarkNoShow(ctx, db, r.ID, 8, "未到店", testBase.Add(3*time.Hour)), CodeStoreMismatch)

	// 成功
	if err := MarkNoShow(ctx, db, r.ID, 1, "客户未到场", testBase.Add(3*time.Hour)); err != nil {
		t.Fatalf("时段结束后应可标记未到店: %v", err)
	}
	st, _ := reservationStatus(t, db, r.ID)
	if st != model.ResNoShow {
		t.Fatalf("应为未到店，得到 %s", st)
	}
	var noShowAt sql.NullString
	var reason string
	if err := db.QueryRow(`SELECT no_show_at, no_show_reason FROM reservation WHERE id=?`, r.ID).
		Scan(&noShowAt, &reason); err != nil || !noShowAt.Valid || reason != "客户未到场" {
		t.Fatalf("应记录未到店时间与原因: err=%v", err)
	}
	// 名额释放一次，且不产生退款
	if n := reservedCount(t, db, r.SlotID); n != 0 {
		t.Fatalf("未到店应释放名额，得到 %d", n)
	}
	var refunds int
	if err := db.QueryRow(`SELECT COUNT(*) FROM refund WHERE reservation_id=?`, r.ID).Scan(&refunds); err != nil || refunds != 0 {
		t.Fatalf("未到店不得产生退款，得到 %d err=%v", refunds, err)
	}
	// 终态不可逆
	wantBizCode(t, MarkNoShow(ctx, db, r.ID, 1, "再标一次", testBase.Add(4*time.Hour)), CodeNotNoShowable)

	// 已完成的预约不能标记未到店（R7.1 仅已确认）
	done := confirmedReservation(t, db, 2*time.Hour, 1, 2)
	if _, err := CheckIn(ctx, db, done.ID, 2, testBase); err != nil {
		t.Fatal(err)
	}
	if _, err := StartService(ctx, db, done.ID, 3, testBase.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := CompleteService(ctx, db, done.ID, 3, testBase.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	wantBizCode(t, MarkNoShow(ctx, db, done.ID, 1, "已完成", testBase.Add(6*time.Hour)), CodeNotNoShowable)
}
