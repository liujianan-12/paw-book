package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"paw-book/internal/domain"
	"paw-book/internal/store"
)

var base = time.Date(2026, 11, 14, 6, 0, 0, 0, time.UTC)

func newTestServer(t *testing.T) (*Server, *sql.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	return New(db, func() time.Time { return base }, 2*time.Hour, 5*time.Minute, "test-secret"), db
}

type client struct {
	t     *testing.T
	s     *Server
	roles map[string]string // role -> cookie
}

func newClient(t *testing.T, s *Server) *client {
	return &client{t: t, s: s, roles: map[string]string{}}
}

func (c *client) login(account string) string {
	c.t.Helper()
	rec := c.doAs("", "POST", "/api/auth/login", map[string]any{"account": account, "password": "123456"})
	if rec.Code != 200 {
		c.t.Fatalf("登录 %s 失败: %d %s", account, rec.Code, rec.Body.String())
	}
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == "pb_sid" {
			c.roles[account] = ck.Value
			return ck.Value
		}
	}
	c.t.Fatalf("登录 %s 未返回会话 cookie", account)
	return ""
}

func (c *client) doAs(account, method, path string, body any) *httptest.ResponseRecorder {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if account != "" {
		req.AddCookie(&http.Cookie{Name: "pb_sid", Value: c.roles[account]})
	}
	rec := httptest.NewRecorder()
	c.s.Handler().ServeHTTP(rec, req)
	return rec
}

func decodeData(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if rec.Code < 200 || rec.Code >= 300 {
		t.Fatalf("期望 2xx，得到 %d: %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("响应不是 JSON: %v %s", err, rec.Body.String())
	}
	if env.Code != 0 {
		t.Fatalf("业务码非 0: %d", env.Code)
	}
	return env.Data
}

func decodeList(t *testing.T, rec *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	if rec.Code < 200 || rec.Code >= 300 {
		t.Fatalf("期望 2xx，得到 %d: %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Code int              `json:"code"`
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("列表响应不是 JSON: %v %s", err, rec.Body.String())
	}
	if env.Code != 0 {
		t.Fatalf("业务码非 0: %d", env.Code)
	}
	return env.Data
}

func errCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var env struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("错误响应不是 JSON: %v %s", err, rec.Body.String())
	}
	return env.Code
}

func (c *client) createSlot(hours int, capacity int) int64 {
	c.t.Helper()
	rec := c.doAs("mgr", "POST", "/api/manager/slots", map[string]any{
		"startAt":  base.Add(time.Duration(hours) * time.Hour).Format(time.RFC3339),
		"endAt":    base.Add(time.Duration(hours+1) * time.Hour).Format(time.RFC3339),
		"capacity": capacity,
	})
	return int64(decodeData(c.t, rec)["slotId"].(float64))
}

