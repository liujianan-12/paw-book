//go:build t02probe

package api

// T02 复测会话新增的“相邻风险探针”（默认不参与 go test ./...，用 -tags=t02probe 运行）。
// 背景：D-T02-01 修复把「操作者」加入了幂等作用域（scope+user/staff），但幂等指纹仍只由
// 「请求体 hash」组成，**不含路由路径与路径参数**。
// 风险：同一用户对**不同预约**（POST /api/reservations/{id}/pay，body 为空）复用同一
// Idempotency-Key 时，第二次会把第一次的响应原样回放，第二个预约的支付请求被静默吞掉。
// 本探针断言“第二次必须为 res2 建支付单或明确冲突”，当前实现预期 FAIL。

import (
	"database/sql"
	"net/http"
	"testing"
)

func TestT02RetestProbeIdempotencyKeyNotScopedByResourcePath(t *testing.T) {
	s, db := newTestServer(t)
	c := newClient(t, s)
	c.login("u1")
	c.login("mgr")

	slot1 := c.createSlot(2, 3)
	slot2 := c.createSlot(3, 3)
	res1 := t02rResID(t, t02Raw(s, c.roles["u1"], "POST", "/api/reservations",
		t02JSON(map[string]any{"slotId": slot1, "petId": 1}), nil))
	res2 := t02rResID(t, t02Raw(s, c.roles["u1"], "POST", "/api/reservations",
		t02JSON(map[string]any{"slotId": slot2, "petId": 2}), nil))

	hdr := map[string]string{"Idempotency-Key": "retest-shared-key-across-resources"}

	first := t02Raw(s, c.roles["u1"], "POST", "/api/reservations/"+itoa(res1)+"/pay", nil, hdr)
	if first.Code != http.StatusOK {
		t.Fatalf("前置：res1 支付失败 %d %s", first.Code, first.Body.String())
	}
	second := t02Raw(s, c.roles["u1"], "POST", "/api/reservations/"+itoa(res2)+"/pay", nil, hdr)
	t.Logf("res2 用同一 key 发起支付：status=%d body=%s", second.Code, second.Body.String())

	var payOfRes2 int
	if err := db.QueryRow(`SELECT COUNT(*) FROM payment WHERE reservation_id=?`, res2).Scan(&payOfRes2); err != nil {
		t.Fatalf("统计 res2 支付单失败: %v", err)
	}
	t.Logf("库中 res2 的支付单数 = %d（期望 1，或第二次返回 409 冲突）", payOfRes2)

	if payOfRes2 == 0 && second.Code == http.StatusOK {
		t.Errorf("风险：同一 Idempotency-Key 跨不同预约重放时回放了首个预约的响应，res2 未建支付单"+
			"（幂等指纹不含路由路径/路径参数，server.go idem() 仅对 body 求 hash）；res1=%d res2=%d",
			res1, res2)
	}
}

var _ = sql.ErrNoRows
