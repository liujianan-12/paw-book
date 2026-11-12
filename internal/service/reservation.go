package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"paw-book/internal/domain"
	"paw-book/internal/repo"
)

// CreateReservationParams 创建预约参数（R1.2/R1.3/R1.4/R2.1）。
type CreateReservationParams struct {
	SlotID int64
	UserID int64
	PetID  int64
}

// CreateReservation 用户为本人宠物预约时段。
// 名额采用"条件自增"：UPDATE slot SET reserved_count=reserved_count+1
// WHERE id=? AND reserved_count<capacity AND start_at>?
// 一条语句同时完成"加锁 + 容量校验 + 开始时间校验"，避免先查后改。
func CreateReservation(ctx context.Context, db repo.DBTX, p CreateReservationParams, now time.Time, payHold time.Duration) (*domain.Reservation, error) {
	// R1.2：宠物归属（改请求 ID 无法替他人宠物预约）
	var ownerID int64
	err := db.QueryRowContext(ctx, `SELECT user_id FROM pet WHERE id=?`, p.PetID).Scan(&ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.Errf(domain.CodePetNotFound, "宠物不存在")
	}
	if err != nil {
		return nil, err
	}
	if ownerID != p.UserID {
		return nil, domain.Errf(domain.CodePetNotOwned, "只能为本人名下的宠物预约")
	}

	tx, err := repo.BeginTx(ctx, db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	// R2.2：先清理本时段过期占位，避免名额被过期单占用（同一事务内执行）
	if _, err := expireSlotTx(ctx, tx, p.SlotID, now); err != nil {
		return nil, err
	}

	// R1.3/R1.4：容量与开始时间在同一条件内校验
	res, err := tx.ExecContext(ctx,
		`UPDATE slot SET reserved_count=reserved_count+1 WHERE id=? AND reserved_count<capacity AND start_at>?`,
		p.SlotID, repo.FmtTime(now))
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var capacity, reserved int
		var startAt string
		qerr := tx.QueryRowContext(ctx, `SELECT capacity, reserved_count, start_at FROM slot WHERE id=?`, p.SlotID).
			Scan(&capacity, &reserved, &startAt)
		if errors.Is(qerr, sql.ErrNoRows) {
			return nil, domain.Errf(domain.CodeSlotNotFound, "时段不存在")
		}
		if qerr != nil {
			return nil, qerr
		}
		start, perr := repo.ParseTime(startAt)
		if perr != nil {
			return nil, perr
		}
		if !now.Before(start) {
			return nil, domain.Errf(domain.CodeSlotStarted, "时段已开始，不可预约")
		}
		return nil, domain.Errf(domain.CodeSlotFull, "该时段名额已满（%d/%d）", reserved, capacity)
	}

	s, err := slotByID(ctx, tx, p.SlotID)
	if err != nil {
		return nil, err
	}
	var price int64
	if err := tx.QueryRowContext(ctx, `SELECT price_cents FROM service_item WHERE id=? AND active=1`, s.ItemID).Scan(&price); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.Errf(domain.CodeNotFound, "服务项目不存在或已下架")
		}
		return nil, err
	}

	expireAt := now.Add(payHold)
	created := repo.FmtTime(now)
	ins, err := tx.ExecContext(ctx, `
		INSERT INTO reservation(store_id, slot_id, user_id, pet_id, item_id, status, price_cents,
		                        pay_expire_at, seat_released, version, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,0,0,?,?)`,
		s.StoreID, p.SlotID, p.UserID, p.PetID, s.ItemID, domain.ResPendingPayment, price,
		repo.FmtTime(expireAt), created, created)
	if err != nil {
		// I3 兜底：部分唯一索引冲突说明同宠物同时段已有有效预约（事务回滚自动撤销名额 +1）
		if repo.IsConstraintViolation(err, "reservation", "slot_id", "pet_id") || strings.Contains(err.Error(), "uk_slot_pet_active") {
			return nil, domain.Errf(domain.CodeDuplicateActive, "该宠物在此时段已有有效预约")
		}
		return nil, err
	}
	id, err := ins.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := repo.Audit(ctx, tx, p.UserID, "user", s.StoreID, "RESERVATION_CREATE", "reservation", id, "ok", "", now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &domain.Reservation{
		ID: id, StoreID: s.StoreID, SlotID: p.SlotID, UserID: p.UserID, PetID: p.PetID, ItemID: s.ItemID,
		Status: domain.ResPendingPayment, PriceCents: price, PayExpireAt: expireAt, CreatedAt: now, UpdatedAt: now,
	}, nil
}

