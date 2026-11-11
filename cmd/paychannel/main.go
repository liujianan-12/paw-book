// 模拟支付渠道服务：可触发成功、失败、超时（迟到通知）与重复通知四种场景。
// 仅用于教学演示与测试，不代表真实第三方支付接入。
//
// 用法示例：
//	POST /pay?behavior=fail&repeat=2&delay=1s
//	{"out_trade_no":"T1001","amount_cents":9900,"notify_url":"http://localhost:8080/api/pay/notify"}
package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"paw-book/internal/paychannel"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /pay", handle("PAY", paychannel.EventPay))
	mux.HandleFunc("POST /refund", handle("REFUND", paychannel.EventRefund))

	addr := ":8081"
	log.Printf("模拟支付渠道启动于 %s（仅教学用途）", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func handle(kind string, event string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OutTradeNo  string `json:"out_trade_no"`
			RefundNo    string `json:"refund_no"`
			AmountCents int64  `json:"amount_cents"`
			NotifyURL   string `json:"notify_url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "请求体不合法: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.NotifyURL == "" {
			http.Error(w, "缺少 notify_url", http.StatusBadRequest)
			return
		}

		opts := paychannel.ParseOptions(r.URL.Query())
		result := "SUCCESS"
		if opts.Behavior == paychannel.BehaviorFail {
			result = "FAILED"
		}
		if kind == "REFUND" && result == "SUCCESS" && opts.Behavior == paychannel.BehaviorTimeout {
			result = "FAILED" // 迟到且失败用于触发退款跟进
		}

		// 同步应答：渠道受理结果
		writeJSON(w, http.StatusOK, map[string]string{
			"accepted": "true", "behavior": string(opts.Behavior), "result": result,
		})

		// 异步回调（延迟、重复注入）
		notify := paychannel.Result{OutTradeNo: req.OutTradeNo, RefundNo: req.RefundNo, Event: event, Result: result, AmountCents: req.AmountCents}
		go scheduleNotify(req.NotifyURL, notify, opts)
	}
}

func scheduleNotify(notifyURL string, payload paychannel.Result, opts paychannel.Options) {
	if opts.Delay > 0 {
		time.Sleep(opts.Delay)
	}
	body, _ := json.Marshal(payload)
	for i := 0; i < opts.Repeat; i++ {
		resp, err := http.Post(notifyURL, "application/json", bytes.NewReader(body))
		if err != nil {
			log.Printf("回调 %s 失败: %v", notifyURL, err)
		} else {
			resp.Body.Close()
		}
		time.Sleep(100 * time.Millisecond) // 重复通知间隔
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
