# 02 领域模型与数据库

目标：**名额、状态、金额、退款四本账在任何并发和重试下都能对上**。数据库类型自选（推荐 MySQL 8 / PostgreSQL 15），本文件以 MySQL 8 为例，使用 InnoDB + 事务 + 行锁 + 唯一约束；SQLite 需注意没有 `SELECT ... FOR UPDATE` 的替代方案（改用单写连接 + 乐观版本）。

## 1. 实体与表

| 表 | 作用 | 关键约束 |
|---|---|---|
| `store` | 预置 1 门店 | 主键 |
| `staff` | 预置员工，含角色与门店 | `role IN (manager, reception, server, cs)`，`store_id` 必填 |
| `app_user` / `pet` | 预置用户与宠物 | `pet.user_id` 归属校验 |
| `service_item` | 洗护项目，`price_cents=9900` | 价格快照来源 |
| `slot` | 时段：`store_id, item_id, start_at, end_at, capacity=3, reserved_count` | `CHECK(reserved_count BETWEEN 0 AND capacity)` |
| `reservation` | 预约主记录 | 唯一键防同宠物同时段重复有效；`seat_released` 防重复释放 |
| `payment` | 支付单，`order_no` 唯一；同一预约可多次失败尝试，最多一条成功 | 金额=预约价快照 |
| `payment_notification` | 渠道回调原始流水，`channel_txn_id` 唯一 | 幂等第一道闸 |
| `refund` | 退款单，`payment_id` 唯一 | 状态独立，可重试 |
| `cancel_request` | 取消申请，`reservation_id` **仅待审核时唯一**；历史可多条 | 待审核期间保持名额 |
| `checkin_record` / `service_record` | 核销、服务记录，`reservation_id` 唯一 | 防重复记收入 |
| `payment_anomaly` | 关闭后支付成功等异常登记 | 关联退款单 |
| `idempotency_key` | 写接口幂等（`Idempotency-Key`） | key 唯一 |
| `operation_log` | 谁在何时对哪条记录做了什么 | 审计与证据 |

## 2. DDL 关键片段（可直接改用于 MySQL 8）

