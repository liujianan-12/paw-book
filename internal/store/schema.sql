-- paw-book 数据库结构（SQLite）
-- 设计原则：可重复执行（IF NOT EXISTS）；状态为字符串常量；金额用分（INTEGER）。

CREATE TABLE IF NOT EXISTS stores (
    id    INTEGER PRIMARY KEY,
    name  TEXT NOT NULL
);

-- 员工角色：manager 店长 / front 前台 / staff 服务人员 / customerservice 客服
CREATE TABLE IF NOT EXISTS employees (
    id       INTEGER PRIMARY KEY,
    store_id INTEGER NOT NULL REFERENCES stores(id),
    name     TEXT NOT NULL,
    role     TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
    id   INTEGER PRIMARY KEY,
    name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS pets (
    id      INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    name    TEXT NOT NULL,
    breed   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS services (
    id          INTEGER PRIMARY KEY,
    store_id    INTEGER NOT NULL REFERENCES stores(id),
    name        TEXT NOT NULL,
    price_cents INTEGER NOT NULL,
    active      INTEGER NOT NULL DEFAULT 1
);

-- 可预约时段；capacity 固定 3
CREATE TABLE IF NOT EXISTS slots (
    id         INTEGER PRIMARY KEY,
    store_id   INTEGER NOT NULL REFERENCES stores(id),
    service_id INTEGER NOT NULL REFERENCES services(id),
    start_at   TEXT NOT NULL,  -- RFC3339
    end_at     TEXT NOT NULL,  -- RFC3339
    capacity   INTEGER NOT NULL DEFAULT 3
);

-- 预约主表；状态见 internal/model 常量
CREATE TABLE IF NOT EXISTS reservations (
    id              INTEGER PRIMARY KEY,
    slot_id         INTEGER NOT NULL REFERENCES slots(id),
    store_id        INTEGER NOT NULL REFERENCES stores(id),
    user_id         INTEGER NOT NULL REFERENCES users(id),
    pet_id          INTEGER NOT NULL REFERENCES pets(id),
    status          TEXT NOT NULL,
    amount_cents    INTEGER NOT NULL,
    pay_deadline    TEXT NOT NULL,             -- 未付款占位截止时间
    cancel_reason   TEXT,
    no_show_reason  TEXT,
    cancel_review_id INTEGER,
    checked_in_at   TEXT,
    service_started_at TEXT,
    service_finished_at TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

-- 有效预约唯一约束：同一宠物同一时段最多一个有效预约（并发抢位的最后防线）
CREATE UNIQUE INDEX IF NOT EXISTS idx_res_active_slot_pet
    ON reservations(slot_id, pet_id)
    WHERE status IN ('PENDING_PAYMENT','CONFIRMED','PENDING_REVIEW','CHECKED_IN','IN_SERVICE');

CREATE TABLE IF NOT EXISTS payments (
    id              INTEGER PRIMARY KEY,
    reservation_id  INTEGER NOT NULL REFERENCES reservations(id),
    out_trade_no    TEXT NOT NULL UNIQUE,
    amount_cents    INTEGER NOT NULL,
    status          TEXT NOT NULL,   -- CREATED / SUCCESS / FAILED / TIMEOUT
    created_at      TEXT NOT NULL,
    notified_at     TEXT
);

-- 退款单独立于预约状态（R14/I6）
CREATE TABLE IF NOT EXISTS refunds (
    id             INTEGER PRIMARY KEY,
    reservation_id INTEGER NOT NULL REFERENCES reservations(id),
    payment_id     INTEGER NOT NULL REFERENCES payments(id),
    amount_cents   INTEGER NOT NULL,
    status         TEXT NOT NULL,   -- PENDING / PROCESSING / SUCCESS / FAILED
    fail_reason    TEXT,
    retry_count    INTEGER NOT NULL DEFAULT 0,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);

-- 临近开始的取消审核（R11/R12）
CREATE TABLE IF NOT EXISTS cancel_reviews (
    id             INTEGER PRIMARY KEY,
    reservation_id INTEGER NOT NULL REFERENCES reservations(id),
    user_id        INTEGER NOT NULL REFERENCES users(id),
    reason         TEXT NOT NULL,
    status         TEXT NOT NULL,   -- PENDING / APPROVED / REJECTED
    decided_by     INTEGER REFERENCES employees(id),
    decided_at     TEXT,
    created_at     TEXT NOT NULL
);

-- 已关闭预约收到支付成功的异常登记（R8）
CREATE TABLE IF NOT EXISTS payment_exceptions (
    id             INTEGER PRIMARY KEY,
    reservation_id INTEGER NOT NULL REFERENCES reservations(id),
    payment_id     INTEGER NOT NULL REFERENCES payments(id),
    refund_id      INTEGER REFERENCES refunds(id),
    amount_cents   INTEGER NOT NULL,
    reason         TEXT NOT NULL,
    created_at     TEXT NOT NULL
);

-- 审计日志：所有状态变更留痕
CREATE TABLE IF NOT EXISTS audit_logs (
    id         INTEGER PRIMARY KEY,
    actor_type TEXT NOT NULL,   -- user / employee / system
    actor_id   INTEGER,
    action     TEXT NOT NULL,
    target     TEXT NOT NULL,
    detail     TEXT,
    created_at TEXT NOT NULL
);
