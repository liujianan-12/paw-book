---
name: slot-rule-guard
description: 业务专项 Skill。检查宠物门店预约的名额账、状态机、取消政策、退款独立性与金额对账是否一致；发现违规时输出可复现证据并交回开发修复。
version: 0.1.0
owner: 学生（提交前填写姓名）
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

### 步骤 2：执行检查集（SQLite 版）

```bash
# 只读打开 SQLite（本项目 D1/D8），检查集见 skills/slot-rule-guard/checks.sql
go run ./cmd/audit -db <paw-book.db> -date 2026-11-14 -store 1 \
  -commit <sha> -out docs/evidence/skill/slot-guard-<date>-<sha>.json
```

检查覆盖：I1 名额账、I3 同宠物唯一、I9 状态/时间戳、R3.1 2 小时分界、R3.2 批准来源、R3.3 待审核销、I5/I8 取消释放与退款、I10 关闭后支付、I8 重复记录、I13 多成功支付、I14 多待审申请、I6/I7 金额勾稽。每条语句返回 0 行为通过，具体 SQL 以 `checks.sql` 为准，不在 Skill 中重复维护。

### 步骤 3：与日清接口交叉核对（有接口时必做）

调用 `GET /api/manager/daily-close?date=<date>`，将 `CollectedCents/RefundedCents/CompletedIncomeCents/RefundPendingCents` 与步骤 2 结果逐项比较；差异 >0 即违规（`MONEY_MISMATCH`）。记录接口原始响应路径。

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