```sql
CREATE TABLE slot (
  id            BIGINT PRIMARY KEY AUTO_INCREMENT,
  store_id      BIGINT NOT NULL,
  item_id       BIGINT NOT NULL,
  start_at      DATETIME(3) NOT NULL,
  end_at        DATETIME(3) NOT NULL,
  capacity      INT NOT NULL DEFAULT 3,
  reserved_count INT NOT NULL DEFAULT 0,
  created_by    BIGINT NOT NULL,
  created_at    DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  CONSTRAINT chk_slot_time CHECK (end_at > start_at),
  CONSTRAINT chk_slot_cap  CHECK (capacity > 0 AND reserved_count BETWEEN 0 AND capacity),
  KEY idx_slot_store_day (store_id, start_at)
) ENGINE=InnoDB;

CREATE TABLE reservation (
  id             BIGINT PRIMARY KEY AUTO_INCREMENT,
  store_id       BIGINT NOT NULL,
  slot_id        BIGINT NOT NULL,
  user_id        BIGINT NOT NULL,
  pet_id         BIGINT NOT NULL,
  item_id        BIGINT NOT NULL,
  status         VARCHAR(32) NOT NULL,
  price_cents    INT NOT NULL,
  pay_expire_at  DATETIME(3) NOT NULL,
  paid_at        DATETIME(3) NULL,
  checked_in_at  DATETIME(3) NULL,
  started_at     DATETIME(3) NULL,
  completed_at   DATETIME(3) NULL,
  no_show_at     DATETIME(3) NULL,
  no_show_reason VARCHAR(255) NULL,
  cancelled_at   DATETIME(3) NULL,          -- 取消生效时间（审计/2h边界校验用）
  cancel_type    VARCHAR(32) NULL,          -- USER_UNPAID / AUTO_BEFORE_2H / CS_APPROVED / EXPIRED
  seat_released  TINYINT NOT NULL DEFAULT 0,
  version        INT NOT NULL DEFAULT 0,    -- 乐观锁兜底（可与行锁二选一）
  created_at     DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  -- 有效预约唯一：生效为 1，非生效为 NULL（MySQL 唯一索引允许多个 NULL）
  active_marker  TINYINT GENERATED ALWAYS AS (
      CASE WHEN status IN ('PENDING_PAYMENT','CONFIRMED','CANCELLATION_REVIEW',
                           'CHECKED_IN','IN_SERVICE','COMPLETED') THEN 1 END) STORED,
  UNIQUE KEY uk_slot_pet_active (slot_id, pet_id, active_marker),
  KEY idx_res_slot (slot_id),
  KEY idx_res_user (user_id),
  KEY idx_res_status (status, pay_expire_at)
) ENGINE=InnoDB;

CREATE TABLE payment (
  id            BIGINT PRIMARY KEY AUTO_INCREMENT,
  reservation_id BIGINT NOT NULL,
  order_no      VARCHAR(64) NOT NULL,
  amount_cents  INT NOT NULL,
  channel       VARCHAR(16) NOT NULL DEFAULT 'mock',
  status        VARCHAR(16) NOT NULL,       -- INIT / SUCCESS / FAILED
  provider_txn_id VARCHAR(64) NULL,
  paid_at       DATETIME(3) NULL,
  success_marker TINYINT GENERATED ALWAYS AS (CASE WHEN status='SUCCESS' THEN 1 END) STORED,
  UNIQUE KEY uk_payment_order (order_no),
  -- 允许同一预约多次支付尝试（每次一条流水），但最多一条成功
  -- SQLite：CREATE UNIQUE INDEX uk_payment_success ON payment(reservation_id) WHERE status='SUCCESS';
  UNIQUE KEY uk_payment_success (reservation_id, success_marker)
) ENGINE=InnoDB;

CREATE TABLE payment_notification (
  id              BIGINT PRIMARY KEY AUTO_INCREMENT,
  channel_txn_id  VARCHAR(64) NOT NULL,
  order_no        VARCHAR(64) NOT NULL,
  payload_hash    CHAR(64) NOT NULL,
  result          VARCHAR(32) NOT NULL,     -- CONFIRMED / DUPLICATE_IGNORED / ANOMALY_REFUND / REJECTED
  received_at     DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_notify_txn (channel_txn_id)
) ENGINE=InnoDB;

CREATE TABLE refund (
  id            BIGINT PRIMARY KEY AUTO_INCREMENT,
  payment_id    BIGINT NOT NULL,
  reservation_id BIGINT NOT NULL,
  amount_cents  INT NOT NULL,
  reason        VARCHAR(32) NOT NULL,       -- AUTO_CANCEL / CS_APPROVED / LATE_PAYMENT
  status        VARCHAR(16) NOT NULL,       -- PENDING / PROCESSING / SUCCESS / FAILED
  retry_count   INT NOT NULL DEFAULT 0,
  last_error    VARCHAR(255) NULL,
  provider_refund_id VARCHAR(64) NULL,
  created_at    DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at    DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_refund_payment (payment_id),
  UNIQUE KEY uk_refund_res (reservation_id)
) ENGINE=InnoDB;

CREATE TABLE cancel_request (
  id             BIGINT PRIMARY KEY AUTO_INCREMENT,
  reservation_id BIGINT NOT NULL,
  reason         VARCHAR(255) NOT NULL,
  status         VARCHAR(16) NOT NULL DEFAULT 'PENDING', -- PENDING / APPROVED / REJECTED
  requested_at   DATETIME(3) NOT NULL,
  decided_by     BIGINT NULL,
  decided_at     DATETIME(3) NULL,
  decision_note  VARCHAR(255) NULL,
  -- 拒绝后可再次提交：只约束“待审核”唯一，历史申请保留
  pending_marker TINYINT GENERATED ALWAYS AS (CASE WHEN status='PENDING' THEN 1 END) STORED,
  UNIQUE KEY uk_cancel_pending (reservation_id, pending_marker)
) ENGINE=InnoDB;

CREATE TABLE payment_anomaly (
  id             BIGINT PRIMARY KEY AUTO_INCREMENT,
  reservation_id BIGINT NOT NULL,
  payment_id     BIGINT NOT NULL,
  kind           VARCHAR(32) NOT NULL,   -- LATE_PAYMENT（关闭后支付）等
  detail         JSON NULL,
  created_at     DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_anomaly_payment (payment_id)
) ENGINE=InnoDB;

CREATE TABLE idempotency_key (
  id             BIGINT PRIMARY KEY AUTO_INCREMENT,
  scope          VARCHAR(64) NOT NULL,   -- 接口/动作标识
  key_value      VARCHAR(64) NOT NULL,
  request_hash   CHAR(64) NOT NULL,
  response_code  INT NOT NULL,
  response_body  JSON NOT NULL,
  created_at     DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_scope_key (scope, key_value)
) ENGINE=InnoDB;

-- 其余审计/核销/服务表按同一规则建：
-- checkin_record(reservation_id UNIQUE, staff_id, checked_at)
-- service_record(reservation_id UNIQUE, started_at, completed_at, started_by, completed_by)
-- operation_log(actor_id, actor_role, store_id, action, target_type, target_id, result, request_id, created_at)
```


> 若数据库不支持生成列，用应用层唯一表 `active_reservation(slot_id, pet_id)` 或 `slot_seat(slot_id, seat_no)` 保证同一宠物同一时段唯一，并说明理由。

## 3. 状态枚举（与 05 完全一致，代码常量唯一来源）

- `reservation.status`：`PENDING_PAYMENT`、`CONFIRMED`、`CANCELLATION_REVIEW`、`CHECKED_IN`、`IN_SERVICE`、`COMPLETED`、`CANCELLED`、`EXPIRED`、`NO_SHOW`
- `refund.status`：`PENDING`、`PROCESSING`、`SUCCESS`、`FAILED`
- `cancel_request.status`：`PENDING`、`APPROVED`、`REJECTED`
- `payment.status`：`INIT`、`SUCCESS`、`FAILED`

## 4. 必须成立的数据不变量（I 系列，测试与 Skill 都检查）

