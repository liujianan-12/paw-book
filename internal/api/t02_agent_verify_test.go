package api

// T02 独立测试会话新增（Test Agent）。覆盖 docs/test-plan.md 中 P7 回调签名、幂等重放/冲突、
// 越权与跨店拒绝；只含测试代码。

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"paw-book/internal/domain"
	"paw-book/internal/repo"
)

// t02Raw 发送原始请求（可带 cookie 与自定义头）。
func t02Raw(s *Server, cookie, method, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "pb_sid", Value: cookie})
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func t02JSON(v any) []byte {
	raw, _ := json.Marshal(v)
	return raw
}

// —— P7：回调签名缺失/错误必须拒绝且无副作用；正确签名才生效 ——
func TestT02P7PayNotifySignatureRequired(t *testing.T) {
	s, db := newTestServer(t)
	c := newClient(t, s)
	c.login("u1")
	c.login("mgr")

	slot := c.createSlot(2, 3)
	resID := int64(decodeData(t, c.doAs("u1", "POST", "/api/reservations", map[string]any{
		"slotId": slot, "petId": 1,
	}))["reservationId"].(float64))
	orderNo := decodeData(t, c.doAs("u1", "POST", "/api/reservations/"+itoa(resID)+"/pay", nil))["orderNo"].(string)

	body := t02JSON(map[string]any{"channelTxnId": "t02-sig-txn", "orderNo": orderNo, "success": true})

	// 缺签名
	rec := t02Raw(s, "", "POST", "/api/payment/notify", body, nil)
	if rec.Code != http.StatusUnprocessableEntity || errCode(t, rec) != domain.CodeSignInvalid {
		t.Fatalf("P7 缺签名应 422 %s，实际 %d %s", domain.CodeSignInvalid, rec.Code, rec.Body.String())
	}
	// 错误签名
	rec = t02Raw(s, "", "POST", "/api/payment/notify", body, map[string]string{"X-Mock-Signature": "deadbeef"})
	if rec.Code != http.StatusUnprocessableEntity || errCode(t, rec) != domain.CodeSignInvalid {
		t.Fatalf("P7 错误签名应 422 %s，实际 %d", domain.CodeSignInvalid, rec.Code)
	}
	// 无副作用：支付单仍 INIT、无通知流水、预约仍待支付
	if n := t02ApiCount(t, db, `SELECT COUNT(*) FROM payment WHERE reservation_id=? AND status='INIT'`, resID); n != 1 {
		t.Fatalf("P7 验签失败后支付单应仍为 INIT，实际 INIT 数=%d", n)
	}
	if n := t02ApiCount(t, db, `SELECT COUNT(*) FROM payment_notification`); n != 0 {
		t.Fatalf("P7 验签失败不得写入回调流水，实际 %d 条", n)
	}
	if st := t02ApiResStatus(t, db, resID); st != domain.ResPendingPayment {
		t.Fatalf("P7 验签失败不得改变预约状态，实际 %s", st)
	}

	// 正确签名 → 确认（对照）
	if rec := t02Raw(s, "", "POST", "/api/payment/notify", body, map[string]string{"X-Mock-Signature": s.sign(body)}); rec.Code != 200 {
		t.Fatalf("正确签名回调应成功，实际 %d %s", rec.Code, rec.Body.String())
	}
	if st := t02ApiResStatus(t, db, resID); st != domain.ResConfirmed {
		t.Fatalf("正确签名后预约应为 CONFIRMED，实际 %s", st)
	}

	// 退款回调同样必须验签
	autoCancel := c.doAs("u1", "POST", "/api/reservations/"+itoa(resID)+"/cancel", map[string]any{})
	if autoCancel.Code != 200 {
		t.Fatalf("前置：≥2h 取消失败 %d %s", autoCancel.Code, autoCancel.Body.String())
	}
	var refundNo string
	if err := db.QueryRow(`SELECT refund_no FROM refund WHERE reservation_id=?`, resID).Scan(&refundNo); err != nil {
		t.Fatalf("查询退款单失败: %v", err)
	}
	rbody := t02JSON(map[string]any{"refundNo": refundNo, "success": true, "failReason": ""})
	rec = t02Raw(s, "", "POST", "/api/refund/notify", rbody, nil)
	if rec.Code != http.StatusUnprocessableEntity || errCode(t, rec) != domain.CodeSignInvalid {
		t.Fatalf("P7 退款回调缺签名应 422，实际 %d", rec.Code)
	}
	var rStatus string
	if err := db.QueryRow(`SELECT status FROM refund WHERE reservation_id=?`, resID).Scan(&rStatus); err != nil {
		t.Fatalf("查询退款状态失败: %v", err)
	}
	if rStatus != domain.RefundPending {
		t.Fatalf("P7 验签失败不得推进退款状态，实际 %s", rStatus)
	}
}