// 登录、静态页、健康检查与冒烟。
func TestAuthPagesAndHealth(t *testing.T) {
	s, _ := newTestServer(t)
	c := newClient(t, s)

	for _, path := range []string{"/login.html", "/user.html", "/reception.html", "/service.html", "/cs.html", "/manager.html"} {
		rec := c.doAs("", "GET", path, nil)
		if rec.Code != 200 || rec.Header().Get("Content-Type") == "" {
			t.Fatalf("页面 %s 应可访问: %d", path, rec.Code)
		}
	}
	if rec := c.doAs("", "GET", "/", nil); rec.Code != http.StatusFound {
		t.Fatalf("根路径应跳转登录页: %d", rec.Code)
	}
	if rec := c.doAs("", "GET", "/nope", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("未知路径应 404: %d", rec.Code)
	}
	for _, path := range []string{"/healthz", "/readyz", "/api/smoke"} {
		if rec := c.doAs("", "GET", path, nil); rec.Code != 200 {
			t.Fatalf("%s 应 200: %d", path, rec.Code)
		}
	}

	// 未登录 401
	if rec := c.doAs("", "GET", "/api/slots", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应 401: %d", rec.Code)
	}
	// 密码错误
	rec := c.doAs("", "POST", "/api/auth/login", map[string]any{"account": "u1", "password": "bad"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("密码错误应 401: %d", rec.Code)
	}
	// 角色 cookie 齐全
	for _, acc := range []string{"u1", "mgr", "rec", "srv", "cs1"} {
		c.login(acc)
	}
	me := decodeData(t, c.doAs("u1", "GET", "/api/me", nil))
	if me["Role"] != "user" {
		t.Fatalf("/api/me 应返回 user: %v", me)
	}
	// 登出后会话失效
	rec = c.doAs("u1", "POST", "/api/auth/logout", nil)
	if rec.Code != 200 {
		t.Fatalf("登出应 200: %d", rec.Code)
	}
	if rec := c.doAs("u1", "GET", "/api/me", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("登出后应 401: %d", rec.Code)
	}
}

// 主链路：发布→预约→支付→回调→核销→开始→完成→日清。
func TestHappyPath(t *testing.T) {
	s, _ := newTestServer(t)
	c := newClient(t, s)
	for _, acc := range []string{"u1", "u2", "mgr", "rec", "srv", "cs1"} {
		c.login(acc)
	}
	slotID := c.createSlot(3, 3)

	if len(decodeList(t, c.doAs("u1", "GET", "/api/slots", nil))) == 0 {
		t.Fatal("用户应看到可约时段")
	}

	res := decodeData(t, c.doAs("u1", "POST", "/api/reservations", map[string]any{"slotId": slotID, "petId": 1}))
	resID := int64(res["reservationId"].(float64))
	if res["status"] != domain.ResPendingPayment {
		t.Fatalf("初始状态应为待支付: %v", res)
	}

	pay := decodeData(t, c.doAs("u1", "POST", fmt.Sprintf("/api/reservations/%d/pay", resID), nil))
	orderNo := pay["orderNo"].(string)

	// 签名错误
	body := map[string]any{"channelTxnId": "txn-1", "orderNo": orderNo, "success": true}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/payment/notify", bytes.NewReader(raw))
	req.Header.Set("X-Mock-Signature", "bad")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity || errCode(t, rec) != domain.CodeSignInvalid {
		t.Fatalf("签名错误应 422/SIGN_INVALID: %d", rec.Code)
	}

	// 正确签名 → 确认
	req = httptest.NewRequest("POST", "/api/payment/notify", bytes.NewReader(raw))
	req.Header.Set("X-Mock-Signature", s.sign(raw))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if res := decodeData(t, rec); res["result"] != domain.NotifyConfirmed {
		t.Fatalf("回调应确认: %v", res)
	}
	// 重复回调幂等
	req = httptest.NewRequest("POST", "/api/payment/notify", bytes.NewReader(raw))
	req.Header.Set("X-Mock-Signature", s.sign(raw))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if res := decodeData(t, rec); res["result"] != domain.NotifyDuplicate {
		t.Fatalf("重复回调应幂等: %v", res)
	}

	// 详情与列表
	detail := decodeData(t, c.doAs("u1", "GET", fmt.Sprintf("/api/reservations/%d", resID), nil))
	if detail["PetName"] != "豆豆" {
		t.Fatalf("详情宠物不符: %v", detail)
	}
	if rec := c.doAs("u2", "GET", fmt.Sprintf("/api/reservations/%d", resID), nil); rec.Code != http.StatusNotFound {
		t.Fatalf("他人预约应 404: %d", rec.Code)
	}
	if len(decodeList(t, c.doAs("u1", "GET", "/api/my/reservations", nil))) == 0 {
		t.Fatal("我的预约不应为空")
	}

	// 核销 → 开始 → 完成（含幂等）
	if d := decodeData(t, c.doAs("rec", "POST", fmt.Sprintf("/api/reception/reservations/%d/checkin", resID), nil)); d["alreadyDone"] != false {
		t.Fatalf("首次核销 alreadyDone=false: %v", d)
	}
	if d := decodeData(t, c.doAs("rec", "POST", fmt.Sprintf("/api/reception/reservations/%d/checkin", resID), nil)); d["alreadyDone"] != true {
		t.Fatalf("重复核销应幂等: %v", d)
	}
	decodeData(t, c.doAs("srv", "POST", fmt.Sprintf("/api/service/reservations/%d/start", resID), nil))
	decodeData(t, c.doAs("srv", "POST", fmt.Sprintf("/api/service/reservations/%d/complete", resID), nil))
	if d := decodeData(t, c.doAs("srv", "POST", fmt.Sprintf("/api/service/reservations/%d/complete", resID), nil)); d["alreadyDone"] != true {
		t.Fatalf("重复完成应幂等: %v", d)
	}

	// 日清勾稽
	daily := decodeData(t, c.doAs("mgr", "GET", "/api/manager/daily-close", nil))
	if daily["UnaccountedCents"].(float64) != 0 || daily["CompletedIncomeCents"].(float64) != 9900 {
		t.Fatalf("日清勾稽不符: %v", daily)
	}
}

// 取消政策、审核、退款与未到店。
func TestCancelReviewRefundNoShow(t *testing.T) {
	s, db := newTestServer(t)
	c := newClient(t, s)
	for _, acc := range []string{"u1", "mgr", "rec", "srv", "cs1"} {
		c.login(acc)
	}

	// 未付款取消
	slot0 := c.createSlot(2, 3)
	r0 := int64(decodeData(t, c.doAs("u1", "POST", "/api/reservations", map[string]any{"slotId": slot0, "petId": 1}))["reservationId"].(float64))
	if d := decodeData(t, c.doAs("u1", "POST", fmt.Sprintf("/api/reservations/%d/cancel", r0), map[string]any{})); d["status"] != "CANCELLED" {
		t.Fatalf("未付款取消应直接成功: %v", d)
	}

	// 已付款、提前 4h → 自动受理（退款待处理）
	slot1 := c.createSlot(4, 3)
	r1 := int64(decodeData(t, c.doAs("u1", "POST", "/api/reservations", map[string]any{"slotId": slot1, "petId": 1}))["reservationId"].(float64))
	p1 := decodeData(t, c.doAs("u1", "POST", fmt.Sprintf("/api/reservations/%d/pay", r1), nil))
	c.notify(t, p1["orderNo"].(string), "txn-a", true)
	if d := decodeData(t, c.doAs("u1", "POST", fmt.Sprintf("/api/reservations/%d/cancel", r1), map[string]any{})); d["status"] != "AUTO_ACCEPTED" {
		t.Fatalf("提前取消应自动受理: %v", d)
	}

	// 已付款、提前 1h → 审核；无原因 422
	slot2 := c.createSlot(1, 3)
	r2 := int64(decodeData(t, c.doAs("u1", "POST", "/api/reservations", map[string]any{"slotId": slot2, "petId": 2}))["reservationId"].(float64))
	p2 := decodeData(t, c.doAs("u1", "POST", fmt.Sprintf("/api/reservations/%d/pay", r2), nil))
	c.notify(t, p2["orderNo"].(string), "txn-b", true)
	rec := c.doAs("u1", "POST", fmt.Sprintf("/api/reservations/%d/cancel", r2), map[string]any{})
	if rec.Code != http.StatusUnprocessableEntity || errCode(t, rec) != domain.CodeCancelReason {
		t.Fatalf("临近取消无原因应 422: %d", rec.Code)
	}
	cr := decodeData(t, c.doAs("u1", "POST", fmt.Sprintf("/api/reservations/%d/cancel", r2), map[string]any{"reason": "宠物生病"}))
	reqID := int64(cr["cancelRequestId"].(float64))
	if cr["status"] != domain.ResCancellationReview {
		t.Fatalf("应进入审核: %v", cr)
	}

	// 待审期间核销被拒
	if rec := c.doAs("rec", "POST", fmt.Sprintf("/api/reception/reservations/%d/checkin", r2), nil); rec.Code != http.StatusConflict {
		t.Fatalf("待审核期间核销应 409: %d %s", rec.Code, rec.Body.String())
	}
	// 客服列表 + 批准
	list := decodeList(t, c.doAs("cs1", "GET", "/api/cs/cancel-requests?status=PENDING", nil))
	if len(list) == 0 {
		t.Fatal("客服应看到待审申请")
	}
	decodeData(t, c.doAs("cs1", "POST", fmt.Sprintf("/api/cs/cancel-requests/%d/approve", reqID), map[string]any{"note": "同意"}))
	// 重复批准 409
	if rec := c.doAs("cs1", "POST", fmt.Sprintf("/api/cs/cancel-requests/%d/approve", reqID), map[string]any{"note": "x"}); rec.Code != http.StatusConflict {
		t.Fatalf("重复批准应 409: %d", rec.Code)
	}
	// 退款列表 + 重试
	refunds := decodeList(t, c.doAs("cs1", "GET", "/api/cs/refunds?status=UNFINISHED", nil))
	if len(refunds) < 2 {
		t.Fatalf("应有至少 2 条未完成退款，得到 %d", len(refunds))
	}
	refundID := int64(refunds[0]["ID"].(float64))
	decodeData(t, c.doAs("cs1", "POST", fmt.Sprintf("/api/cs/refunds/%d/retry", refundID), nil))
	// 退款回调成功（签名）
	refundNo := refunds[0]["RefundNo"].(string)
	c.refundNotify(t, refundNo, true)

	// 未到店：直接造一个已结束时段 + 已确认预约
	if _, err := db.Exec(`INSERT INTO slot(id, store_id, item_id, start_at, end_at, capacity, reserved_count, created_by, created_at)
		VALUES(900, 1, 1, ?, ?, 3, 1, 1, ?)`, base.Add(-3*time.Hour).Format(time.RFC3339),
		base.Add(-2*time.Hour).Format(time.RFC3339), base.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	rs, err := db.Exec(`INSERT INTO reservation(store_id, slot_id, user_id, pet_id, item_id, status, price_cents,
		pay_expire_at, seat_released, version, created_at, updated_at)
		VALUES(1, 900, 1, 1, 1, 'CONFIRMED', 9900, ?, 0, 0, ?, ?)`,
		base.Add(5*time.Minute).Format(time.RFC3339), base.Format(time.RFC3339), base.Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	nsID, _ := rs.LastInsertId()
	// 未到店预约必须是已付款的（R7 仅针对已确认预约），补一条成功支付保证勾稽成立
	if _, err := db.Exec(`INSERT INTO payment(reservation_id, order_no, amount_cents, channel, status, created_at)
		VALUES(?, ?, 9900, 'mock', 'SUCCESS', ?)`, nsID, "NS-ORDER", base.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if rec := c.doAs("mgr", "POST", fmt.Sprintf("/api/manager/reservations/%d/no-show", nsID), map[string]any{"reason": "未到场"}); rec.Code != 200 {
		t.Fatalf("标记未到店应成功: %d %s", rec.Code, rec.Body.String())
	}
	if rec := c.doAs("mgr", "POST", fmt.Sprintf("/api/manager/reservations/%d/no-show", nsID), map[string]any{"reason": "再标"}); rec.Code != http.StatusConflict {
		t.Fatalf("重复标记未到店应 409: %d", rec.Code)
	}
	// 日清仍有勾稽 0
	daily := decodeData(t, c.doAs("mgr", "GET", "/api/manager/daily-close?date=2026-11-14", nil))
	if daily["UnaccountedCents"].(float64) != 0 {
		t.Fatalf("日清差额应为 0: %v", daily)
	}
}

func (c *client) notify(t *testing.T, orderNo, txn string, success bool) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"channelTxnId": txn, "orderNo": orderNo, "success": success})
	req := httptest.NewRequest("POST", "/api/payment/notify", bytes.NewReader(raw))
	req.Header.Set("X-Mock-Signature", c.s.sign(raw))
	rec := httptest.NewRecorder()
	c.s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("支付回调失败: %d %s", rec.Code, rec.Body.String())
	}
}

