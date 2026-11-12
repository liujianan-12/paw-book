package api

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"paw-book/internal/domain"
	"paw-book/internal/repo"
	"paw-book/internal/service"
)

// A1 时段列表（所有登录角色）。
func (s *Server) handleSlots(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.require(w, r)
	if !ok {
		return
	}
	storeID := sess.StoreID
	if sess.Role == "user" {
		storeID = 1 // 本题单店；用户只能看本店可约时段
	}
	slots, err := service.ListAvailableSlots(r.Context(), s.DB, storeID, s.Now())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, slots)
}

// A2 店长发布时段（R1.1）。
func (s *Server) handleCreateSlot(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.require(w, r, domain.RoleManager)
	if !ok {
		return
	}
	var req struct {
		StartAt  string `json:"startAt"`
		EndAt    string `json:"endAt"`
		ItemID   int64  `json:"itemId"`
		Capacity int    `json:"capacity"`
	}
	if !decode(w, r, &req) {
		return
	}
	start, err1 := time.Parse(time.RFC3339, req.StartAt)
	end, err2 := time.Parse(time.RFC3339, req.EndAt)
	if err1 != nil || err2 != nil {
		writeErr(w, domain.Errf(domain.CodeInvalidParam, "startAt/endAt 必须是 RFC3339 时间"))
		return
	}
	if req.ItemID == 0 {
		req.ItemID = 1
	}
	if req.Capacity == 0 {
		req.Capacity = 3
	}
	id, err := service.CreateSlot(r.Context(), s.DB, service.CreateSlotParams{
		ActorID: sess.StaffID, StoreID: sess.StoreID, ItemID: req.ItemID,
		StartAt: start, EndAt: end, Capacity: req.Capacity,
	}, s.Now())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{"slotId": id})
}

// A3 创建预约（R1.2/R1.3/R1.4/R2.1）。
func (s *Server) handleCreateReservation(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.require(w, r, "user")
	if !ok {
		return
	}
	var req struct {
		SlotID int64 `json:"slotId"`
		PetID  int64 `json:"petId"`
	}
	if !decode(w, r, &req) {
		return
	}
	res, err := service.CreateReservation(r.Context(), s.DB,
		service.CreateReservationParams{SlotID: req.SlotID, UserID: sess.UserID, PetID: req.PetID},
		s.Now(), s.PayHold)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{
		"reservationId": res.ID, "status": res.Status,
		"payExpireAt": repo.FmtTime(res.PayExpireAt), "priceCents": res.PriceCents,
	})
}

// A4 发起支付（R2.7：每次一条新流水）。
func (s *Server) handlePay(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.require(w, r, "user")
	if !ok {
		return
	}
	id, err := idParam(r)
	if err != nil {
		writeErr(w, domain.Errf(domain.CodeInvalidParam, "预约 ID 不合法"))
		return
	}
	p, err := service.CreatePayment(r.Context(), s.DB, id, sess.UserID, s.Now())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{
		"paymentId": p.ID, "orderNo": p.OrderNo, "status": p.Status, "attempt": p.Attempt,
		"amountCents": p.AmountCents,
	})
}

// A5 支付回调（签名校验 + 幂等）。
func (s *Server) handlePayNotify(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, domain.Errf(domain.CodeInvalidParam, "读取回调失败"))
		return
	}
	if !s.verifySignature(r, body) {
		writeErr(w, domain.Errf(domain.CodeSignInvalid, "回调签名缺失或不正确"))
		return
	}
	var req struct {
		ChannelTxnID string `json:"channelTxnId"`
		OrderNo      string `json:"orderNo"`
		Success      bool   `json:"success"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, domain.Errf(domain.CodeInvalidParam, "回调体不合法: %v", err))
		return
	}
	res, err := service.HandlePayNotify(r.Context(), s.DB, req.ChannelTxnID, req.OrderNo, req.Success, s.Now())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{
		"result": res.Result, "reservationId": res.ReservationID,
		"paymentId": res.PaymentID, "refundId": res.RefundID,
	})
}

// 退款回调（R4.3/R4.4）。
func (s *Server) handleRefundNotify(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, domain.Errf(domain.CodeInvalidParam, "读取回调失败"))
		return
	}
	if !s.verifySignature(r, body) {
		writeErr(w, domain.Errf(domain.CodeSignInvalid, "回调签名缺失或不正确"))
		return
	}
	var req struct {
		RefundNo   string `json:"refundNo"`
		Success    bool   `json:"success"`
		FailReason string `json:"failReason"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, domain.Errf(domain.CodeInvalidParam, "回调体不合法: %v", err))
		return
	}
	res, err := service.HandleRefundNotify(r.Context(), s.DB, req.RefundNo, req.Success, req.FailReason, s.Now())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{"result": res.Result, "refundId": res.RefundID})
}

