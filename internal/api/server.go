// Package api 提供 A1–A20 HTTP 接口、会话身份、幂等与统一错误映射（.agent/03）。
package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"paw-book/internal/domain"
	"paw-book/internal/repo"
)

// Session 服务端会话身份（04 §1：身份只来自会话，忽略请求体 userId/storeId/role）。
type Session struct {
	UserID  int64
	StaffID int64
	Role    string
	StoreID int64
	Name    string
}

// Server HTTP 装配。
type Server struct {
	DB      *sql.DB
	Now     func() time.Time
	Cutoff  time.Duration // 免费取消分界（2h）
	PayHold time.Duration // 占位时长（5m）
	Secret  string        // 模拟渠道回调签名密钥

	mu       sync.RWMutex
	sessions map[string]Session
}

// New 创建服务器；now 为教学时钟。
func New(db *sql.DB, now func() time.Time, cutoff, payHold time.Duration, secret string) *Server {
	if now == nil {
		now = time.Now
	}
	return &Server{DB: db, Now: now, Cutoff: cutoff, PayHold: payHold, Secret: secret,
		sessions: make(map[string]Session)}
}

// Handler 注册 A1–A20 路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	// 身份
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/me", s.handleMe)
	// A1/A2
	mux.HandleFunc("GET /api/slots", s.handleSlots)
	mux.HandleFunc("POST /api/manager/slots", s.idem("slot.create", s.handleCreateSlot))
	// A3/A4/A6/A7/A8
	mux.HandleFunc("POST /api/reservations", s.idem("reservation.create", s.handleCreateReservation))
	mux.HandleFunc("POST /api/reservations/{id}/pay", s.idem("reservation.pay", s.handlePay))
	mux.HandleFunc("POST /api/reservations/{id}/cancel", s.idem("reservation.cancel", s.handleCancel))
	mux.HandleFunc("GET /api/reservations/{id}", s.handleReservationDetail)
	mux.HandleFunc("GET /api/my/reservations", s.handleMyReservations)
	// A5 支付回调 + 退款回调
	mux.HandleFunc("POST /api/payment/notify", s.handlePayNotify)
	mux.HandleFunc("POST /api/refund/notify", s.handleRefundNotify)
	// A9–A11
	mux.HandleFunc("POST /api/reception/reservations/{id}/checkin", s.idem("reservation.checkin", s.handleCheckIn))
	mux.HandleFunc("POST /api/service/reservations/{id}/start", s.idem("reservation.start", s.handleStart))
	mux.HandleFunc("POST /api/service/reservations/{id}/complete", s.idem("reservation.complete", s.handleComplete))
	// A12–A16
	mux.HandleFunc("GET /api/cs/cancel-requests", s.handleCancelRequests)
	mux.HandleFunc("POST /api/cs/cancel-requests/{id}/approve", s.idem("cancel.approve", s.handleApprove))
	mux.HandleFunc("POST /api/cs/cancel-requests/{id}/reject", s.idem("cancel.reject", s.handleReject))
	mux.HandleFunc("GET /api/cs/refunds", s.handleRefunds)
	mux.HandleFunc("POST /api/cs/refunds/{id}/retry", s.idem("refund.retry", s.handleRetryRefund))
	// A17/A18
	mux.HandleFunc("POST /api/manager/reservations/{id}/no-show", s.idem("reservation.noshow", s.handleNoShow))
	mux.HandleFunc("GET /api/manager/daily-close", s.handleDaily)
	// A19/A20
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"code": 0, "message": "ok"})
	})
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("GET /api/smoke", s.handleSmoke)
	// 页面
	mux.Handle("/", s.pages())
	return mux
}

