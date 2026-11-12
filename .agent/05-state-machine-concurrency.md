# 05 状态机、并发与时间边界

本文件是 R2/R3/R4/R5/R6 的实现级说明。**任何状态迁移都要满足：来源状态合法 + 操作者合法 + 副作用在同一事务 + 幂等**。

## 1. 状态迁移表（reservation）

| 来源 | 事件 | 目标 | 守卫条件 | 同事务副作用 |
|---|---|---|---|---|
| — | 创建预约 | PENDING_PAYMENT | 时段未开始、容量未满、同宠物同时段无有效预约 | 时段 `reserved_count+1` |
| PENDING_PAYMENT | 5 分钟超时 | EXPIRED | `pay_expire_at<=now` | 释放名额（一次） |
| PENDING_PAYMENT | 用户取消 | CANCELLED | 本人 | 释放名额；`cancel_type=USER_UNPAID`；无退款 |
| PENDING_PAYMENT | 支付成功回调 | CONFIRMED | 回调验签通过、金额一致、未过期 | 支付单 SUCCESS；`paid_at`；不重复记收款 |
| CONFIRMED | 取消（≥2h） | CANCELLED | 距 `start_at` ≥2h | 释放名额；`cancel_type=AUTO_BEFORE_2H`；创建全额退款 |
| CONFIRMED | 取消（<2h，有原因） | CANCELLATION_REVIEW | 有原因、无待处理申请 | 创建取消申请；**名额保持** |
| CANCELLATION_REVIEW | 客服批准 | CANCELLED | 待处理 | 释放名额一次；创建退款；`cancel_type=CS_APPROVED` |
| CANCELLATION_REVIEW | 客服拒绝 | CONFIRMED | 待处理 | 名额保持；记录决定 |
| CONFIRMED | 前台核销 | CHECKED_IN | 本店、无待处理取消 | 核销记录（唯一） |
| CHECKED_IN | 开始服务 | IN_SERVICE | 本店服务人员 | 服务开始时间 |
| IN_SERVICE | 完成服务 | COMPLETED | 本店服务人员 | 完成时间；收入确认（唯一） |
| CONFIRMED | 时段结束+店长标记 | NO_SHOW | 时段已结束、有原因、未核销 | 记录未到店；不退款 |
| EXPIRED / CANCELLED | 支付成功回调 | 不变 | 回调验签通过 | 异常支付记录 + 退款（`LATE_PAYMENT`），**恢复预约 = 违规** |
| CHECKED_IN / IN_SERVICE / COMPLETED | 取消 | 拒绝 | — | 无副作用（R6.2） |

非法迁移统一返回 `RESERVATION_STATE_INVALID`；**不得用“先查状态再 UPDATE”的无锁写法**。

## 2. 时间边界与教学时钟

- 全部时间判断走同一 `Clock` 接口：`Now()`。业务代码禁止直接 `time.Now()`（除 Clock 实现本身）。
- 教学时钟支持：固定时间、偏移量、手动推进（测试用）；配置 `TEACHING_CLOCK_MODE=fixed|offset|manual`、`TEACHING_CLOCK_BASE`。
- 阈值口径（固定不变，测试与演示一致）：
  - 占位时长：`pay_expire_at = created_at + 5m`（`<5m` 仍有效，`>=5m` 过期）。
  - 免费取消边界：`start_at - now >= 2h` 自动受理；`<2h` 转审核；边界值 `=2h` 归“自动受理”（测试固定此口径）。
  - 未到店：仅 `now >= end_at` 后可由店长标记。
  - 时段可预约：仅 `now < start_at`。
- 时钟推进必须有审计：每次手动推进写 `clock_advance_log`（演示可用，测试断言用），避免“演示时钟”和“真实规则”不一致。

## 3. 并发设计（必须给出可重复验证）

### 3.0 本项目的数据库决定：SQLite（2026-11-12 拍板）

本项目选择 SQLite（零部署），`docs/assumptions.md` 记录替代方案；因此**没有 `SELECT ... FOR UPDATE`**，用以下组合保证正确性，并在 `docs/coverage-report.md` 写明：