// A6 用户取消（未付款 → 直接释放；已确认 → 2h 政策）。R2.3/R3.1/R3.2
func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.require(w, r, "user")
	if !ok {
		return
	}
	id, err := idParam(r)
	if err != nil {
		writeErr(w, domain.Errf(domain.CodeInvalidParam, "预约 ID 不合法"))
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)

	detail, err := service.GetReservation(r.Context(), s.DB, id, sess.UserID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if detail.Status == domain.ResPendingPayment {
		if err := service.CancelUnpaid(r.Context(), s.DB, id, sess.UserID, s.Now()); err != nil {
			writeErr(w, err)
			return
		}
		writeOK(w, map[string]any{"status": "CANCELLED", "message": "未付款预约已取消，名额已释放"})
		return
	}
	res, err := service.CancelReservation(r.Context(), s.DB, id, sess.UserID, req.Reason, s.Now(), s.Cutoff)
	if err != nil {
		writeErr(w, err)
		return
	}
	if res.AutoAccepted {
		writeOK(w, map[string]any{"status": "AUTO_ACCEPTED", "refundId": res.RefundID,
			"message": "已自动受理并创建全额退款"})
		return
	}
	writeOK(w, map[string]any{"status": "CANCELLATION_REVIEW", "cancelRequestId": res.RequestID,
		"message": "已提交客服审核，名额保留"})
}

// A7 预约详情（本人或本店员工）。
func (s *Server) handleReservationDetail(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.require(w, r)
	if !ok {
		return
	}
	id, err := idParam(r)
	if err != nil {
		writeErr(w, domain.Errf(domain.CodeInvalidParam, "预约 ID 不合法"))
		return
	}
	uid := int64(0)
	if sess.Role == "user" {
		uid = sess.UserID
	}
	d, err := service.GetReservation(r.Context(), s.DB, id, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	if sess.Role != "user" && d.StoreID != sess.StoreID {
		writeErr(w, domain.Errf(domain.CodeStoreMismatch, "非本店员工无权查看"))
		return
	}
	writeOK(w, d)
}

// A8 我的预约。
func (s *Server) handleMyReservations(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.require(w, r, "user")
	if !ok {
		return
	}
	list, err := service.ListMyReservations(r.Context(), s.DB, sess.UserID, 50)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, list)
}

// A9 前台核销（R5.1/R5.3）。
func (s *Server) handleCheckIn(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.require(w, r, domain.RoleReception)
	if !ok {
		return
	}
	id, err := idParam(r)
	if err != nil {
		writeErr(w, domain.Errf(domain.CodeInvalidParam, "预约 ID 不合法"))
		return
	}
	already, err := service.CheckIn(r.Context(), s.DB, id, sess.StaffID, s.Now())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{"alreadyDone": already, "checkedInAt": repo.FmtTime(s.Now())})
}

// A10 开始服务（R5.2）。
func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.require(w, r, domain.RoleServer)
	if !ok {
		return
	}
	id, err := idParam(r)
	if err != nil {
		writeErr(w, domain.Errf(domain.CodeInvalidParam, "预约 ID 不合法"))
		return
	}
	already, err := service.StartService(r.Context(), s.DB, id, sess.StaffID, s.Now())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{"alreadyDone": already, "startedAt": repo.FmtTime(s.Now())})
}

// A11 完成服务（R5.2/R5.3）。
func (s *Server) handleComplete(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.require(w, r, domain.RoleServer)
	if !ok {
		return
	}
	id, err := idParam(r)
	if err != nil {
		writeErr(w, domain.Errf(domain.CodeInvalidParam, "预约 ID 不合法"))
		return
	}
	already, err := service.CompleteService(r.Context(), s.DB, id, sess.StaffID, s.Now())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{"alreadyDone": already, "completedAt": repo.FmtTime(s.Now())})
}

