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

// CancelResult 取消处理结果。
type CancelResult struct {
	AutoAccepted bool  // ≥2h：自动受理并发起退款（R3.1）
	RequestID    int64 // <2h：进入客服审核（R3.2）
	RefundID     int64
}

// CancelReservation 已确认预约取消（R3.1/R3.2/R3.3）。
func CancelReservation(ctx context.Context, db repo.DBTX, reservationID, userID int64, reason string, now time.Time, cutoff time.Duration) (*CancelResult, error) {
	tx, err := repo.BeginTx(ctx, db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	var ownerID, slotID int64
	var status, startAt string
	err = tx.QueryRowContext(ctx,
		`SELECT r.user_id, r.slot_id, r.status, s.start_at FROM reservation r JOIN slot s ON s.id=r.slot_id WHERE r.id=?`,
		reservationID).Scan(&ownerID, &slotID, &status, &startAt)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && ownerID != userID) {
		return nil, domain.Errf(domain.CodeNotFound, "预约不存在")
	}
	if err != nil {
		return nil, err
	}
	// A6/A10：已有待审核申请时重复提交，返回明确业务码而不是“状态不允许”
	if status == domain.ResCancellationReview {
		pending, perr := hasPendingCancel(ctx, tx, reservationID)
		if perr != nil {
			return nil, perr
		}
		if pending {
			return nil, domain.Errf(domain.CodeCancelAlreadyPending, "该预约已有待处理的取消申请")
		}
	}
	// R6.2：已核销/服务中/已完成/已取消/过期/未到店均不可取消
	if status != domain.ResConfirmed {
		return nil, domain.Errf(domain.CodeCancelNotAllow, "仅已确认预约可取消（当前 %s）", status)
	}
	start, err := repo.ParseTime(startAt)
	if err != nil {
		return nil, err
	}
	reason = strings.TrimSpace(reason)

	if start.Sub(now) >= cutoff {
		// R3.1：自动受理 + 释放名额 + 全额退款
		res, err := tx.ExecContext(ctx,
			`UPDATE reservation SET status=?, cancel_type=?, cancelled_at=?, updated_at=?, seat_released=1
			 WHERE id=? AND status=? AND seat_released=0`,
			domain.ResCancelled, domain.CancelAutoBefore2H, repo.FmtTime(now), repo.FmtTime(now), reservationID, domain.ResConfirmed)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, domain.Errf(domain.CodeStateInvalid, "预约状态已变化，取消失败")
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE slot SET reserved_count=reserved_count-1 WHERE id=? AND reserved_count>0`, slotID); err != nil {
			return nil, err
		}
		refundID, err := refundOfReservation(ctx, tx, reservationID, domain.ReasonAutoCancel, now)
		if err != nil {
			return nil, err
		}
		if err := repo.Audit(ctx, tx, userID, "user", 0, "RESERVATION_CANCEL_AUTO", "reservation", reservationID, "ok", "", now); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &CancelResult{AutoAccepted: true, RefundID: refundID}, nil
	}

	// R3.2：<2h 必须有原因，进入取消审核（名额保持）
	if reason == "" {
		return nil, domain.Errf(domain.CodeCancelReason, "距开始不足 %v，取消须填写原因由客服审核", cutoff)
	}
	ins, err := tx.ExecContext(ctx,
		`INSERT INTO cancel_request(reservation_id, reason, status, requested_at) VALUES(?,?,?,?)`,
		reservationID, reason, domain.CancelReqPending, repo.FmtTime(now))
	if err != nil {
		if repo.IsConstraintViolation(err, "cancel_request", "reservation_id") || strings.Contains(err.Error(), "uk_cancel_res_pending") {
			return nil, domain.Errf(domain.CodeCancelAlreadyPending, "该预约已有待处理的取消申请")
		}
		return nil, err
	}
	reqID, err := ins.LastInsertId()
	if err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE reservation SET status=?, cancel_type=NULL, updated_at=? WHERE id=? AND status=?`,
		domain.ResCancellationReview, repo.FmtTime(now), reservationID, domain.ResConfirmed)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, domain.Errf(domain.CodeStateInvalid, "预约状态已变化，申请失败")
	}
	if err := repo.Audit(ctx, tx, userID, "user", 0, "RESERVATION_CANCEL_REQUEST", "reservation", reservationID, "ok", reason, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &CancelResult{RequestID: reqID}, nil
}

