// Package model 定义业务实体与状态常量（唯一来源，见 .agent/02 §3）。
// 任何业务代码禁止散落状态字符串。
package domain

import "time"

// 预约状态（与 .agent/02 §3、05 状态迁移表完全一致）
const (
	ResPendingPayment     = "PENDING_PAYMENT"
	ResConfirmed          = "CONFIRMED"
	ResCancellationReview = "CANCELLATION_REVIEW"
	ResCheckedIn          = "CHECKED_IN"
	ResInService          = "IN_SERVICE"
	ResCompleted          = "COMPLETED"
	ResCancelled          = "CANCELLED"
	ResExpired            = "EXPIRED"
	ResNoShow             = "NO_SHOW"
)

// OccupiesSlot 是否占用时段名额（.agent/02 I1：含 COMPLETED）。
// 过期的待支付预约是否仍占用由调用方按 pay_expire_at 判断后先行释放。
func OccupiesSlot(status string) bool {
	switch status {
	case ResPendingPayment, ResConfirmed, ResCancellationReview, ResCheckedIn, ResInService, ResCompleted:
		return true
	}
	return false
}

// CanCancel 是否允许发起取消流程（R6.2：已核销/服务中/已完成不可取消）。
func CanCancel(status string) bool { return status == ResConfirmed }

// IsTerminal 是否为终态。
func IsTerminal(status string) bool {
	switch status {
	case ResCancelled, ResExpired, ResNoShow, ResCompleted:
		return true
	}
	return false
}

// 取消类型（reservation.cancel_type，见 .agent/02 DDL 注释）
const (
	CancelUserUnpaid   = "USER_UNPAID"    // 待支付用户主动取消
	CancelAutoBefore2H = "AUTO_BEFORE_2H" // 开始前≥2小时自动受理
	CancelCSApproved   = "CS_APPROVED"    // 客服批准
	CancelExpired      = "EXPIRED"        // 占位超时
)

// 支付单状态
const (
	PayInit    = "INIT"
	PaySuccess = "SUCCESS"
	PayFailed  = "FAILED"
)

// 退款单状态
const (
	RefundPending    = "PENDING"
	RefundProcessing = "PROCESSING"
	RefundSuccess    = "SUCCESS"
	RefundFailed     = "FAILED"
)

// 退款原因（refund.reason）
const (
	ReasonAutoCancel   = "AUTO_CANCEL"
	ReasonCSApproved   = "CS_APPROVED"
	ReasonLatePayment  = "LATE_PAYMENT"
	ReasonChannelError = "CHANNEL_TIMEOUT"
)

// 取消申请状态
const (
	CancelReqPending  = "PENDING"
	CancelReqApproved = "APPROVED"
	CancelReqRejected = "REJECTED"
)

// 员工角色（staff.role）
const (
	RoleManager   = "manager"
	RoleReception = "reception"
	RoleServer    = "server"
	RoleCS        = "cs"
)

// 回调流水处理结果（payment_notification.result）
const (
	NotifyConfirmed     = "CONFIRMED"
	NotifyDuplicate     = "DUPLICATE_IGNORED"
	NotifyAnomalyRefund = "ANOMALY_REFUND"
	NotifyRejected      = "REJECTED"
)

// Slot 时段。
type Slot struct {
	ID            int64
	StoreID       int64
	ItemID        int64
	StartAt       time.Time
	EndAt         time.Time
	Capacity      int
	ReservedCount int
}

// Reservation 预约。
type Reservation struct {
	ID           int64
	StoreID      int64
	SlotID       int64
	UserID       int64
	PetID        int64
	ItemID       int64
	Status       string
	PriceCents   int64
	PayExpireAt  time.Time
	PaidAt       *time.Time
	CheckedInAt  *time.Time
	StartedAt    *time.Time
	CompletedAt  *time.Time
	NoShowAt     *time.Time
	NoShowReason string
	CancelledAt  *time.Time
	CancelType   string
	SeatReleased bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Payment 支付流水（同一预约允许多次失败尝试，最多一条 SUCCESS，I13）。
type Payment struct {
	ID            int64
	ReservationID int64
	OrderNo       string
	AmountCents   int64
	Channel       string
	Status        string
	ProviderTxnID string
	PaidAt        *time.Time
	CreatedAt     time.Time
	Attempt       int
}

// Refund 退款单（一预约一条）。
type Refund struct {
	ID               int64
	RefundNo         string
	PaymentID        int64
	ReservationID    int64
	AmountCents      int64
	Reason           string
	Status           string
	RetryCount       int
	LastError        string
	ProviderRefundID string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}
