package biz

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"paw-book/internal/model"
)

// CreateReservationParams 创建预约参数（R2：只能为本人宠物预约）。
type CreateReservationParams struct {
	SlotID int64
	UserID int64
	PetID  int64
}

// CreateReservation 用户为本人宠物预约时段（R2/R3/R4）。
// 成功返回生成的预约（待支付，占名额 payHold 时长）。
// 并发安全：_txlock=immediate 事务串行写 + 部分唯一索引兜底（50 抢 3 恰好 3 成功）。
func CreateReservation(ctx context.Context, db DBTX, p CreateReservationParams, now time.Time, payHold time.Duration) (*model.Reservation, error) {
	// R2：宠物归属校验——改请求里的用户 ID 无法替他人宠物预约
	var ownerID int64
	err := db.QueryRowContext(ctx, `SELECT user_id FROM pets WHERE id=?`, p.PetID).Scan(&ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errf(CodeNotPetOwner, "宠物不存在")
	}
	if err != nil {
		return nil, err
	}
	if ownerID != p.UserID {
		return nil, errf(CodeNotPetOwner, "只能为本人名下的宠物预约")
	}

	tx, err := beginTx(ctx, db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck // 已提交时回滚是空操作

	// R4：懒清理过期待支付预约，释放名额后再校验
	if err := sweepExpiredForSlot(ctx, tx, p.SlotID, now); err != nil {
		return nil, err
	}

	// 时段信息
	var (
		storeID, serviceID, capacity int64
		startAt, endAt               string
	)
	err = tx.QueryRowContext(ctx, `SELECT store_id, service_id, start_at, end_at, capacity FROM slots WHERE id=?`, p.SlotID).
		Scan(&storeID, &serviceID, &startAt, &endAt, &capacity)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errf(CodeNotFound, "时段不存在")
	}
	if err != nil {
		return nil, err
	}
	start, err := parseTime(startAt)
	if err != nil {
		return nil, err
	}
	// R3：时段已开始不可预约
	if !now.Before(start) {
		return nil, errf(CodeSlotStarted, "时段已开始，不可预约")
	}

	// 预约款 = 本次服务全款（预置 9900 分）
	var price int64
	if err := tx.QueryRowContext(ctx, `SELECT price_cents FROM services WHERE id=? AND active=1`, serviceID).Scan(&price); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errf(CodeNotFound, "服务项目不存在或已下架")
		}
		return nil, err
	}

	// R3：名额校验
	active, err := countActive(ctx, tx, p.SlotID, now)
	if err != nil {
		return nil, err
	}
	if active >= int(capacity) {
		return nil, errf(CodeSlotFull, "该时段名额已满（%d/%d）", active, capacity)
	}

	// R2：同宠物同时段唯一（显式校验给出友好错误；索引兜底并发窗口）
	dup, err := countActiveForPet(ctx, tx, p.SlotID, p.PetID, now)
	if err != nil {
		return nil, err
	}
	if dup > 0 {
		return nil, errf(CodePetConflict, "该宠物在此时段已有有效预约")
	}

	deadline := now.Add(payHold)
	created := fmtTime(now)
	res, err := tx.ExecContext(ctx, `
		INSERT INTO reservations(slot_id, store_id, user_id, pet_id, status, amount_cents, pay_deadline, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		p.SlotID, storeID, p.UserID, p.PetID, model.ResPendingPayment, price, fmtTime(deadline), created, created)
	if err != nil {
		// 并发兜底：唯一索引冲突说明同宠物同时段已有有效预约
		if strings.Contains(err.Error(), "idx_res_active_slot_pet") {
			return nil, errf(CodePetConflict, "该宠物在此时段已有有效预约")
		}
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := audit(ctx, tx, "user", p.UserID, "RESERVATION_CREATE", fmt.Sprintf("reservation:%d", id), "", now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &model.Reservation{
		ID:          id,
		SlotID:      p.SlotID,
		StoreID:     storeID,
		UserID:      p.UserID,
		PetID:       p.PetID,
		Status:      model.ResPendingPayment,
		AmountCents: price,
		PayDeadline: deadline,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// CancelPendingReservation 用户取消自己的未支付预约（R4：立即释放名额）。
// 仅本人 + 待支付状态可取消；释放由状态离开"有效集合"实现，天然只发生一次。
func CancelPendingReservation(ctx context.Context, db DBTX, reservationID, userID int64, now time.Time) error {
	tx, err := beginTx(ctx, db)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx,
		`UPDATE reservations SET status=?, cancel_type=?, updated_at=? WHERE id=? AND user_id=? AND status=?`,
		model.ResCancelled, model.CancelUser, fmtTime(now), reservationID, userID, model.ResPendingPayment)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// 区分三种失败原因，给用户明确提示
		var uid int64
		var status string
		qerr := tx.QueryRowContext(ctx, `SELECT user_id, status FROM reservations WHERE id=?`, reservationID).Scan(&uid, &status)
		if errors.Is(qerr, sql.ErrNoRows) {
			return errf(CodeNotFound, "预约不存在")
		}
		if qerr != nil {
			return qerr
		}
		if uid != userID {
			return errf(CodePermission, "只能取消本人的预约")
		}
		return errf(CodeInvalidState, "当前状态（%s）不可取消，仅待支付状态可主动取消", status)
	}
	if err := audit(ctx, tx, "user", userID, "RESERVATION_CANCEL_PENDING", fmt.Sprintf("reservation:%d", reservationID), "", now); err != nil {
		return err
	}
	return tx.Commit()
}
