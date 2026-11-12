package biz

import "fmt"

// 业务错误码（与 .agent/03 §4 一致；扩展项在 docs/assumptions.md 记录）。
const (
	CodeSlotNotFound    = "SLOT_NOT_FOUND"
	CodeSlotStarted     = "SLOT_STARTED"
	CodeSlotFull        = "SLOT_FULL"
	CodePetNotFound     = "PET_NOT_FOUND"
	CodePetNotOwned     = "PET_NOT_OWNED"
	CodeDuplicateActive = "DUPLICATE_ACTIVE_RESERVATION"
	CodeStateInvalid    = "RESERVATION_STATE_INVALID"
	CodePaymentExpired  = "PAYMENT_EXPIRED"
	CodePaymentNotPend  = "PAYMENT_NOT_PENDING"
	CodePaymentNotFound = "PAYMENT_NOT_FOUND" // 扩展：回调单号不存在（待确认，见 assumptions P4）
	CodeSignInvalid     = "PAYMENT_SIGNATURE_INVALID"
	CodeCancelReason    = "CANCEL_REASON_REQUIRED"
	CodeCancelNotAllow  = "CANCEL_NOT_ALLOWED"
	// CodeCancelAlreadyPending：同一预约同一时刻已有待审核申请（03 §4）
	CodeCancelAlreadyPending = "CANCEL_ALREADY_PENDING"
	CodeCancelNotPend        = "CANCEL_REQUEST_NOT_PENDING"
	CodeCheckinPending       = "CHECKIN_NOT_ALLOWED_PENDING_CANCEL"
	CodeCheckinNotAllow      = "CHECKIN_NOT_ALLOWED"
	CodeServiceNotIn         = "SERVICE_NOT_CHECKED_IN"
	CodeServiceNotStart      = "SERVICE_NOT_STARTED"
	CodeStoreMismatch        = "STORE_MISMATCH"
	CodeRoleForbidden        = "ROLE_FORBIDDEN"
	CodeUnauthenticated      = "UNAUTHENTICATED"
	CodeNoShowTooEarly       = "NO_SHOW_TOO_EARLY"
	CodeNoShowNoReason       = "NO_SHOW_REASON_REQUIRED"
	CodeNotNoShowable        = "NOT_NO_SHOWABLE"
	CodeRefundNotRetry       = "REFUND_NOT_RETRYABLE"
	CodeRefundChanFail       = "REFUND_CHANNEL_FAILED"
	CodeIdemConflict         = "IDEMPOTENCY_CONFLICT"
	CodeNotFound             = "NOT_FOUND"
	CodeInvalidParam         = "INVALID_PARAM"
	CodePermission           = "ROLE_FORBIDDEN"
)

// BizError 业务错误：Code 供程序断言，Msg 供人阅读（不含 SQL/堆栈）。
type BizError struct {
	Code string
	Msg  string
}

func (e *BizError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Msg) }

func errf(code, format string, a ...any) *BizError {
	return &BizError{Code: code, Msg: fmt.Sprintf(format, a...)}
}
