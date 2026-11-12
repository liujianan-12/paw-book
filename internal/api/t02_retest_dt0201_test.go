package api

// T02 复测会话（Test Agent，独立于 Dev 会话）新增，仅测试代码。
// 目的：独立复测缺陷 D-T02-01「Idempotency-Key 作用域未按用户隔离」。
//
// 复测口径（自行按 .agent/05 §3.4 与 .agent/11 P1–P7 推导，未引用 Dev 结论）：
//  1. u1 与 u2 使用同一 Idempotency-Key，但 u2 用**自己**的宠物(petId=3)建单，
//     u2 必须得到自己的 reservationId（既不是 u1 的，也不是错误/冲突），且库中 u2 有效预约=1；
//  2. 同一用户同 key 同载荷重放 → 返回首次响应（字节一致），不重复建单/占名额；
//  3. 同一用户同 key 异载荷 → 409 IDEMPOTENCY_CONFLICT，且无副作用。

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"paw-book/internal/domain"
)

// t02rCount 计数查询（复测专用，避免与前序会话辅助函数重名）。
func t02rCount(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("计数查询失败 %q: %v", q, err)
	}
	return n
}

// t02rResOwner 返回预约的归属用户与宠物。
func t02rResOwner(t *testing.T, db *sql.DB, id int64) (userID, petID int64) {
	t.Helper()
	if err := db.QueryRow(`SELECT user_id, pet_id FROM reservation WHERE id=?`, id).Scan(&userID, &petID); err != nil {
		t.Fatalf("查询预约 %d 归属失败: %v", id, err)
	}
	return userID, petID
}