// —— 认证 ——

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Account  string `json:"account"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	sum := sha256.Sum256([]byte(req.Password))
	hash := hex.EncodeToString(sum[:])

	sess := Session{}
	var storeID sql.NullInt64
	var name, role string
	err := s.DB.QueryRow(`SELECT id, store_id, name, role FROM staff WHERE account=? AND password_hash=?`,
		req.Account, hash).Scan(&sess.StaffID, &storeID, &name, &role)
	if err == nil {
		sess.Role, sess.StoreID, sess.Name = role, storeID.Int64, name
	} else if errors.Is(err, sql.ErrNoRows) {
		var uid int64
		if err2 := s.DB.QueryRow(`SELECT id, name FROM app_user WHERE account=? AND password_hash=?`,
			req.Account, hash).Scan(&uid, &name); err2 != nil {
			writeErr(w, domain.Errf(domain.CodeUnauthenticated, "账号或密码错误"))
			return
		}
		sess.UserID, sess.Role, sess.Name = uid, "user", name
	} else {
		writeErr(w, err)
		return
	}

	token := randToken()
	s.mu.Lock()
	s.sessions[token] = sess
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "pb_sid", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	writeOK(w, map[string]any{"role": sess.Role, "name": sess.Name})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("pb_sid"); err == nil {
		s.mu.Lock()
		delete(s.sessions, c.Value)
		s.mu.Unlock()
	}
	writeOK(w, map[string]any{})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.require(w, r)
	if !ok {
		return
	}
	writeOK(w, sess)
}

// require 从会话取身份并按角色授权（未登录 401，角色不符 403）。
func (s *Server) require(w http.ResponseWriter, r *http.Request, roles ...string) (Session, bool) {
	c, err := r.Cookie("pb_sid")
	if err != nil {
		writeErr(w, domain.Errf(domain.CodeUnauthenticated, "未登录"))
		return Session{}, false
	}
	s.mu.RLock()
	sess, ok := s.sessions[c.Value]
	s.mu.RUnlock()
	if !ok {
		writeErr(w, domain.Errf(domain.CodeUnauthenticated, "会话已失效"))
		return Session{}, false
	}
	if len(roles) > 0 {
		allowed := false
		for _, role := range roles {
			if sess.Role == role {
				allowed = true
				break
			}
		}
		if !allowed {
			writeErr(w, domain.Errf(domain.CodeRoleForbidden, "当前角色 %s 无权访问", sess.Role))
			return Session{}, false
		}
	}
	return sess, true
}

// —— 幂等（03 §5） ——

func (s *Server) idem(scope string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if key == "" {
			h(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		sum := sha256.Sum256(body)
		hash := hex.EncodeToString(sum[:])

		var code int
		var storedHash, stored string
		err := s.DB.QueryRow(`SELECT request_hash, response_code, response_body FROM idempotency_key WHERE scope=? AND key_value=?`,
			scope, key).Scan(&storedHash, &code, &stored)
		if err == nil {
			if storedHash != hash {
				writeErr(w, domain.Errf(domain.CodeIdemConflict, "同一 Idempotency-Key 的请求载荷不一致"))
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(code)
			_, _ = w.Write([]byte(stored))
			return
		}
		if !errors.Is(err, sql.ErrNoRows) {
			writeErr(w, err)
			return
		}

		rec := &recorder{header: http.Header{}, status: http.StatusOK}
		h(rec, r)
		if rec.status >= 200 && rec.status < 300 {
			if _, err := s.DB.Exec(`INSERT INTO idempotency_key(scope, key_value, request_hash, response_code, response_body, created_at)
				VALUES(?,?,?,?,?,?)`, scope, key, hash, rec.status, rec.body.String(), repo.FmtTime(s.Now())); err != nil {
				writeErr(w, err)
				return
			}
		}
		for k, vs := range rec.header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(rec.status)
		_, _ = w.Write(rec.body.Bytes())
	}
}

type recorder struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (r *recorder) Header() http.Header  { return r.header }
func (r *recorder) WriteHeader(code int) { r.status = code }
func (r *recorder) Write(b []byte) (int, error) {
	return r.body.Write(b)
}

// —— JSON 与错误 ——

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		writeErr(w, domain.Errf(domain.CodeInvalidParam, "请求体不合法: %v", err))
		return false
	}
	return true
}

func writeOK(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, map[string]any{"code": 0, "message": "ok", "data": data})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr 业务错误 → HTTP 语义状态（03 §4），不泄露 SQL/堆栈。
func writeErr(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	code := "INTERNAL_ERROR"
	msg := "服务暂时不可用"
	var be *domain.BizError
	if errors.As(err, &be) {
		code, msg = be.Code, be.Msg
		switch be.Code {
		case domain.CodeUnauthenticated:
			status = http.StatusUnauthorized
		case domain.CodeRoleForbidden, domain.CodeStoreMismatch, domain.CodePetNotOwned:
			status = http.StatusForbidden
		case domain.CodeNotFound, domain.CodeSlotNotFound, domain.CodePetNotFound, domain.CodePaymentNotFound:
			status = http.StatusNotFound
		case domain.CodeSlotFull, domain.CodeDuplicateActive, domain.CodeCancelAlreadyPending,
			domain.CodeCancelNotPend, domain.CodeStateInvalid, domain.CodeCheckinNotAllow,
			domain.CodeCheckinPending, domain.CodeServiceNotIn, domain.CodeServiceNotStart,
			domain.CodeNotNoShowable, domain.CodeCancelNotAllow, domain.CodeRefundNotRetry,
			domain.CodePaymentNotPend, domain.CodeIdemConflict:
			status = http.StatusConflict
		case domain.CodePaymentExpired, domain.CodeCancelReason, domain.CodeNoShowTooEarly,
			domain.CodeNoShowNoReason, domain.CodeSignInvalid:
			status = http.StatusUnprocessableEntity
		case domain.CodeRefundChanFail:
			status = http.StatusServiceUnavailable
		}
	} else if err != nil {
		code, msg = "INTERNAL_ERROR", "服务暂时不可用"
		status = http.StatusInternalServerError
	}
	writeJSON(w, status, map[string]any{"code": code, "message": msg})
}

func idParam(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

func randToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// sign 计算模拟渠道回调签名（HMAC-SHA256 hex）。
func (s *Server) sign(body []byte) string {
	m := hmac.New(sha256.New, []byte(s.Secret))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

func (s *Server) verifySignature(r *http.Request, body []byte) bool {
	got := r.Header.Get("X-Mock-Signature")
	if got == "" {
		return false
	}
	want := s.sign(body)
	return hmac.Equal([]byte(got), []byte(want))
}
