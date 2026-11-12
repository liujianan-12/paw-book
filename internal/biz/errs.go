package biz

import "fmt"

// 业务错误码：所有校验失败必须返回携带错误码的 BizError，页面据此展示失败原因。
const (
	CodeSlotFull     = "SLOT_FULL"     // 名额已满（R3）
	CodeSlotStarted  = "SLOT_STARTED"  // 时段已开始（R3）
	CodePetConflict  = "PET_CONFLICT"  // 同宠物同时段已有有效预约（R2）
	CodeNotPetOwner  = "NOT_PET_OWNER" // 宠物不属于当前用户（R2/越权）
	CodeInvalidState = "INVALID_STATE" // 当前状态不允许该操作（状态机校验）
	CodeInvalidParam = "INVALID_PARAM" // 参数不合法
	CodeNotFound     = "NOT_FOUND"     // 目标不存在
	CodePermission   = "PERMISSION"    // 无权操作（越权/非本店员工，R20）
)

// BizError 业务错误：Code 供程序判断，Msg 供用户阅读。
type BizError struct {
	Code string
	Msg  string
}

func (e *BizError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Msg) }

func errf(code, format string, a ...any) *BizError {
	return &BizError{Code: code, Msg: fmt.Sprintf(format, a...)}
}
