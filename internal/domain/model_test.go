package domain

import "testing"

// I1/R6.2：占名额状态与可取消状态必须是唯一口径。
func TestOccupiesSlot(t *testing.T) {
	occupying := []string{ResPendingPayment, ResConfirmed, ResCancellationReview, ResCheckedIn, ResInService, ResCompleted}
	for _, s := range occupying {
		if !OccupiesSlot(s) {
			t.Errorf("%s 应占用名额", s)
		}
	}
	for _, s := range []string{ResCancelled, ResExpired, ResNoShow, "", "UNKNOWN"} {
		if OccupiesSlot(s) {
			t.Errorf("%s 不应占用名额", s)
		}
	}
}

func TestCanCancelAndTerminal(t *testing.T) {
	if !CanCancel(ResConfirmed) {
		t.Fatal("仅已确认可取消")
	}
	for _, s := range []string{ResPendingPayment, ResCancellationReview, ResCheckedIn, ResInService, ResCompleted, ResCancelled, ResExpired, ResNoShow} {
		if CanCancel(s) {
			t.Errorf("%s 不应允许走取消退款流程（R6.2）", s)
		}
	}
	terminal := []string{ResCancelled, ResExpired, ResNoShow, ResCompleted}
	for _, s := range terminal {
		if !IsTerminal(s) {
			t.Errorf("%s 应为终态", s)
		}
	}
	for _, s := range []string{ResPendingPayment, ResConfirmed, ResCancellationReview, ResCheckedIn, ResInService} {
		if IsTerminal(s) {
			t.Errorf("%s 不应为终态", s)
		}
	}
}

func TestErrf(t *testing.T) {
	e := Errf(CodeStoreMismatch, "门店 %d 不匹配", 2)
	if e.Code != CodeStoreMismatch {
		t.Fatalf("错误码不符: %s", e.Code)
	}
	if e.Error() == "" {
		t.Fatal("Error() 不应为空")
	}
}
