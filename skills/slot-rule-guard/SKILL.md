---
name: slot-rule-guard
description: 业务专项 Skill。检查宠物门店预约的名额账、状态机、取消政策、退款独立性与金额对账是否一致；发现违规时输出可复现证据并交回开发修复。
version: 0.1.0
owner: <学生姓名>
---

# slot-rule-guard（业务专项检查）

## 用途

对某一营业日（或指定时段）做**只读**业务一致性审计：名额是否超卖/漏释放、同宠物同时段是否重复有效、状态与时间戳是否合法、取消政策是否按 2 小时边界执行、待审取消期间是否被核销、退款是否独立且金额正确、关闭后支付是否异常退款且未恢复预约、日清金额四口径能否对上。检查项与规则编号（R/I）一一对应，发现的问题直接进入缺陷流程。

## 触发场景（满足任一即必须调用）

- 每次完成 R1/R2/R3/R4/R5/R7/R8 相关任务后，自测前。
- Test Agent 开始独立测试前（先审计再写新用例）。
- 验收前、部署冒烟后。
- 修复并发/名额/退款缺陷后复检。

## 输入（缺一项则不能执行）

1. 只读数据库连接：`SLOT_AUDIT_DSN`（环境变量，禁止写入 Skill 文件）。
2. 目标日期 `AUDIT_DATE=YYYY-MM-DD`，门店 ID `STORE_ID`（本题固定 1 个门店）。
3. 可选：日清接口地址与只读 token，用于交叉核对 API 金额。
4. 当前提交 SHA（写入输出，便于追溯）。

## 执行步骤

### 步骤 1：准备

```bash
# 只读连接，禁止 UPDATE/DELETE
export SLOT_AUDIT_DSN='user:pass@tcp(host:3306)/pet_store?...'
export AUDIT_DATE='2026-11-12'; export STORE_ID=1
git rev-parse HEAD   # 记录审计版本
```

### 步骤 2：执行检查 SQL（保存为 `skills/slot-rule-guard/checks.sql`）

```sql
-- I1/I2 名额账：reserved_count 与占名额预约数应相等，且不超容量
SELECT s.id, s.capacity, s.reserved_count,
       COUNT(r.id) AS holding
FROM slot s
LEFT JOIN reservation r ON r.slot_id = s.id
 AND r.status IN ('PENDING_PAYMENT','CONFIRMED','CANCELLATION_REVIEW','CHECKED_IN','IN_SERVICE','COMPLETED')
WHERE s.store_id = :store AND DATE(s.start_at) = :date
GROUP BY s.id, s.capacity, s.reserved_count
HAVING holding <> s.reserved_count OR s.reserved_count > s.capacity;

-- I3 同一宠物同一时段重复有效
SELECT slot_id, pet_id, COUNT(*) AS n
FROM reservation
WHERE status IN ('PENDING_PAYMENT','CONFIRMED','CANCELLATION_REVIEW','CHECKED_IN','IN_SERVICE','COMPLETED')
GROUP BY slot_id, pet_id HAVING n > 1;

-- 状态与时间戳不合法（示例：已确认未付款、已到店无核销时间、已完成未开始、未到店无原因）
SELECT id, status FROM reservation
WHERE (status='CONFIRMED'   AND paid_at IS NULL)
   OR (status='CHECKED_IN'  AND checked_in_at IS NULL)
   OR (status='IN_SERVICE'  AND started_at IS NULL)
   OR (status='COMPLETED'   AND (started_at IS NULL OR completed_at IS NULL))
   OR (status='NO_SHOW'     AND (no_show_at IS NULL OR no_show_reason IS NULL OR no_show_reason=''));

-- R3 取消政策：自动受理必须距开始 >=2h；<2h 必须有取消申请且经客服批准
SELECT r.id, r.cancel_type, r.cancelled_at, s.start_at,
       TIMESTAMPDIFF(MINUTE, r.cancelled_at, s.start_at) AS minutes_before_start
FROM reservation r JOIN slot s ON s.id=r.slot_id
WHERE r.status='CANCELLED' AND r.cancel_type='AUTO_BEFORE_2H'
  AND TIMESTAMPDIFF(MINUTE, r.cancelled_at, s.start_at) < 120;

SELECT r.id FROM reservation r
JOIN cancel_request c ON c.reservation_id=r.id
JOIN slot s ON s.id=r.slot_id
WHERE r.status='CANCELLED' AND r.cancel_type='CS_APPROVED'
  AND TIMESTAMPDIFF(MINUTE, r.cancelled_at, s.start_at) >= 120;

-- R3.3/R5.1 待审取消期间被核销（或状态不一致）
SELECT r.id, r.status, c.status AS cancel_status
FROM reservation r JOIN cancel_request c ON c.reservation_id=r.id
WHERE c.status='PENDING' AND r.status IN ('CHECKED_IN','IN_SERVICE','COMPLETED');

-- R4.2/R4.4 取消后名额释放与退款一致性
SELECT r.id, r.status, r.seat_released, rf.id AS refund_id, rf.status AS refund_status
FROM reservation r LEFT JOIN refund rf ON rf.reservation_id=r.id
WHERE r.status IN ('CANCELLED') AND (r.seat_released=0 OR rf.id IS NULL);

-- R2.6 关闭后支付：异常记录必须存在且预约不得恢复为有效状态
SELECT r.id, r.status, pa.id AS anomaly_id, rf.status AS refund_status
FROM reservation r
LEFT JOIN payment_anomaly pa ON pa.reservation_id=r.id
LEFT JOIN refund rf ON rf.reservation_id=r.id
WHERE r.status IN ('EXPIRED','CANCELLED') AND pa.id IS NULL;   -- 需人工结合支付通知确认

-- R2.6 反向：异常支付对应的预约被恢复（违规）
SELECT r.id, r.status FROM reservation r
JOIN payment_anomaly pa ON pa.reservation_id=r.id
WHERE r.status IN ('CONFIRMED','CHECKED_IN','IN_SERVICE','COMPLETED','PENDING_PAYMENT');

-- I8 重复核销 / 重复服务 / 重复退款（若未建唯一键则会暴露）
SELECT reservation_id, COUNT(*) n FROM checkin_record GROUP BY reservation_id HAVING n>1;
SELECT reservation_id, COUNT(*) n FROM service_record GROUP BY reservation_id HAVING n>1;
SELECT reservation_id, COUNT(*) n FROM refund GROUP BY reservation_id HAVING n>1;

-- I13 同一预约多条支付成功（应最多一条；允许失败重试产生多条其他流水）
SELECT reservation_id, COUNT(*) n FROM payment WHERE status='SUCCESS' GROUP BY reservation_id HAVING n>1;

-- I14 同一预约多条待审核取消申请（同一时刻应最多一条；历史申请可多条）
SELECT reservation_id, COUNT(*) n FROM cancel_request WHERE status='PENDING' GROUP BY reservation_id HAVING n>1;

-- I4/I6/I7 金额四口径
SELECT
  (SELECT COALESCE(SUM(p.amount_cents),0) FROM payment p JOIN reservation r ON r.id=p.reservation_id
    JOIN slot s ON s.id=r.slot_id
    WHERE s.store_id=:store AND DATE(s.start_at)=:date AND p.status='SUCCESS') AS paid,
  (SELECT COALESCE(SUM(rf.amount_cents),0) FROM refund rf JOIN reservation r ON r.id=rf.reservation_id
    JOIN slot s ON s.id=r.slot_id
    WHERE s.store_id=:store AND DATE(s.start_at)=:date AND rf.status='SUCCESS') AS refunded,
  (SELECT COALESCE(SUM(r.price_cents),0) FROM reservation r JOIN slot s ON s.id=r.slot_id
    WHERE s.store_id=:store AND DATE(s.start_at)=:date AND r.status='COMPLETED') AS completed_service,
  (SELECT COUNT(*) FROM refund rf JOIN reservation r ON r.id=rf.reservation_id JOIN slot s ON s.id=r.slot_id
    WHERE s.store_id=:store AND DATE(s.start_at)=:date AND rf.status IN ('PENDING','PROCESSING','FAILED')) AS pending_refund_cnt;
```