// CancelUnpaid 用户取消未支付预约（R2.3：释放名额，不产生退款）。
func CancelUnpaid(ctx context.Context, db repo.DBTX, reservationID, userID int64, now time.Time) error {
	tx, err := repo.BeginTx(ctx, db)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	// P2：他人预约统一按"不存在"返回，避免泄露存在性
	var ownerID, slotID int64
	var status string
	err = tx.QueryRowContext(ctx, `SELECT user_id, slot_id, status FROM reservation WHERE id=?`, reservationID).
		Scan(&ownerID, &slotID, &status)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && ownerID != userID) {
		return domain.Errf(domain.CodeNotFound, "预约不存在")
	}
	if err != nil {
		return err
	}
	if status != domain.ResPendingPayment {
		return domain.Errf(domain.CodeStateInvalid, "当前状态（%s）不可主动取消，仅待支付可取消", status)
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE reservation SET status=?, cancel_type=?, cancelled_at=?, seat_released=1, updated_at=?
		 WHERE id=? AND status=? AND seat_released=0`,
		domain.ResCancelled, domain.CancelUserUnpaid, repo.FmtTime(now), repo.FmtTime(now), reservationID, domain.ResPendingPayment)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.Errf(domain.CodeStateInvalid, "预约状态已变化，取消失败")
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE slot SET reserved_count=reserved_count-1 WHERE id=? AND reserved_count>0`, slotID); err != nil {
		return err
	}
	if err := repo.Audit(ctx, tx, userID, "user", 0, "RESERVATION_CANCEL_UNPAID", "reservation", reservationID, "ok", "", now); err != nil {
		return err
	}
	return tx.Commit()
}

// ReservationDetail 预约详情视图（A7：预约 + 取消申请 + 退款 + 服务进度）。
type ReservationDetail struct {
	domain.Reservation
	PetName       string
	UserName      string
	SlotStart     string
	SlotEnd       string
	CancelReqID   int64
	CancelReqStat string
	CancelReason  string
	RefundID      int64
	RefundStatus  string
	RefundRetry   int
	RefundError   string
}

// GetReservation 查询预约详情；userID>0 时校验归属（P2）。
func GetReservation(ctx context.Context, db repo.DBTX, reservationID, userID int64) (*ReservationDetail, error) {
	var d ReservationDetail
	var payExpire, createdAt, updatedAt string
	var paidAt, checkedIn, started, completed, noShowAt, cancelledAt sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT r.id, r.store_id, r.slot_id, r.user_id, r.pet_id, r.item_id, r.status, r.price_cents,
		       r.pay_expire_at, r.paid_at, r.checked_in_at, r.started_at, r.completed_at,
		       r.no_show_at, COALESCE(r.no_show_reason,''), r.cancelled_at, COALESCE(r.cancel_type,''),
		       r.seat_released, r.created_at, r.updated_at,
		       p.name, u.name, s.start_at, s.end_at
		FROM reservation r
		JOIN pet p ON p.id = r.pet_id
		JOIN app_user u ON u.id = r.user_id
		JOIN slot s ON s.id = r.slot_id
		WHERE r.id=?`, reservationID).Scan(
		&d.ID, &d.StoreID, &d.SlotID, &d.UserID, &d.PetID, &d.ItemID, &d.Status, &d.PriceCents,
		&payExpire, &paidAt, &checkedIn, &started, &completed, &noShowAt, &d.NoShowReason, &cancelledAt, &d.CancelType,
		&d.SeatReleased, &createdAt, &updatedAt, &d.PetName, &d.UserName, &d.SlotStart, &d.SlotEnd)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.Errf(domain.CodeNotFound, "预约不存在")
	}
	if err != nil {
		return nil, err
	}
	if userID > 0 && d.UserID != userID {
		return nil, domain.Errf(domain.CodeNotFound, "预约不存在")
	}
	d.PayExpireAt, _ = repo.ParseTime(payExpire)
	d.CreatedAt, _ = repo.ParseTime(createdAt)
	d.UpdatedAt, _ = repo.ParseTime(updatedAt)

	// 取消申请（取最近一条）
	var reqStatus, reqReason sql.NullString
	if err := db.QueryRowContext(ctx,
		`SELECT id, status, reason FROM cancel_request WHERE reservation_id=? ORDER BY id DESC LIMIT 1`, reservationID).
		Scan(&d.CancelReqID, &reqStatus, &reqReason); err == nil {
		d.CancelReqStat = reqStatus.String
		d.CancelReason = reqReason.String
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	// 退款单
	var rfStatus, rfErr sql.NullString
	if err := db.QueryRowContext(ctx,
		`SELECT id, status, retry_count, COALESCE(last_error,'') FROM refund WHERE reservation_id=?`, reservationID).
		Scan(&d.RefundID, &rfStatus, &d.RefundRetry, &rfErr); err == nil {
		d.RefundStatus = rfStatus.String
		d.RefundError = rfErr.String
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return &d, nil
}

// ListMyReservations 用户自己的预约列表（A8）。
func ListMyReservations(ctx context.Context, db repo.DBTX, userID int64, limit int) ([]ReservationDetail, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := db.QueryContext(ctx, `
		SELECT r.id FROM reservation r WHERE r.user_id=? ORDER BY r.id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []ReservationDetail
	for _, id := range ids {
		d, err := GetReservation(ctx, db, id, 0)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, nil
}
