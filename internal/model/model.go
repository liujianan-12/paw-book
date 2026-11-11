// Package model 定义业务实体与状态常量。
// 状态与 docs/01-需求说明.md 第 5 节状态机一一对应；禁止在业务代码中散落字符串。
package model

import "time"

// 预约状态
const (
	ResPendingPayment = "PENDING_PAYMENT" // 待支付（占名额）
	ResConfirmed      = "CONFIRMED"       // 已确认（已付款）
	ResPendingReview  = "PENDING_REVIEW"  // 取消审核中（占名额，禁止核销）
	ResCancelled      = "CANCELLED"       // 已取消（终态）
	ResCheckedIn      = "CHECKED_IN"      // 已核销
	ResInService      = "IN_SERVICE"      // 服务中
	ResCompleted      = "COMPLETED"       // 已完成（终态，确认收入）
	ResNoShow         = "NO_SHOW"         // 未到店（终态，本题约定不自动退款）
)

// OccupiesSlot 该状态是否占用时段名额（假设 A1：审核中与履约中均占用）。
func OccupiesSlot(status string) bool {
	switch status {
	case ResPendingPayment, ResConfirmed, ResPendingReview, ResCheckedIn, ResInService:
		return true
	}
	return false
}

// CanCancel 是否允许发起本题取消流程（R15：履约开始后不可取消）。
func CanCancel(status string) bool {
	return status == ResConfirmed
}

// IsTerminal 预约是否已到达终态。
func IsTerminal(status string) bool {
	switch status {
	case ResCancelled, ResCompleted, ResNoShow:
		return true
	}
	return false
}

// 取消类型（写入 reservations.cancel_type）
const (
	CancelTimeout  = "TIMEOUT"  // 超时未付款
	CancelUser     = "USER"     // 待支付阶段用户主动取消
	CancelEarly    = "EARLY"    // 开始前≥2小时，自动受理
	CancelApproved = "APPROVED" // 临近开始，客服批准
)

// 支付单状态
const (
	PayCreated = "CREATED"
	PaySuccess = "SUCCESS"
	PayFailed  = "FAILED"
	PayTimeout = "TIMEOUT"
)

// 退款单状态（独立于预约状态，见 R14/I6）
const (
	RefundPending    = "PENDING"
	RefundProcessing = "PROCESSING"
	RefundSuccess    = "SUCCESS"
	RefundFailed     = "FAILED"
)

// 取消审核状态
const (
	ReviewPending  = "PENDING"
	ReviewApproved = "APPROVED"
	ReviewRejected = "REJECTED"
)

// 员工角色
const (
	RoleManager         = "manager"         // 店长
	RoleFront           = "front"           // 前台
	RoleStaff           = "staff"           // 服务人员
	RoleCustomerService = "customerservice" // 客服
)

// Slot 可预约时段。
type Slot struct {
	ID        int64
	StoreID   int64
	ServiceID int64
	StartAt   time.Time
	EndAt     time.Time
	Capacity  int
}

// Reservation 预约。
type Reservation struct {
	ID           int64
	SlotID       int64
	StoreID      int64
	UserID       int64
	PetID        int64
	Status       string
	AmountCents  int64
	PayDeadline  time.Time
	CancelType   string
	CancelReason string
	NoShowReason string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Payment 支付单。
type Payment struct {
	ID            int64
	ReservationID int64
	OutTradeNo    string
	AmountCents   int64
	Status        string
	CreatedAt     time.Time
	NotifiedAt    *time.Time
}

// Refund 退款单。
type Refund struct {
	ID            int64
	ReservationID int64
	PaymentID     int64
	AmountCents   int64
	Status        string
	FailReason    string
	RetryCount    int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