// ReviewCancel 客服审核取消申请（R4.1/R4.2/R3.3）。
func ReviewCancel(ctx context.Context, db repo.DBTX, requestID, employeeID int64, approve bool, note string, now time.Time) error {
	storeID, err := repo.CheckStaff(ctx, db, employeeID, domain.RoleCS)
	if err != nil {
		return err
	}

	tx, err := repo.BeginTx(ctx, db)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	var resID, resStore int64
	var reqStatus, resStatus string
	err = tx.QueryRowContext(ctx,
		`SELECT c.reservation_id, c.status, r.store_id, r.status
		 FROM cancel_request c JOIN reservation r ON r.id=c.reservation_id WHERE c.id=?`,
		requestID).Scan(&resID, &reqStatus, &resStore, &resStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Errf(domain.CodeNotFound, "取消申请不存在")
	}
	if err != nil {
		return err
	}
	if resStore != storeID {
		return domain.Errf(domain.CodeStoreMismatch, "非本店员工无权操作本店预约")
	}
	if reqStatus != domain.CancelReqPending {
		return domain.Errf(domain.CodeCancelNotPend, "该申请已处理（%s）", reqStatus)
	}
	if resStatus != domain.ResCancellationReview {
		return domain.Errf(domain.CodeStateInvalid, "预约状态已变化（%s）", resStatus)
	}

	decision := domain.CancelReqRejected
	if approve {
		decision = domain.CancelReqApproved
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE cancel_request SET status=?, decided_by=?, decided_at=?, decision_note=? WHERE id=?`,
		decision, employeeID, repo.FmtTime(now), note, requestID); err != nil {
		return err
	}

	if approve {
		// R4.1：批准 → 取消 + 释放名额 + 退款
		res, err := tx.ExecContext(ctx,
			`UPDATE reservation SET status=?, cancel_type=?, cancelled_at=?, updated_at=?, seat_released=1
			 WHERE id=? AND status=? AND seat_released=0`,
			domain.ResCancelled, domain.CancelCSApproved, repo.FmtTime(now), repo.FmtTime(now), resID, domain.ResCancellationReview)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return domain.Errf(domain.CodeStateInvalid, "预约状态已变化，批准失败")
		}
		var slotID int64
		if err := tx.QueryRowContext(ctx, `SELECT slot_id FROM reservation WHERE id=?`, resID).Scan(&slotID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE slot SET reserved_count=reserved_count-1 WHERE id=? AND reserved_count>0`, slotID); err != nil {
			return err
		}
		if _, err := refundOfReservation(ctx, tx, resID, domain.ReasonCSApproved, now); err != nil {
			return err
		}
	} else {
		// R3.3：拒绝 → 恢复已确认，名额不变
		if _, err := tx.ExecContext(ctx,
			`UPDATE reservation SET status=?, updated_at=? WHERE id=? AND status=?`,
			domain.ResConfirmed, repo.FmtTime(now), resID, domain.ResCancellationReview); err != nil {
			return err
		}
	}
	result := map[bool]string{true: "APPROVED", false: "REJECTED"}[approve]
	if err := repo.Audit(ctx, tx, employeeID, domain.RoleCS, storeID, "CANCEL_REVIEW", "cancel_request", requestID, result, note, now); err != nil {
		return err
	}
	return tx.Commit()
}

// refundOfReservation 取该预约最近一笔成功支付，创建/复用退款单（A4 统一通道）。
func refundOfReservation(ctx context.Context, db repo.DBTX, reservationID int64, reason string, now time.Time) (int64, error) {
	var payID, amount int64
	err := db.QueryRowContext(ctx,
		`SELECT id, amount_cents FROM payment WHERE reservation_id=? AND status=?`,
		reservationID, domain.PaySuccess).Scan(&payID, &amount)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, domain.Errf(domain.CodeStateInvalid, "预约无成功支付记录，无法创建退款")
	}
	if err != nil {
		return 0, err
	}
	return ensureRefund(ctx, db, reservationID, payID, amount, reason, now)
}

// CancelRequestView 待审取消申请（A12）。
type CancelRequestView struct {
	ID            int64
	ReservationID int64
	Reason        string
	Status        string
	RequestedAt   string
	PetName       string
	UserName      string
	SlotStart     string
}

// ListCancelRequests 查询本店取消申请（status 为空表示全部）。
func ListCancelRequests(ctx context.Context, db repo.DBTX, storeID int64, status string) ([]CancelRequestView, error) {
	q := `
		SELECT c.id, c.reservation_id, c.reason, c.status, c.requested_at, p.name, u.name, s.start_at
		FROM cancel_request c
		JOIN reservation r ON r.id = c.reservation_id
		JOIN pet p ON p.id = r.pet_id
		JOIN app_user u ON u.id = r.user_id
		JOIN slot s ON s.id = r.slot_id
		WHERE r.store_id=?`
	args := []any{storeID}
	if status != "" {
		q += " AND c.status=?"
		args = append(args, status)
	}
	q += " ORDER BY c.requested_at DESC"

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CancelRequestView
	for rows.Next() {
		var v CancelRequestView
		if err := rows.Scan(&v.ID, &v.ReservationID, &v.Reason, &v.Status, &v.RequestedAt, &v.PetName, &v.UserName, &v.SlotStart); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
