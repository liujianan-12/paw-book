package payclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"paw-book/internal/paychannel"
)

func TestPaySendsBodyAndBehavior(t *testing.T) {
	var gotPath, gotBehavior, gotRepeat string
	var gotBody PayRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBehavior = r.URL.Query().Get("behavior")
		gotRepeat = r.URL.Query().Get("repeat")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"accepted":"true"}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	err := c.Pay(context.Background(), PayRequest{
		OutTradeNo:  "P1-123",
		AmountCents: 9900,
		NotifyURL:   "http://localhost:8080/api/pay/notify",
	}, paychannel.Options{Behavior: paychannel.BehaviorFail, Repeat: 2, Delay: time.Second})
	if err != nil {
		t.Fatalf("支付请求应成功: %v", err)
	}

	if gotPath != "/pay" {
		t.Errorf("请求路径应为 /pay，得到 %s", gotPath)
	}
	if gotBehavior != "fail" {
		t.Errorf("行为参数应为 fail，得到 %s", gotBehavior)
	}
	if gotRepeat != "2" {
		t.Errorf("重复参数应为 2，得到 %s", gotRepeat)
	}
	if gotBody.OutTradeNo != "P1-123" || gotBody.AmountCents != 9900 {
		t.Errorf("请求体不符: %+v", gotBody)
	}
}

func TestRefundSendsRefundNo(t *testing.T) {
	var gotBody RefundRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/refund" {
			t.Errorf("请求路径应为 /refund，得到 %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"accepted":"true"}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	err := c.Refund(context.Background(), RefundRequest{
		RefundNo:    "R1-456",
		AmountCents: 9900,
		NotifyURL:   "http://localhost:8080/api/refund/notify",
	}, paychannel.Options{})
	if err != nil {
		t.Fatalf("退款请求应成功: %v", err)
	}
	if gotBody.RefundNo != "R1-456" {
		t.Errorf("退款单号不符: %+v", gotBody)
	}
}

func TestServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL)
	if err := c.Pay(context.Background(), PayRequest{}, paychannel.Options{}); err == nil {
		t.Fatal("渠道 500 应返回错误")
	}
}