// A12 待审取消申请（本店）。
func (s *Server) handleCancelRequests(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.require(w, r, domain.RoleCS)
	if !ok {
		return
	}
	list, err := service.ListCancelRequests(r.Context(), s.DB, sess.StoreID, r.URL.Query().Get("status"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, list)
}

// A13 批准取消（R4.1/R4.2）。
func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.require(w, r, domain.RoleCS)
	if !ok {
		return
	}
	id, err := idParam(r)
	if err != nil {
		writeErr(w, domain.Errf(domain.CodeInvalidParam, "申请 ID 不合法"))
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
	if err := service.ReviewCancel(r.Context(), s.DB, id, sess.StaffID, true, req.Note, s.Now()); err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{"status": domain.CancelReqApproved})
}

// A14 拒绝取消（R3.3）。
func (s *Server) handleReject(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.require(w, r, domain.RoleCS)
	if !ok {
		return
	}
	id, err := idParam(r)
	if err != nil {
		writeErr(w, domain.Errf(domain.CodeInvalidParam, "申请 ID 不合法"))
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
	if err := service.ReviewCancel(r.Context(), s.DB, id, sess.StaffID, false, req.Note, s.Now()); err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{"status": domain.CancelReqRejected})
}

// A15 退款列表（status=FAILED 只看未完成）。
func (s *Server) handleRefunds(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.require(w, r, domain.RoleCS)
	if !ok {
		return
	}
	onlyUnfinished := r.URL.Query().Get("status") != ""
	list, err := service.ListRefunds(r.Context(), s.DB, sess.StoreID, onlyUnfinished)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, list)
}

// A16 客服重试退款（R4.4）。
func (s *Server) handleRetryRefund(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.require(w, r, domain.RoleCS)
	if !ok {
		return
	}
	id, err := idParam(r)
	if err != nil {
		writeErr(w, domain.Errf(domain.CodeInvalidParam, "退款 ID 不合法"))
		return
	}
	no, err := service.RetryRefund(r.Context(), s.DB, id, sess.StaffID, s.Now())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{"status": domain.RefundProcessing, "refundNo": no})
}

// A17 店长标记未到店（R7.1）。
func (s *Server) handleNoShow(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.require(w, r, domain.RoleManager)
	if !ok {
		return
	}
	id, err := idParam(r)
	if err != nil {
		writeErr(w, domain.Errf(domain.CodeInvalidParam, "预约 ID 不合法"))
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := service.MarkNoShow(r.Context(), s.DB, id, sess.StaffID, req.Reason, s.Now()); err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{"noShowAt": repo.FmtTime(s.Now()), "reason": req.Reason})
}

// A18 店长日清（R8）。
func (s *Server) handleDaily(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.require(w, r, domain.RoleManager)
	if !ok {
		return
	}
	day := s.Now()
	if v := r.URL.Query().Get("date"); v != "" {
		parsed, err := time.Parse("2006-01-02", v)
		if err != nil {
			writeErr(w, domain.Errf(domain.CodeInvalidParam, "date 格式应为 yyyy-mm-dd"))
			return
		}
		day = parsed
	}
	rep, err := service.GetDailyReport(r.Context(), s.DB, sess.StoreID, day)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, rep)
}

// A20 冒烟：只读检查种子数据可用性。
func (s *Server) handleSmoke(w http.ResponseWriter, r *http.Request) {
	var stores, staffs, items, slots int
	for q, dst := range map[string]*int{
		`SELECT COUNT(*) FROM store`:        &stores,
		`SELECT COUNT(*) FROM staff`:        &staffs,
		`SELECT COUNT(*) FROM service_item`: &items,
		`SELECT COUNT(*) FROM slot`:         &slots,
	} {
		if err := s.DB.QueryRowContext(r.Context(), q).Scan(dst); err != nil {
			writeErr(w, err)
			return
		}
	}
	writeOK(w, map[string]any{"stores": stores, "staff": staffs, "items": items, "slots": slots})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.PingContext(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"code": "DB_UNAVAILABLE", "message": "数据库不可用"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": 0, "message": "ok"})
}