func (c *client) refundNotify(t *testing.T, refundNo string, success bool) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"refundNo": refundNo, "success": success, "failReason": ""})
	req := httptest.NewRequest("POST", "/api/refund/notify", bytes.NewReader(raw))
	req.Header.Set("X-Mock-Signature", c.s.sign(raw))
	rec := httptest.NewRecorder()
	c.s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("退款回调失败: %d %s", rec.Code, rec.Body.String())
	}
}

// 权限矩阵、参数校验与幂等重放。
func TestPermissionsIdempotencyAndBadInput(t *testing.T) {
	s, _ := newTestServer(t)
	c := newClient(t, s)
	for _, acc := range []string{"u1", "mgr", "rec", "srv", "cs1"} {
		c.login(acc)
	}

	// 角色越权
	cases := []struct {
		acc, method, path string
	}{
		{"u1", "POST", "/api/manager/slots"},
		{"u1", "POST", "/api/reception/reservations/1/checkin"},
		{"u1", "POST", "/api/service/reservations/1/start"},
		{"u1", "POST", "/api/cs/refunds/1/retry"},
		{"u1", "GET", "/api/manager/daily-close"},
		{"rec", "POST", "/api/manager/reservations/1/no-show"},
	}
	for _, cs := range cases {
		if rec := c.doAs(cs.acc, cs.method, cs.path, map[string]any{}); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s 应 403，得到 %d", cs.acc, cs.path, rec.Code)
		}
	}

	// 参数校验
	if rec := c.doAs("mgr", "POST", "/api/manager/slots", map[string]any{"startAt": "bad", "endAt": "bad"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法时间应 400: %d", rec.Code)
	}
	if rec := c.doAs("u1", "POST", "/api/reservations/abc/pay", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 ID 应 400: %d", rec.Code)
	}
	if rec := c.doAs("mgr", "GET", "/api/manager/daily-close?date=bad", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法日期应 400: %d", rec.Code)
	}
	if rec := c.doAs("u1", "GET", "/api/reservations/notnum", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法预约 ID 应 400: %d", rec.Code)
	}
	// 不存在
	if rec := c.doAs("u1", "GET", "/api/reservations/9999", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("不存在预约应 404: %d", rec.Code)
	}
	// 未付款预约取消走 A6 分支
	slot := c.createSlot(2, 3)
	r := int64(decodeData(t, c.doAs("u1", "POST", "/api/reservations", map[string]any{"slotId": slot, "petId": 1}))["reservationId"].(float64))

	// 幂等：同 key 同体重放
	key := "idem-cancel-1"
	do := func(body any) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		_ = json.NewEncoder(&buf).Encode(body)
		req := httptest.NewRequest("POST", fmt.Sprintf("/api/reservations/%d/cancel", r), &buf)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		req.AddCookie(&http.Cookie{Name: "pb_sid", Value: c.roles["u1"]})
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}
	first := do(map[string]any{})
	second := do(map[string]any{})
	if first.Code != 200 || second.Code != 200 || first.Body.String() != second.Body.String() {
		t.Fatalf("同 key 重放应返回首次结果: %d/%d", first.Code, second.Code)
	}
	// 同 key 不同体 → 409
	third := do(map[string]any{"reason": "不同载荷"})
	if third.Code != http.StatusConflict || errCode(t, third) != domain.CodeIdemConflict {
		t.Fatalf("同 key 不同体应 409 IDEMPOTENCY_CONFLICT: %d", third.Code)
	}

	// 审批不存在 / 退款不存在
	if rec := c.doAs("cs1", "POST", "/api/cs/cancel-requests/999/approve", map[string]any{}); rec.Code != http.StatusNotFound {
		t.Fatalf("不存在申请应 404: %d", rec.Code)
	}
	if rec := c.doAs("cs1", "POST", "/api/cs/refunds/999/retry", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("不存在退款应 404: %d", rec.Code)
	}
	if rec := c.doAs("cs1", "POST", "/api/cs/cancel-requests/abc/reject", map[string]any{}); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法申请 ID 应 400: %d", rec.Code)
	}
	// 退款列表不过滤
	_ = decodeData(t, c.doAs("cs1", "GET", "/api/cs/refunds", nil))

	// 内部错误：关闭数据库后 ready 503、smoke 500
	if err := s.DB.Close(); err != nil {
		t.Fatal(err)
	}
	if rec := c.doAs("", "GET", "/readyz", nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("数据库关闭后 readyz 应 503: %d", rec.Code)
	}
	if rec := c.doAs("", "GET", "/api/smoke", nil); rec.Code != http.StatusInternalServerError {
		t.Fatalf("数据库关闭后 smoke 应 500: %d", rec.Code)
	}
	// 登录数据库错误分支
	if rec := c.doAs("", "POST", "/api/auth/login", map[string]any{"account": "u1", "password": "123456"}); rec.Code != http.StatusInternalServerError {
		t.Fatalf("数据库关闭后登录应 500: %d", rec.Code)
	}
}