### 步骤 3：与日清接口交叉核对（有接口时必做）

调用 `GET /api/manager/daily-close?date=<date>`，将 `totals.paidCents/refundedCents/completedCents/pendingRefundCents` 与步骤 2 结果逐项比较；差异 >0 即违规（`MONEY_MISMATCH`）。记录接口原始响应路径。

### 步骤 4：输出（固定 JSON，`docs/evidence/skill/slot-guard-<date>-<sha>.json`）

```json
{"skill":"slot-rule-guard","version":"0.1.0","ts":"2026-11-12T10:00:00+08:00",
 "date":"2026-11-12","storeId":1,"commit":"<sha>","pass":true,
 "checks":[{"id":"I1","pass":true,"violations":[]},
           {"id":"I3","pass":true,"violations":[]}],
 "commands":["mysql ... < checks.sql","GET /api/manager/daily-close?date=..."],
 "artifacts":["docs/evidence/skill/slot-guard-2026-11-12-<sha>.json"]}
```

## 失败处理

1. 任一检查不通过 → 生成缺陷记录（模板 `.agent/templates/defect-record.md`），标题带规则编号，如 `BUG-xx I1 名额账偏差`。
2. 交回 **Dev 会话**修复（禁止本 Skill 直接改数据/改代码），修复提交后由 **Test 会话**用相同 SQL 复检。
3. 复检输出与首次输出一并保存，证明“修复前后”；不得删除首次失败输出。
4. 若差异来自设计口径（例如未到店是否占名额），停止并请学生确认，写入假设清单后再改断言。

## 完成标准

- 全部检查通过，且日清接口与 SQL 四金额一致，输出 JSON 的 `pass=true`。
- 有缺陷时：缺陷已关闭且有复检证据，不复用旧输出。
- 输出文件已提交到仓库并在 Test/Acceptance 报告中引用。

## 已知局限（不得夸大）

- 只检查数据库中已存在的数据，无法发现“完全未实现”的分支（需题面逐项验收）。
- 依赖实现记录 `cancelled_at`、`payment_anomaly` 等字段；缺字段时报告 BLOCKED 而非猜测。
- 不判断页面交互与视频质量，那属于 acceptance-audit。
- 只读审计，不模拟并发；并发胜负由 C1–C10 测试覆盖。