1. **单写连接**：写事务使用独立连接池并设 `SetMaxOpenConns(1)`，避免 `SQLITE_BUSY` 竞争；或统一 `PRAGMA busy_timeout=5000`。
2. **`BEGIN IMMEDIATE`**：需要“先读后写”的事务一开始就拿写锁，避免读锁升级冲突。
3. **条件更新 + 影响行数**：所有状态迁移、名额增减都用 `UPDATE ... WHERE id=? AND status IN (...) AND seat_released=0`，以 `RowsAffected=1` 判定成功。
4. **唯一/部分唯一索引**：同宠物同时段有效预约、同预约唯一成功支付、同预约唯一待审核申请、核销/服务/退款唯一，全部由索引兜底。
5. **对账兜底**：`slot-rule-guard` 的 I1–I14 检查必须全部通过；并发测试断言最终一致状态与记录数。
6. **WAL + 外键开启**：`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`，测试与演示使用相同参数。

> 局限（必须写进验收报告）：SQLite 没有真正的多进程行锁，并发正确性来自“条件更新 + 唯一约束 + 单写连接”，不是数据库行锁；不把它说成与 MySQL 行锁等价。

### 3.1 名额（R1.3/R2.1/R2.2/R2.3/R4.2）

推荐：**时段行锁 + 计数**。所有会改变名额的事务，第一步 `SELECT ... FOR UPDATE` 锁定 `slot` 行，再操作预约：

```sql
-- 占位（同一事务）
SELECT id, capacity, reserved_count FROM slot WHERE id=? FOR UPDATE;
-- 校验 reserved_count < capacity 且 now < start_at
UPDATE slot SET reserved_count = reserved_count + 1 WHERE id=?;
INSERT INTO reservation(...);
```

释放同样先锁 slot，再条件更新预约（保证只释放一次）：

```sql
UPDATE reservation
   SET status='CANCELLED', cancel_type=?, seat_released=1
 WHERE id=? AND status IN ('PENDING_PAYMENT','CONFIRMED')
   AND seat_released=0;
-- affected rows=1 时才 UPDATE slot SET reserved_count=reserved_count-1
```

对账兜底：后台/日清脚本校验 I1，发现偏差即缺陷，不允许“差不多”。

### 3.2 唯一有效预约（R1.2）
数据库唯一键 `uk_slot_pet_active(slot_id,pet_id,active_marker)`；应用层捕获唯一冲突 → 返回 `DUPLICATE_ACTIVE_RESERVATION`，不要先查后插。

### 3.3 核销 vs 取消并发（R6.1/R3.3/R6.2）
两个操作都以**同一顺序**加锁：先预约行 `SELECT ... FOR UPDATE`，再查取消申请。然后：

- 核销：`UPDATE reservation SET status='CHECKED_IN', checked_in_at=now WHERE id=? AND status='CONFIRMED'`，并要求当时无 `PENDING` 取消申请（同事务内查询，锁住申请行）。
- 取消（<2h 提交申请）：要求 `status='CONFIRMED'` 且无核销记录，插入申请。
- 结果只有两种：核销成功且取消失败（`RESERVATION_STATE_INVALID`/`CHECKIN_...`），或取消失败后核销成功；**不可能两者都写成功**。
- 推荐用 `status` 条件更新决定胜负，并在失败分支返回明确业务码；所有副作用（核销记录/取消申请/名额）与状态更新同事务。

### 3.4 重复请求
- 写接口 `Idempotency-Key` 唯一约束：先插入占位行，重复 key 读回首次结果；同 key 不同载荷 → `IDEMPOTENCY_CONFLICT`。
- 支付回调 `channel_txn_id` 唯一；先插通知，唯一冲突即重复，返回 `DUPLICATE_IGNORED`。
- 核销/完成/退款创建都有唯一键，重复请求命中唯一冲突后返回首次结果，不重复记收入。

