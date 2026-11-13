-- 001_init.sql  paw-book 初始化结构（SQLite 版，对应 .agent/02 表设计）
-- 全部 IF NOT EXISTS，可重复执行；时间统一 TEXT(UTC RFC3339)；金额整数分。
-- SQLite 替代方案：无 SELECT ... FOR UPDATE，改用 _txlock=immediate 单写事务；
-- 无生成列，改用部分唯一索引实现 active_marker 语义。

CREATE TABLE IF NOT EXISTS store (
    id   INTEGER PRIMARY KEY,
    name TEXT NOT NULL
);

-- 员工角色：manager 店长 / reception 前台 / server 服务人员 / cs 客服
CREATE TABLE IF NOT EXISTS staff (
    id            INTEGER PRIMARY KEY,
    store_id      INTEGER NOT NULL REFERENCES store(id),
    name          TEXT NOT NULL,
    role          TEXT NOT NULL CHECK (role IN ('manager','reception','server','cs')),
    account       TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS app_user (
    id            INTEGER PRIMARY KEY,
    name          TEXT NOT NULL,
    phone         TEXT NOT NULL,
    account       TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS pet (
    id      INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES app_user(id),
    name    TEXT NOT NULL,
    breed   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS service_item (
    id          INTEGER PRIMARY KEY,
    store_id    INTEGER NOT NULL REFERENCES store(id),
    name        TEXT NOT NULL,
    price_cents INTEGER NOT NULL,
    active      INTEGER NOT NULL DEFAULT 1
);

-- 时段：reserved_count 为名额账（I1/I2），与预约占位状态实时一致
CREATE TABLE IF NOT EXISTS slot (
    id             INTEGER PRIMARY KEY,
    store_id       INTEGER NOT NULL REFERENCES store(id),
    item_id        INTEGER NOT NULL REFERENCES service_item(id),
    start_at       TEXT NOT NULL,
    end_at         TEXT NOT NULL,
    capacity       INTEGER NOT NULL DEFAULT 3,
    reserved_count INTEGER NOT NULL DEFAULT 0,
    created_by     INTEGER NOT NULL,
    created_at     TEXT NOT NULL,
    CHECK (end_at > start_at),
    CHECK (reserved_count >= 0 AND reserved_count <= capacity)
);
CREATE INDEX IF NOT EXISTS idx_slot_store_day ON slot(store_id, start_at);

-- 预约：seat_released 保证名额只释放一次（I5）
CREATE TABLE IF NOT EXISTS reservation (
    id             INTEGER PRIMARY KEY,
    store_id       INTEGER NOT NULL,
    slot_id        INTEGER NOT NULL REFERENCES slot(id),
    user_id        INTEGER NOT NULL REFERENCES app_user(id),
    pet_id         INTEGER NOT NULL REFERENCES pet(id),
    item_id        INTEGER NOT NULL,
    status         TEXT NOT NULL CHECK (status IN
                     ('PENDING_PAYMENT','CONFIRMED','CANCELLATION_REVIEW','CHECKED_IN',
                      'IN_SERVICE','COMPLETED','CANCELLED','EXPIRED','NO_SHOW')),
    price_cents    INTEGER NOT NULL,
    pay_expire_at  TEXT NOT NULL,
    paid_at        TEXT,
    checked_in_at  TEXT,
    started_at     TEXT,
    completed_at   TEXT,
    no_show_at     TEXT,
    no_show_reason TEXT,
    cancelled_at   TEXT,
    cancel_type    TEXT,
    seat_released  INTEGER NOT NULL DEFAULT 0,
    version        INTEGER NOT NULL DEFAULT 0,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);
-- 同一宠物同一时段最多一条有效预约（I3，含 COMPLETED；SQLite 部分唯一索引等价 MySQL active_marker）
CREATE UNIQUE INDEX IF NOT EXISTS uk_slot_pet_active ON reservation(slot_id, pet_id)
    WHERE status IN ('PENDING_PAYMENT','CONFIRMED','CANCELLATION_REVIEW','CHECKED_IN','IN_SERVICE','COMPLETED');
CREATE INDEX IF NOT EXISTS idx_res_slot ON reservation(slot_id);
CREATE INDEX IF NOT EXISTS idx_res_user ON reservation(user_id);
CREATE INDEX IF NOT EXISTS idx_res_status ON reservation(status, pay_expire_at);

-- 支付单：同一预约允许多次失败尝试（每次一条新流水、新 order_no），最多一条成功（I13）
CREATE TABLE IF NOT EXISTS payment (
    id              INTEGER PRIMARY KEY,
    reservation_id  INTEGER NOT NULL REFERENCES reservation(id),
    order_no        TEXT NOT NULL UNIQUE,
    amount_cents    INTEGER NOT NULL,
    channel         TEXT NOT NULL DEFAULT 'mock',
    status          TEXT NOT NULL CHECK (status IN ('INIT','SUCCESS','FAILED')),
    provider_txn_id TEXT,
    paid_at         TEXT,
    created_at      TEXT NOT NULL
);
-- 同一预约最多一条成功支付（部分唯一索引，对应 02 §7.4）
CREATE UNIQUE INDEX IF NOT EXISTS uk_payment_success ON payment(reservation_id) WHERE status='SUCCESS';

-- 渠道回调原始流水：channel_txn_id 唯一，幂等第一道闸（I11）
CREATE TABLE IF NOT EXISTS payment_notification (
    id             INTEGER PRIMARY KEY,
    channel_txn_id TEXT NOT NULL UNIQUE,
    order_no       TEXT NOT NULL,
    result         TEXT NOT NULL,
    received_at    TEXT NOT NULL
);

-- 退款单：payment_id 与 reservation_id 各自唯一（I4/I8）
CREATE TABLE IF NOT EXISTS refund (
    id                 INTEGER PRIMARY KEY,
    refund_no          TEXT NOT NULL UNIQUE,
    payment_id         INTEGER NOT NULL UNIQUE REFERENCES payment(id),
    reservation_id     INTEGER NOT NULL UNIQUE REFERENCES reservation(id),
    amount_cents       INTEGER NOT NULL,
    reason             TEXT NOT NULL,
    status             TEXT NOT NULL CHECK (status IN ('PENDING','PROCESSING','SUCCESS','FAILED')),
    retry_count        INTEGER NOT NULL DEFAULT 0,
    last_error         TEXT,
    provider_refund_id TEXT,
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL
);

-- 取消申请：同一预约同时最多一条待审（D3-1：允许被拒后重新申请，历史保留）
CREATE TABLE IF NOT EXISTS cancel_request (
    id             INTEGER PRIMARY KEY,
    reservation_id INTEGER NOT NULL REFERENCES reservation(id),
    reason         TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','APPROVED','REJECTED')),
    requested_at   TEXT NOT NULL,
    decided_by     INTEGER,
    decided_at     TEXT,
    decision_note  TEXT
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_cancel_res_pending ON cancel_request(reservation_id) WHERE status='PENDING';

-- 核销记录：每预约唯一（I8）
CREATE TABLE IF NOT EXISTS checkin_record (
    id             INTEGER PRIMARY KEY,
    reservation_id INTEGER NOT NULL UNIQUE REFERENCES reservation(id),
    staff_id       INTEGER NOT NULL,
    checked_at     TEXT NOT NULL
);

-- 服务记录：每预约唯一（I8）
CREATE TABLE IF NOT EXISTS service_record (
    id             INTEGER PRIMARY KEY,
    reservation_id INTEGER NOT NULL UNIQUE REFERENCES reservation(id),
    started_at     TEXT,
    completed_at   TEXT,
    started_by     INTEGER,
    completed_by   INTEGER
);

-- 关闭后支付等异常登记（I10）
CREATE TABLE IF NOT EXISTS payment_anomaly (
    id             INTEGER PRIMARY KEY,
    reservation_id INTEGER NOT NULL,
    payment_id     INTEGER NOT NULL UNIQUE,
    kind           TEXT NOT NULL,
    detail         TEXT,
    created_at     TEXT NOT NULL
);

-- 写接口幂等（I11）
CREATE TABLE IF NOT EXISTS idempotency_key (
    id            INTEGER PRIMARY KEY,
    scope         TEXT NOT NULL,
    key_value     TEXT NOT NULL,
    request_hash  TEXT NOT NULL,
    response_code INTEGER NOT NULL,
    response_body TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    UNIQUE (scope, key_value)
);

-- 操作审计
CREATE TABLE IF NOT EXISTS operation_log (
    id          INTEGER PRIMARY KEY,
    actor_id    INTEGER,
    actor_role  TEXT,
    store_id    INTEGER,
    action      TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id   TEXT NOT NULL,
    result      TEXT NOT NULL,
    request_id  TEXT,
    detail      TEXT,
    created_at  TEXT NOT NULL
);

-- 教学时钟推进审计（.agent/05 §2）
CREATE TABLE IF NOT EXISTS clock_advance_log (
    id          INTEGER PRIMARY KEY,
    advanced_to TEXT NOT NULL,
    reason      TEXT,
    created_at  TEXT NOT NULL
);
