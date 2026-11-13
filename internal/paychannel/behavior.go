// Package paychannel 提供模拟支付渠道的行为定义与解析。
// 独立进程运行（cmd/paychannel），可注入四种行为：成功、失败、超时（延迟回调）、重复通知。
package paychannel

import (
	"net/url"
	"strconv"
	"time"
)

// 事件类型（区分支付回调与退款回调）
const (
	EventPay    = "PAY"
	EventRefund = "REFUND"
)

// Behavior 渠道对一笔交易的处理行为。
type Behavior string

const (
	BehaviorSuccess Behavior = "success" // 正常成功并回调
	BehaviorFail    Behavior = "fail"    // 返回失败并回调失败结果
	BehaviorTimeout Behavior = "timeout" // 延迟很久后才回调成功（模拟迟到通知，验证 R8）
)

// Options 一次交易的行为参数。
type Options struct {
	Behavior Behavior      // 处理行为，默认 success
	Repeat   int           // 回调通知重复次数，最小 1（验证 R7 幂等）
	Delay    time.Duration // 回调延迟，0 表示立即
}

// ParseOptions 从查询参数解析行为：?behavior=fail&repeat=2&delay=3s
func ParseOptions(q url.Values) Options {
	opts := Options{Behavior: BehaviorSuccess, Repeat: 1}

	switch Behavior(q.Get("behavior")) {
	case BehaviorSuccess, BehaviorFail, BehaviorTimeout:
		opts.Behavior = Behavior(q.Get("behavior"))
	}

	if n, err := strconv.Atoi(q.Get("repeat")); err == nil && n > 1 {
		opts.Repeat = n
	}

	if d, err := time.ParseDuration(q.Get("delay")); err == nil && d > 0 {
		opts.Delay = d
	}
	return opts
}

// Result 同步应答与异步回调共用的结果模型。
type Result struct {
	OutTradeNo  string `json:"out_trade_no,omitempty"`
	RefundNo    string `json:"refund_no,omitempty"`
	Event       string `json:"event"` // PAY / REFUND
	Result      string `json:"result"`
	AmountCents int64  `json:"amount_cents"`
}