// 各 Handler 的错误分支（参数、状态、签名、不存在）。
func TestHandlerErrorBranches(t *testing.T) {
	s, _ := newTestServer(t)
	c := newClient(t, s)
	for _, acc := range []string{"u1", "mgr", "rec", "srv", "cs1"} {
		c.login(acc)
	}

	// 请求体不是 JSON
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader([]byte("{bad")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400: %d", rec.Code)
	}

	// 时段创建业务错误（过去时间）
	if rec := c.doAs("mgr", "POST", "/api/manager/slots", map[string]any{
		"startAt": base.Add(-2 * time.Hour).Format(time.RFC3339),
		"endAt":   base.Add(-time.Hour).Format(time.RFC3339)}); rec.Code != http.StatusBadRequest {
		t.Fatalf("过去时段应 400: %d", rec.Code)
	}
	// 预约不存在 / 名额满
	if rec := c.doAs("u1", "POST", "/api/reservations", map[string]any{"slotId": 9999, "petId": 1}); rec.Code != http.StatusNotFound {
		t.Fatalf("时段不存在应 404: %d %s", rec.Code, rec.Body.String())
	}
	// 支付不存在 / 已支付
	if rec := c.doAs("u1", "POST", "/api/reservations/9999/pay", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("预约不存在支付应 404: %d", rec.Code)
	}
	slot := c.createSlot(3, 3)
	r := int64(decodeData(t, c.doAs("u1", "POST", "/api/reservations", map[string]any{"slotId": slot, "petId": 1}))["reservationId"].(float64))
	p := decodeData(t, c.doAs("u1", "POST", fmt.Sprintf("/api/reservations/%d/pay", r), nil))
	c.notify(t, p["orderNo"].(string), "txn-err", true)
	if rec := c.doAs("u1", "POST", fmt.Sprintf("/api/reservations/%d/pay", r), nil); rec.Code != http.StatusConflict {
		t.Fatalf("已支付再发起应 409: %d", rec.Code)
	}
	// 取消失败分支：不存在
	if rec := c.doAs("u1", "POST", "/api/reservations/9999/cancel", map[string]any{"reason": "x"}); rec.Code != http.StatusNotFound {
		t.Fatalf("不存在预约取消应 404: %d", rec.Code)
	}

	// 未核销直接开始/完成
	slot2 := c.createSlot(4, 3)
	r2 := int64(decodeData(t, c.doAs("u1", "POST", "/api/reservations", map[string]any{"slotId": slot2, "petId": 2}))["reservationId"].(float64))
	if rec := c.doAs("srv", "POST", fmt.Sprintf("/api/service/reservations/%d/start", r2), nil); rec.Code != http.StatusConflict {
		t.Fatalf("未核销开始应 409: %d", rec.Code)
	}
	if rec := c.doAs("srv", "POST", fmt.Sprintf("/api/service/reservations/%d/complete", r2), nil); rec.Code != http.StatusConflict {
		t.Fatalf("未开始完成应 409: %d", rec.Code)
	}
	// 非法 ID
	if rec := c.doAs("rec", "POST", "/api/reception/reservations/x/checkin", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法核销 ID 应 400: %d", rec.Code)
	}
	if rec := c.doAs("srv", "POST", "/api/service/reservations/x/start", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法开始 ID 应 400: %d", rec.Code)
	}
	if rec := c.doAs("srv", "POST", "/api/service/reservations/x/complete", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法完成 ID 应 400: %d", rec.Code)
	}

	// 未到店无原因 / 非法 ID
	if rec := c.doAs("mgr", "POST", fmt.Sprintf("/api/manager/reservations/%d/no-show", r), map[string]any{"reason": " "}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("未到店无原因应 422: %d", rec.Code)
	}
	if rec := c.doAs("mgr", "POST", "/api/manager/reservations/x/no-show", map[string]any{"reason": "x"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("未到店非法 ID 应 400: %d", rec.Code)
	}

	// 回调：缺签名、签名正确但体非法、单号不存在
	raw := []byte(`{"channelTxnId":"t","orderNo":"x","success":true}`)
	req = httptest.NewRequest("POST", "/api/payment/notify", bytes.NewReader(raw))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("缺签名应 422: %d", rec.Code)
	}
	badBody := []byte("{bad")
	req = httptest.NewRequest("POST", "/api/payment/notify", bytes.NewReader(badBody))
	req.Header.Set("X-Mock-Signature", s.sign(badBody))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("回调体非法应 400: %d", rec.Code)
	}
	req = httptest.NewRequest("POST", "/api/payment/notify", bytes.NewReader(raw))
	req.Header.Set("X-Mock-Signature", s.sign(raw))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("支付单不存在应 404: %d", rec.Code)
	}
	if rec := c.doAs("", "POST", "/api/refund/notify", map[string]any{}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("退款回调缺签名应 422: %d", rec.Code)
	}
	rb, _ := json.Marshal(map[string]any{"refundNo": "NOPE", "success": true})
	req = httptest.NewRequest("POST", "/api/refund/notify", bytes.NewReader(rb))
	req.Header.Set("X-Mock-Signature", s.sign(rb))
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("退款单不存在应 404: %d", rec.Code)
	}
}

