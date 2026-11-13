-- slot-rule-guard 检查集（SQLite 版，只读；每条语句返回违规行，0 行=通过）
-- 参数占位：:store :date（由 cmd/audit 注入）

-- check: I1 名额账：reserved_count 必须等于占名额状态数，且不超容量
SELECT s.id AS slot_id, s.capacity, s.reserved_count, COUNT(r.id) AS holding
FROM slot s
LEFT JOIN reservation r ON r.slot_id = s.id
 AND r.status IN ('PENDING_PAYMENT','CONFIRMED','CANCELLATION_REVIEW','CHECKED_IN','IN_SERVICE','COMPLETED')
WHERE s.store_id = :store AND date(s.start_at) = :date
GROUP BY s.id, s.capacity, s.reserved_count
HAVING holding <> s.reserved_count OR s.reserved_count > s.capacity;

-- check: I3 同一宠物同一时段重复有效
SELECT slot_id, pet_id, COUNT(*) AS n
FROM reservation
WHERE status IN ('PENDING_PAYMENT','CONFIRMED','CANCELLATION_REVIEW','CHECKED_IN','IN_SERVICE','COMPLETED')
GROUP BY slot_id, pet_id HAVING COUNT(*) > 1;

-- check: I9 状态与时间戳一致
SELECT id, status FROM reservation
WHERE (status='CONFIRMED'  AND paid_at IS NULL)
   OR (status='CHECKED_IN' AND checked_in_at IS NULL)
   OR (status='IN_SERVICE' AND started_at IS NULL)
   OR (status='COMPLETED'  AND (started_at IS NULL OR completed_at IS NULL))
   OR (status='NO_SHOW'    AND (no_show_at IS NULL OR COALESCE(no_show_reason,'')=''));

-- check: R3.1 自动受理必须距开始 >= 120 分钟
SELECT r.id, r.cancel_type, s.start_at, r.cancelled_at
FROM reservation r JOIN slot s ON s.id=r.slot_id
WHERE r.status='CANCELLED' AND r.cancel_type='AUTO_BEFORE_2H'
  AND (julianday(s.start_at) - julianday(r.cancelled_at)) * 24 * 60 < 120;

-- check: R3.2 客服批准取消必须来自待审申请
SELECT r.id FROM reservation r
LEFT JOIN cancel_request c ON c.reservation_id=r.id AND c.status='APPROVED'
WHERE r.status='CANCELLED' AND r.cancel_type='CS_APPROVED' AND c.id IS NULL;

-- check: R3.3/R5.1 待审期间被核销
SELECT r.id, r.status, c.status
FROM reservation r JOIN cancel_request c ON c.reservation_id=r.id
WHERE c.status='PENDING' AND r.status IN ('CHECKED_IN','IN_SERVICE','COMPLETED');

-- check: I5/I8 取消后必须释放名额且有退款
SELECT r.id, r.seat_released, rf.id AS refund_id
FROM reservation r LEFT JOIN refund rf ON rf.reservation_id=r.id
WHERE r.status='CANCELLED' AND (r.seat_released=0 OR rf.id IS NULL);

-- check: I10 异常支付对应的预约不得被恢复为占用状态
SELECT r.id, r.status FROM reservation r
JOIN payment_anomaly pa ON pa.reservation_id=r.id
WHERE r.status IN ('CONFIRMED','CHECKED_IN','IN_SERVICE','COMPLETED','PENDING_PAYMENT');

-- check: I8 重复记录
SELECT reservation_id, COUNT(*) n FROM checkin_record GROUP BY reservation_id HAVING COUNT(*)>1;

-- check: I8 重复服务记录
SELECT reservation_id, COUNT(*) n FROM service_record GROUP BY reservation_id HAVING COUNT(*)>1;

-- check: I8 一预约多退款
SELECT reservation_id, COUNT(*) n FROM refund GROUP BY reservation_id HAVING COUNT(*)>1;

-- check: I13 一预约多条成功支付
SELECT reservation_id, COUNT(*) n FROM payment WHERE status='SUCCESS' GROUP BY reservation_id HAVING COUNT(*)>1;

-- check: I14 一预约多条待审取消申请
SELECT reservation_id, COUNT(*) n FROM cancel_request WHERE status='PENDING' GROUP BY reservation_id HAVING COUNT(*)>1;

-- check: I6/I7 金额勾稽（返回 1 行当且仅当差额非 0）
WITH paid AS (
  SELECT COALESCE(SUM(p.amount_cents),0) v FROM payment p
  JOIN reservation r ON r.id=p.reservation_id JOIN slot s ON s.id=r.slot_id
  WHERE s.store_id=:store AND date(s.start_at)=:date AND p.status='SUCCESS'),
refunded AS (
  SELECT COALESCE(SUM(rf.amount_cents),0) v FROM refund rf
  JOIN reservation r ON r.id=rf.reservation_id JOIN slot s ON s.id=r.slot_id
  WHERE s.store_id=:store AND date(s.start_at)=:date AND rf.status='SUCCESS'),
refund_pending AS (
  SELECT COALESCE(SUM(rf.amount_cents),0) v FROM refund rf
  JOIN reservation r ON r.id=rf.reservation_id JOIN slot s ON s.id=r.slot_id
  WHERE s.store_id=:store AND date(s.start_at)=:date AND rf.status IN ('PENDING','PROCESSING','FAILED')),
completed AS (
  SELECT COALESCE(SUM(r.price_cents),0) v FROM reservation r JOIN slot s ON s.id=r.slot_id
  WHERE s.store_id=:store AND date(s.start_at)=:date AND r.status='COMPLETED'),
noshow AS (
  SELECT COALESCE(SUM(r.price_cents),0) v FROM reservation r JOIN slot s ON s.id=r.slot_id
  WHERE s.store_id=:store AND date(s.start_at)=:date AND r.status='NO_SHOW'),
active AS (
  SELECT COALESCE(SUM(r.price_cents),0) v FROM reservation r JOIN slot s ON s.id=r.slot_id
  WHERE s.store_id=:store AND date(s.start_at)=:date
    AND r.status IN ('CONFIRMED','CANCELLATION_REVIEW','CHECKED_IN','IN_SERVICE'))
SELECT paid.v AS paid_cents, completed.v + refunded.v + refund_pending.v + noshow.v + active.v AS accounted_cents,
       paid.v - (completed.v + refunded.v + refund_pending.v + noshow.v + active.v) AS diff
FROM paid, refunded, refund_pending, completed, noshow, active
WHERE paid.v <> completed.v + refunded.v + refund_pending.v + noshow.v + active.v;