// —— A9/A10 幂等：同 key 同载荷重放返回首次结果且不重复建单；同 key 不同载荷 409 ——
func TestT02IdempotencyReplayAndConflictOnCreateReservation(t *testing.T) {
	s, db := newTestServer(t)
	c := newClient(t, s)
	c.login("u1")
	c.login("mgr")
	slot := c.createSlot(2, 3)

	body := t02JSON(map[string]any{"slotId": slot, "petId": 1})
	hdr := map[string]string{"Idempotency-Key": "t02-idem-create-1"}

	first := t02Raw(s, c.roles["u1"], "POST", "/api/reservations", body, hdr)
	second := t02Raw(s, c.roles["u1"], "POST", "/api/reservations", body, hdr)
	if first.Code != 200 || second.Code != 200 {
		t.Fatalf("同 key 重放应 200/200，实际 %d/%d", first.Code, second.Code)
	}
	if first.Body.String() != second.Body.String() {
		t.Fatalf("同 key 重放必须返回首次结果\n第一次: %s\n第二次: %s", first.Body.String(), second.Body.String())
	}
	if n := t02ApiCount(t, db, `SELECT COUNT(*) FROM reservation WHERE slot_id=?`, slot); n != 1 {
		t.Fatalf("同 key 重放不得重复建单，实际 %d 条预约", n)
	}
	if n := t02ApiCount(t, db, `SELECT reserved_count FROM slot WHERE id=?`, slot); n != 1 {
		t.Fatalf("同 key 重放不得重复占名额，reserved_count=%d", n)
	}

	conflict := t02Raw(s, c.roles["u1"], "POST", "/api/reservations",
		t02JSON(map[string]any{"slotId": slot, "petId": 2}), hdr)
	if conflict.Code != http.StatusConflict || errCode(t, conflict) != domain.CodeIdemConflict {
		t.Fatalf("同 key 不同载荷应 409 %s，实际 %d %s", domain.CodeIdemConflict, conflict.Code, conflict.Body.String())
	}
	if n := t02ApiCount(t, db, `SELECT COUNT(*) FROM reservation WHERE slot_id=?`, slot); n != 1 {
		t.Fatalf("冲突的重复请求不得产生副作用，实际 %d 条预约", n)
	}
}