// 审核拒绝分支单独覆盖（恢复已确认）。
func TestRejectRestoresConfirmed(t *testing.T) {
	s, _ := newTestServer(t)
	c := newClient(t, s)
	for _, acc := range []string{"u1", "mgr", "cs1"} {
		c.login(acc)
	}
	slot := c.createSlot(1, 3)
	r := int64(decodeData(t, c.doAs("u1", "POST", "/api/reservations", map[string]any{"slotId": slot, "petId": 1}))["reservationId"].(float64))
	p := decodeData(t, c.doAs("u1", "POST", fmt.Sprintf("/api/reservations/%d/pay", r), nil))
	c.notify(t, p["orderNo"].(string), "txn-reject", true)
	cr := decodeData(t, c.doAs("u1", "POST", fmt.Sprintf("/api/reservations/%d/cancel", r), map[string]any{"reason": "不想去了"}))
	reqID := int64(cr["cancelRequestId"].(float64))
	if rec := c.doAs("cs1", "POST", fmt.Sprintf("/api/cs/cancel-requests/%d/reject", reqID), map[string]any{"note": "已排班"}); rec.Code != 200 {
		t.Fatalf("拒绝应成功: %d %s", rec.Code, rec.Body.String())
	}
	detail := decodeData(t, c.doAs("u1", "GET", fmt.Sprintf("/api/reservations/%d", r), nil))
	if detail["Status"] != domain.ResConfirmed {
		t.Fatalf("拒绝后应恢复已确认: %v", detail)
	}
}
