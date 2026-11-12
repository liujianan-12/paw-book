// Package payclient 是调用模拟支付渠道（cmd/paychannel）的 HTTP 客户端。
package payclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"paw-book/internal/paychannel"
)

// Client 渠道客户端。
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// New 创建客户端。
func New(baseURL string) *Client {
	return &Client{BaseURL: baseURL, HTTP: &http.Client{Timeout: 10 * time.Second}}
}

// PayRequest 发起支付的请求体。
type PayRequest struct {
	OutTradeNo  string `json:"out_trade_no"`
	AmountCents int64  `json:"amount_cents"`
	NotifyURL   string `json:"notify_url"`
}

// RefundRequest 发起退款的请求体。
type RefundRequest struct {
	RefundNo    string `json:"refund_no"`
	AmountCents int64  `json:"amount_cents"`
	NotifyURL   string `json:"notify_url"`
}

// Pay 向渠道发起支付，行为参数（成功/失败/超时/重复）通过 opts 注入。
func (c *Client) Pay(ctx context.Context, r PayRequest, opts paychannel.Options) error {
	return c.post(ctx, "/pay", r, opts)
}

// Refund 向渠道发起退款。
func (c *Client) Refund(ctx context.Context, r RefundRequest, opts paychannel.Options) error {
	return c.post(ctx, "/refund", r, opts)
}

func (c *Client) post(ctx context.Context, path string, body any, opts paychannel.Options) error {
	q := buildQuery(opts)
	url := c.BaseURL + path + "?" + q.Encode()

	buf, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("序列化请求体: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("构造请求: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("请求渠道 %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("渠道 %s 应答非 200: %d", path, resp.StatusCode)
	}
	return nil
}

func buildQuery(opts paychannel.Options) url.Values {
	q := url.Values{}
	q.Set("behavior", string(opts.Behavior))
	if opts.Repeat > 1 {
		q.Set("repeat", strconv.Itoa(opts.Repeat))
	}
	if opts.Delay > 0 {
		q.Set("delay", opts.Delay.String())
	}
	return q
}