### 3.5 超时清理（R2.2）
两条路径都要有，且幂等：
1. 惰性：创建预约/查询时段时，先执行一次过期扫描（`WHERE status='PENDING_PAYMENT' AND pay_expire_at<=now`）。
2. 定时：每分钟扫描一次过期预约并释放名额（后台 goroutine + 单实例锁/DB 条件更新；多实例部署时用 `SELECT ... FOR UPDATE SKIP LOCKED` 或条件更新）。
- 扫描失败要告警日志，不得静默吞错。

### 3.6 退款与支付渠道
- 退款状态独立：`PENDING → PROCESSING → SUCCESS / FAILED`；超时/网络错误记 `FAILED`（`CHANNEL_TIMEOUT`），允许重试；重试只允许 `FAILED/PENDING`，每次 `retry_count+1` 并留痕。
- **退款失败不得改变预约状态**：取消已生效就保持不可用（R4.4），禁止“退款失败 → 恢复已确认/可核销”。
- 关闭后支付（R2.6）：回调在事务内创建 `payment_anomaly` + `refund(LATE_PAYMENT)`，预约状态不迁移；若退款也失败，进入客服失败列表。

### 3.7 支付失败重试与取消重提（2026-11-12 决策）

- **R2.7 支付失败重试**：失败不改预约状态（仍为待支付）；重试生成新 `payment` 流水与新 `order_no`；同一预约只允许一条 `SUCCESS`（部分唯一索引/生成列）；5 分钟窗口过后成功回调走关闭后支付路径（异常 + 退款，不恢复预约）。
- **D3 取消拒绝后重提**：拒绝把预约恢复为 `CONFIRMED`；用户可再次提交新申请，但同一时刻只允许一条 `PENDING`（部分唯一索引）；历史申请保留，客服可查看前次拒绝原因。
- 两条规则都不改变名额口径：失败支付不释放名额（占位窗口内），拒绝不释放名额，批准/过期/未付款取消才释放且只释放一次。

### 3.8 未到店口径（2026-11-12 默认确认）

仅时段结束后（`now >= end_at`）可由店长标记 `NO_SHOW` 并必填原因；同一事务把 `seat_released` 0→1、名额减一次（NO_SHOW 不再计入占名额集合，I1 对账因此一致），**不创建退款、不可撤销**；应在日清中单列未到店计数与金额。

## 4. 必须编写的并发/时序测试
| 编号 | 用例 | 通过标准 |
|---|---|---|
| C1 | 20 个用户并发抢 3 个名额 | 成功 3 条、其余 `SLOT_FULL`，`reserved_count=3` |
| C2 | 同一宠物同时段并发预约 ×20 | 成功 1 条、其余 `DUPLICATE_ACTIVE_RESERVATION`，库中有效预约 1 条 |
| C3 | 超时与支付成功并发 | 只有一种终态：要么 EXPIRED 且回调走异常退款，要么 CONFIRMED；不会既确认又过期 |
| C4 | 核销与取消审核提交并发 ×10 轮 | 每轮唯一合法结果，无双重副作用，金额不变 |
| C5 | 重复核销/重复完成 ×20 | 只有一条核销/服务记录，收入只记一次 |
| C6 | 取消重复点击/重复批准 ×20 | 只释放一次名额、只生成一张退款 |
| C7 | 重复支付通知 ×10 | 只确认一次、只记一次收款 |
| C8 | 关闭后支付回调 | 预约状态不变、异常单 1 条、退款 1 条 |
| C9 | 退款失败后重试成功 | 状态 SUCCESS、金额正确、预约仍不可用 |
| C10 | `-race` 下全量跑 C1–C9 | 无数据竞争 |
| C11 | 支付失败后重试成功 ×20；并发重复支付成功 | 仅一条 SUCCESS、一次收款、预约已确认 |
| C12 | 取消被拒后可重提；待审中重复提交 | 拒绝后恢复 CONFIRMED；待审中重复提交返回 `CANCEL_ALREADY_PENDING`；历史申请保留 |

运行方式见 06；测试必须引用规则编号（R1.3/R6.1 等），便于验收追溯。