| 编号 | 不变量 | 检查方式 |
|---|---|---|
| I1 | `slot.reserved_count` = 该时段处于占名额状态的预约数（待支付未超时、已确认、取消审核中、已到店、服务中、已完成）；**未到店(NO_SHOW) 标记时释放一次名额并置 `seat_released=1`，故不计入占名额集合**（不退款） | 对账 SQL |
| I2 | `0 ≤ reserved_count ≤ capacity` | 约束/对账 |
| I3 | 同一 `(slot_id, pet_id)` 最多一条有效预约 | 唯一索引 + 对账 |
| I4 | 每条 `refund` 对应一条 `payment`，退款额 ≤ 支付额 | 外键 + 对账 |
| I5 | 取消/过期只释放一次名额（`seat_released` 0→1 仅一次） | 条件更新影响行数 |
| I6 | 已收款 = Σ payment.SUCCESS，已退款 = Σ refund.SUCCESS，未退款 = 已收款 − 已退款 | 日清对账 |
| I7 | 已完成服务金额 = Σ 已完成预约的 `price_cents`，与已收款分开 | 日清对账 |
| I8 | 每条预约最多一条核销、一条服务、一条**待审核**取消申请、一条退款；取消申请历史可多条 | 唯一键/部分唯一索引 |
| I13 | 同一预约最多一条支付成功记录（允许多次失败尝试、多条流水） | 部分唯一索引 + 对账 |
| I14 | 支付失败重试、取消拒绝后重提均保留全部历史流水，不覆盖旧记录 | 对账 |
| I9 | 已到店/服务中/已完成/已取消/已过期/未到店的预约不存在“待处理取消申请” | 状态一致性查询 |
| I10 | 关闭后支付：预约状态不变，必然存在 `payment_anomaly` 与对应退款 | 对账 |
| I11 | 重复请求（同一 Idempotency-Key）返回同一结果，不产生第二条业务记录 | 幂等表 + 测试 |
| I12 | 所有金额为整数分，无浮点字段 | schema 检查 |

## 5. 金额口径（写进日清接口与页面，不许含糊）

- **已收款**：当日时段内预约的 `payment.status=SUCCESS` 金额合计。
- **已退款**：对应 `refund.status=SUCCESS` 合计。
- **未退款**：已收款 − 已退款（可能包含已完成服务、未到店保留款、退款处理中金额）。
- **已完成服务金额**：`reservation.status=COMPLETED` 的 `price_cents` 合计（收入确认口径，不等于已收款）。
- **待处理退款**：`refund.status IN (PENDING, PROCESSING, FAILED)` 的金额合计与笔数。
- 所有汇总必须能下钻到单据 ID；不得只返回一个数字。

## 6. 迁移与初始化

- `migrations/` 放顺序 SQL（`001_init.sql`、`002_seed.sql`…），可重复执行的种子数据（预置门店/项目/3 时段/用户/宠物/员工账号）。
- 启动时**不自动改表**；README 写清 `migrate` 命令与初始化步骤。
- 任何 DDL 变更 = 新迁移文件 + 回退说明 + 在任务记录中写原因，禁止直接改已提交迁移。

## 7. SQLite 实施附录（2026-11-12 决策 D1）

本项目选择 SQLite；`docs/assumptions.md` 已记录替代方案。实施必须满足：

1. **连接**：写操作用独立连接池并 `SetMaxOpenConns(1)`；全库 `PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`。
2. **事务**：需要“先读后写”的逻辑用 `BEGIN IMMEDIATE`，避免读锁升级；事务内禁止长时间等待用户输入。
3. **条件更新**：状态迁移与名额增减必须以 `UPDATE ... WHERE ... AND status IN (...)` 的 `RowsAffected` 判定，不依赖先查后改；名额对账由 `slot-rule-guard` 的 I1 兜底。
4. **唯一索引**：SQLite 支持部分索引，用来实现本项目三处“部分唯一”：
   ```sql
   CREATE UNIQUE INDEX uk_payment_success ON payment(reservation_id) WHERE status='SUCCESS';
   CREATE UNIQUE INDEX uk_cancel_pending  ON cancel_request(reservation_id) WHERE status='PENDING';
   CREATE UNIQUE INDEX uk_res_active      ON reservation(slot_id, pet_id)
     WHERE status IN ('PENDING_PAYMENT','CONFIRMED','CANCELLATION_REVIEW','CHECKED_IN','IN_SERVICE','COMPLETED');
   ```
   （MySQL 版对应生成列 + 普通唯一键，见 §2。）
5. **过期扫描**：懒扫描 + 定时扫描并存，均为条件更新（`status='PENDING_PAYMENT' AND pay_expire_at<=now AND seat_released=0`），重复执行安全。
6. **测试**：集成测试每个用例使用 `t.TempDir()` 独立库文件，迁移由同一脚本执行；并发测试断言最终状态与记录数，不依赖睡眠。
7. **证据口径**：`docs/coverage-report.md` 写明“并发正确性由单写连接 + 条件更新 + 唯一索引保证，不是数据库行锁；未验证多进程并发”，不得声称等价 MySQL。