// —— P1–P7 越权与跨店：未登录、跨店核销、跨店审批、查看他人预约全部拒绝且无副作用 ——
func TestT02CrossStoreAndPermissionDenied(t *testing.T) {
	s, db := newTestServer(t)
	c := newClient(t, s)
	for _, acc := range []string{"u1", "u2", "mgr", "rec", "cs1"} {
		c.login(acc)
	}
	// 别店前台/客服（store 2）
	if _, err := db.Exec(`INSERT OR IGNORE INTO store(id, name) VALUES(2, '别家门店')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO staff(id, store_id, name, role, account, password_hash) VALUES
		(21, 2, '别家前台', 'reception', 'rec2', '8d969eef6ecad3c29a3a629280e686cf0c3f5d5a86aff3ca12020c923adc6c92'),
		(22, 2, '别家客服', 'cs', 'cs2', '8d969eef6ecad3c29a3a629280e686cf0c3f5d5a86aff3ca12020c923adc6c92')`); err != nil {
		t.Fatal(err)
	}
	c.login("rec2")
	c.login("cs2")

	slot := c.createSlot(2, 3)
	resID := int64(decodeData(t, c.doAs("u1", "POST", "/api/reservations", map[string]any{
		"slotId": slot, "petId": 1,
	}))["reservationId"].(float64))
	orderNo := decodeData(t, c.doAs("u1", "POST", "/api/reservations/"+itoa(resID)+"/pay", nil))["orderNo"].(string)
	raw := t02JSON(map[string]any{"channelTxnId": "t02-perm-txn", "orderNo": orderNo, "success": true})
	if rec := t02Raw(s, "", "POST", "/api/payment/notify", raw, map[string]string{"X-Mock-Signature": s.sign(raw)}); rec.Code != 200 {
		t.Fatalf("前置支付回调失败: %d %s", rec.Code, rec.Body.String())
	}

	// P5 跨店核销 → 403 STORE_MISMATCH
	rec := c.doAs("rec2", "POST", "/api/reception/reservations/"+itoa(resID)+"/checkin", nil)
	if rec.Code != http.StatusForbidden || errCode(t, rec) != domain.CodeStoreMismatch {
		t.Fatalf("P5 跨店核销应 403 %s，实际 %d %s", domain.CodeStoreMismatch, rec.Code, rec.Body.String())
	}
	if st := t02ApiResStatus(t, db, resID); st != domain.ResConfirmed {
		t.Fatalf("P5 跨店核销被拒后状态不得变化，实际 %s", st)
	}
	if n := t02ApiCount(t, db, `SELECT COUNT(*) FROM checkin_record WHERE reservation_id=?`, resID); n != 0 {
		t.Fatalf("P5 跨店核销不得落核销记录，实际 %d 条", n)
	}

	// P2 用户查看他人预约 → 404（不泄露存在性）
	rec = c.doAs("u2", "GET", "/api/reservations/"+itoa(resID), nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("P2 他人预约应 404，实际 %d %s", rec.Code, rec.Body.String())
	}
	// P1 未登录访问店长日清 → 401
	rec = t02Raw(s, "", "GET", "/api/manager/daily-close", nil, nil)
	if rec.Code != http.StatusUnauthorized || errCode(t, rec) != domain.CodeUnauthenticated {
		t.Fatalf("P1 未登录应 401 %s，实际 %d", domain.CodeUnauthenticated, rec.Code)
	}
	// 角色越权：前台访问店长日清 → 403
	rec = c.doAs("rec", "GET", "/api/manager/daily-close", nil)
	if rec.Code != http.StatusForbidden || errCode(t, rec) != domain.CodeRoleForbidden {
		t.Fatalf("P6 前台访问日清应 403，实际 %d %s", rec.Code, rec.Body.String())
	}

	// P7 跨店客服审批 store1 的取消申请 → 403；申请仍为 PENDING
	// 先让 u1 在 <2h 内提交取消申请（新建一个更近的时段）
	slot2 := c.createSlot(2, 3)
	res2 := int64(decodeData(t, c.doAs("u2", "POST", "/api/reservations", map[string]any{
		"slotId": slot2, "petId": 3,
	}))["reservationId"].(float64))
	orderNo2 := decodeData(t, c.doAs("u2", "POST", "/api/reservations/"+itoa(res2)+"/pay", nil))["orderNo"].(string)
	raw2 := t02JSON(map[string]any{"channelTxnId": "t02-perm-txn-2", "orderNo": orderNo2, "success": true})
	if rec := t02Raw(s, "", "POST", "/api/payment/notify", raw2, map[string]string{"X-Mock-Signature": s.sign(raw2)}); rec.Code != 200 {
		t.Fatalf("前置支付回调失败: %d", rec.Code)
	}
	// 时钟推进到开始前 1h：服务端时钟固定，改用直接改库 start_at 制造 <2h 场景
	if _, err := db.Exec(`UPDATE slot SET start_at=? WHERE id=?`,
		repo.FmtTime(base.Add(time.Hour)), slot2); err != nil {
		t.Fatalf("调整时段起始时间失败: %v", err)
	}
	submit := c.doAs("u2", "POST", "/api/reservations/"+itoa(res2)+"/cancel", map[string]any{"reason": "临时有事"})
	if submit.Code != 200 {
		t.Fatalf("前置：<2h 取消申请失败 %d %s", submit.Code, submit.Body.String())
	}
	reqID := int64(decodeData(t, submit)["cancelRequestId"].(float64))

	rec = c.doAs("cs2", "POST", "/api/cs/cancel-requests/"+itoa(reqID)+"/approve", map[string]any{"note": "越权"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("P7 跨店客服审批应 403，实际 %d %s", rec.Code, rec.Body.String())
	}
	var reqStatus string
	if err := db.QueryRow(`SELECT status FROM cancel_request WHERE id=?`, reqID).Scan(&reqStatus); err != nil {
		t.Fatalf("查询取消申请失败: %v", err)
	}
	if reqStatus != domain.CancelReqPending {
		t.Fatalf("P7 越权审批不得改变申请状态，实际 %s", reqStatus)
	}
	if st := t02ApiResStatus(t, db, res2); st != domain.ResCancellationReview {
		t.Fatalf("P7 越权审批不得改变预约状态，实际 %s", st)
	}
}

// —— 本文件的轻量断言辅助 ——

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

func t02ApiCount(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("计数查询失败 %q: %v", q, err)
	}
	return n
}

func t02ApiResStatus(t *testing.T, db *sql.DB, id int64) string {
	t.Helper()
	var s string
	if err := db.QueryRow(`SELECT status FROM reservation WHERE id=?`, id).Scan(&s); err != nil {
		t.Fatalf("查询预约 %d 状态失败: %v", id, err)
	}
	return s
}
