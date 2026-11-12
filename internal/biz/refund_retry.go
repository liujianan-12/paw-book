package biz

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"paw-book/internal/model"
)

// RetryRefund 客服对失败的退款单发起重试（R14/A3）。
// 将退款单从 FAILED 置回 PROCESSING；实际渠道调用由调用方（HTTP 层）随后发起。
// 重试次数不设上限，失败记录保留；退款状态不影响预约终态（I6）。
func RetryRefund(ctx context.Context, db DBTX, refundID, employeeID int64, now time.Time) (string, error) {
	// 员工校验：本店客服
	var empStore int64
	var empRole string
	err := db.QueryRowContext(ctx, `SELECT store_id, role FROM employees WHERE id=?`, employeeID).Scan(&empStore, &empRole)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errf(CodeNotFound, "员工不存在")
	}
	if err != nil {
		return "", err
	}
	if empRole != model.RoleCustomerService {
		return "", errf(CodePermission, "仅客服可发起退款重试（当前角色 %s）", empRole)
	}

	tx, err := beginTx(ctx, db)
	if err != nil {
		return "", err
	}
	defer tx.Rollback() //nolint:errcheck

	// 退款单校验：本店预约的退款单，且处于失败状态
	var refundNo string
	var resStore int64
	var status string
	err = tx.QueryRowContext(ctx,
		`SELECT f.refund_no, f.status, r.store_id FROM refunds f JOIN reservations r ON r.id=f.reservation_id WHERE f.id=?`,
		refundID).Scan(&refundNo, &status, &resStore)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errf(CodeNotFound, "退款单不存在")
	}
	if err != nil {
		return "", err
	}
	if resStore != empStore {
		return "", errf(CodePermission, "非本店员工无权操作本店退款")
	}
	if status != model.RefundFailed {
		return "", errf(CodeInvalidState, "仅失败状态的退款可重试（当前 %s）", status)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE refunds SET status=?, updated_at=? WHERE id=?`,
		model.RefundProcessing, fmtTime(now), refundID); err != nil {
		return "", err
	}
	if err := audit(ctx, tx, "employee", employeeID, "REFUND_RETRY", fmt.Sprintf("refund:%d", refundID), "", now); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return refundNo, nil
}

// ListFailedRefunds 查询待跟进的退款单（供客服页面展示，R14"退款失败需要跟进"）。
func ListFailedRefunds(ctx context.Context, db DBTX, storeID int64) ([]RefundView, error) {
	return listRefunds(ctx, db, storeID, fmt.Sprintf("f.status IN ('%s','%s')", model.RefundFailed, model.RefundPending))
}

// RefundView 退款单条目（含预约信息便于人工核对）。
type RefundView struct {
	ID            int64
	RefundNo      string
	ReservationID int64
	AmountCents   int64
	Status        string
	FailReason    string
	RetryCount    int
	PetName       string
	SlotStart     string
}

func listRefunds(ctx context.Context, db DBTX, storeID int64, statusCond string) ([]RefundView, error) {
	q := fmt.Sprintf(`
		SELECT f.id, f.refund_no, f.reservation_id, f.amount_cents, f.status,
		       COALESCE(f.fail_reason,''), f.retry_count, COALESCE(p.name,''), s.start_at
		FROM refunds f
		JOIN reservations r ON r.id=f.reservation_id
		JOIN slots s ON s.id=r.slot_id
		JOIN pets p ON p.id=r.pet_id
		WHERE r.store_id=? AND %s
		ORDER BY f.created_at`, statusCond)
	rows, err := db.QueryContext(ctx, q, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RefundView
	for rows.Next() {
		var v RefundView
		if err := rows.Scan(&v.ID, &v.RefundNo, &v.ReservationID, &v.AmountCents, &v.Status, &v.FailReason, &v.RetryCount, &v.PetName, &v.SlotStart); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
