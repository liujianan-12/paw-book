//go:build t02probe

package api

// T02 独立测试会话的“缺陷探针”（默认不参与 go test ./...，用 -tags=t02probe 显式运行）。
// 目的：验证 Idempotency-Key 作用域是否按用户隔离（05 §3.4 / P1–P7 越权矩阵）。
// 期望：不同会话复用同一 key 不应读到他人首次结果。当前实现会泄露他人响应，
// 故本探针预期 FAIL，作为缺陷 D-T02-01 的复现证据；修复后应转为 PASS。

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestT02ProbeCrossUserIdempotencyKeyReplay(t *testing.T) {
	s, db := newTestServer(t)
	c := newClient(t, s)
	c.login("u1")
	c.login("u2")
	c.login("mgr")
	slot := c.createSlot(2, 3)

	body := t02JSON(map[string]any{"slotId": slot, "petId": 1})
	hdr := map[string]string{"Idempotency-Key": "t02-shared-key"}

	first := t02Raw(s, c.roles["u1"], "POST", "/api/reservations", body, hdr)
	if first.Code != 200 {
		t.Fatalf("前置：u1 创建预约失败 %d %s", first.Code, first.Body.String())
	}
	var env1 struct {
		Code int `json:"code"`
		Data map[string]any
	}
	_ = json.Unmarshal(first.Body.Bytes(), &env1)
	resOfU1 := int64(env1.Data["reservationId"].(float64))

	var ownerOfU1 int64
	if err := db.QueryRow(`SELECT user_id FROM reservation WHERE id=?`, resOfU1).Scan(&ownerOfU1); err != nil {
		t.Fatalf("查询预约归属失败: %v", err)
	}

	// u2 使用同一 key + 同一载荷
	second := t02Raw(s, c.roles["u2"], "POST", "/api/reservations", body, hdr)
	t.Logf("u2 复用同一 Idempotency-Key 的响应：status=%d body=%s", second.Code, second.Body.String())

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM reservation WHERE user_id=2`).Scan(&n); err != nil {
		t.Fatalf("统计 u2 预约失败: %v", err)
	}
	t.Logf("库中 u2 自己的预约数 = %d（期望 1）", n)

	if second.Code == http.StatusOK && n == 0 {
		t.Errorf("缺陷：不同用户复用同一 Idempotency-Key 时返回了他人（user_id=%d）的首次结果 %d，且未为 u2 建单；"+
			"幂等作用域缺少用户/会话维度，属越权读取（P1/P7）", ownerOfU1, resOfU1)
	}
	if second.Code != http.StatusConflict && second.Code != http.StatusOK {
		t.Logf("其他行为（非本缺陷判定）：%d", second.Code)
	}
}