// t02rResID 从响应体解析 reservationId（业务码必须为 0）。
func t02rResID(t *testing.T, rec *httptest.ResponseRecorder) int64 {
	t.Helper()
	var env struct {
		Code int `json:"code"`
		Data struct {
			ReservationID int64 `json:"reservationId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("响应不是 JSON: %v %s", err, rec.Body.String())
	}
	if env.Code != 0 || env.Data.ReservationID == 0 {
		t.Fatalf("响应缺少 reservationId: code=%d body=%s", env.Code, rec.Body.String())
	}
	return env.Data.ReservationID
}

func TestRetestDT0201_IdempotencyKeyScopedPerUser(t *testing.T) {
	s, db := newTestServer(t)
	c := newClient(t, s)
	for _, acc := range []string{"u1", "u2", "mgr"} {
		c.login(acc)
	}
	slot := c.createSlot(2, 3)
	const key = "retest-dt0201-shared-key"
	hdr := map[string]string{"Idempotency-Key": key}

	// —— 步骤 1：u1 用 petId=1 建单 ——
	bodyU1 := t02JSON(map[string]any{"slotId": slot, "petId": 1})
	recU1 := t02Raw(s, c.roles["u1"], "POST", "/api/reservations", bodyU1, hdr)
	if recU1.Code != http.StatusOK {
		t.Fatalf("前置：u1 建单失败 %d %s", recU1.Code, recU1.Body.String())
	}
	resU1 := t02rResID(t, recU1)
	if owner, pet := t02rResOwner(t, db, resU1); owner != 1 || pet != 1 {
		t.Fatalf("前置：u1 预约归属应为 user=1/pet=1，实际 user=%d/pet=%d", owner, pet)
	}

	// —— 步骤 2：u2 用同一 key、自己的宠物 petId=3 建单 ——
	bodyU2 := t02JSON(map[string]any{"slotId": slot, "petId": 3})
	recU2 := t02Raw(s, c.roles["u2"], "POST", "/api/reservations", bodyU2, hdr)
	if recU2.Code != http.StatusOK {
		t.Fatalf("D-T02-01：u2 用同一 Idempotency-Key 为自己宠物建单应成功 200，实际 %d %s",
			recU2.Code, recU2.Body.String())
	}
	resU2 := t02rResID(t, recU2)

	if resU2 == resU1 {
		t.Fatalf("D-T02-01 未修复：u2 拿到了 u1 的 reservationId=%d（跨用户读到他人结果）", resU1)
	}
	if strings.Contains(recU2.Body.String(), `"reservationId":`+strconv.FormatInt(resU1, 10)) {
		t.Fatalf("D-T02-01 未修复：u2 响应体含 u1 的 reservationId=%d：%s", resU1, recU2.Body.String())
	}
	if owner, pet := t02rResOwner(t, db, resU2); owner != 2 || pet != 3 {
		t.Fatalf("D-T02-01：u2 的预约归属应为 user=2/pet=3，实际 user=%d/pet=%d", owner, pet)
	}

	// u2 自己的有效预约恰好 1 条；u1 仍为 1 条；时段名额 = 2
	if n := t02rCount(t, db, `SELECT COUNT(*) FROM reservation WHERE user_id=2`); n != 1 {
		t.Fatalf("D-T02-01：库中 u2 预约数应为 1，实际 %d", n)
	}
	if n := t02rCount(t, db, `SELECT COUNT(*) FROM reservation WHERE user_id=1`); n != 1 {
		t.Fatalf("u1 预约数应仍为 1，实际 %d", n)
	}
	// 幂等记录按用户隔离（作用域含 user 维度）
	if n := t02rCount(t, db, `SELECT COUNT(*) FROM idempotency_key WHERE key_value=? AND scope='reservation.create:user:1'`, key); n != 1 {
		t.Fatalf("u1 的幂等记录作用域应为 reservation.create:user:1，命中 %d 条", n)
	}
	if n := t02rCount(t, db, `SELECT COUNT(*) FROM idempotency_key WHERE key_value=? AND scope='reservation.create:user:2'`, key); n != 1 {
		t.Fatalf("u2 的幂等记录作用域应为 reservation.create:user:2，命中 %d 条", n)
	}

	// —— 步骤 3：同用户同 key 同载荷重放 → 首次响应，不改库 ——
	replayU1 := t02Raw(s, c.roles["u1"], "POST", "/api/reservations", bodyU1, hdr)
	if replayU1.Code != http.StatusOK || replayU1.Body.String() != recU1.Body.String() {
		t.Fatalf("A9：u1 同 key 同体重放应返回首次响应\n首次: %s\n重放: %d %s",
			recU1.Body.String(), replayU1.Code, replayU1.Body.String())
	}
	replayU2 := t02Raw(s, c.roles["u2"], "POST", "/api/reservations", bodyU2, hdr)
	if replayU2.Code != http.StatusOK || replayU2.Body.String() != recU2.Body.String() {
		t.Fatalf("A9：u2 同 key 同体重放应返回 u2 自己的首次响应\n首次: %s\n重放: %d %s",
			recU2.Body.String(), replayU2.Code, replayU2.Body.String())
	}
	if n := t02rCount(t, db, `SELECT COUNT(*) FROM reservation WHERE slot_id=?`, slot); n != 2 {
		t.Fatalf("重放不得重复建单：时段 %d 预约数应为 2，实际 %d", slot, n)
	}
	if n := t02rCount(t, db, `SELECT reserved_count FROM slot WHERE id=?`, slot); n != 2 {
		t.Fatalf("重放不得重复占名额：reserved_count 应为 2，实际 %d", n)
	}

	// —— 步骤 4：同用户同 key 异载荷 → 409 IDEMPOTENCY_CONFLICT，无副作用 ——
	slot2 := c.createSlot(3, 3)
	conflict := t02Raw(s, c.roles["u2"], "POST", "/api/reservations",
		t02JSON(map[string]any{"slotId": slot2, "petId": 3}), hdr)
	if conflict.Code != http.StatusConflict || errCode(t, conflict) != domain.CodeIdemConflict {
		t.Fatalf("A9：u2 同 key 异载荷应 409 %s，实际 %d %s",
			domain.CodeIdemConflict, conflict.Code, conflict.Body.String())
	}
	if n := t02rCount(t, db, `SELECT COUNT(*) FROM reservation WHERE slot_id=?`, slot2); n != 0 {
		t.Fatalf("冲突请求不得产生副作用：slot2 预约数应为 0，实际 %d", n)
	}
	// 冲突判定不得影响已存在的两条记录
	if n := t02rCount(t, db, `SELECT COUNT(*) FROM reservation WHERE slot_id=?`, slot); n != 2 {
		t.Fatalf("冲突判定后原预约数应仍为 2，实际 %d", n)
	}
}

// TestRetestDT0201_UnauthenticatedIdempotencyKeyHasNoScope 覆盖修复新增分支：
// 未登录请求携带 Idempotency-Key 时必须仍返回 401，且不得写入任何幂等记录
// （否则会落到 "user:0" 之类共享作用域，再次形成跨用户共享）。
func TestRetestDT0201_UnauthenticatedIdempotencyKeyHasNoScope(t *testing.T) {
	s, db := newTestServer(t)
	c := newClient(t, s)
	c.login("mgr")
	slot := c.createSlot(2, 3)

	const key = "retest-dt0201-anon-key"
	body := t02JSON(map[string]any{"slotId": slot, "petId": 1})

	rec := t02Raw(s, "", "POST", "/api/reservations", body, map[string]string{"Idempotency-Key": key})
	if rec.Code != http.StatusUnauthorized || errCode(t, rec) != domain.CodeUnauthenticated {
		t.Fatalf("未登录带 Idempotency-Key 应 401 %s，实际 %d %s",
			domain.CodeUnauthenticated, rec.Code, rec.Body.String())
	}
	if n := t02rCount(t, db, `SELECT COUNT(*) FROM idempotency_key WHERE key_value=?`, key); n != 0 {
		t.Fatalf("未登录请求不得写入幂等记录，实际 %d 条", n)
	}
	if n := t02rCount(t, db, `SELECT COUNT(*) FROM reservation WHERE slot_id=?`, slot); n != 0 {
		t.Fatalf("未登录请求不得建单，实际 %d 条", n)
	}

	// 同一 key 随后由已登录用户使用，必须正常工作（未登录请求未污染作用域）
	c.login("u1")
	ok := t02Raw(s, c.roles["u1"], "POST", "/api/reservations", body, map[string]string{"Idempotency-Key": key})
	if ok.Code != http.StatusOK {
		t.Fatalf("u1 复用同一 key 应正常建单 200，实际 %d %s", ok.Code, ok.Body.String())
	}
}
