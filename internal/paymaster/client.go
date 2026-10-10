// Package paymaster implements PayMaster REST v2 payments and refunds.
package paymaster

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"print-kiosk/internal/storage"
	"strings"
	"time"
)

type APIError struct{ Status int }

func (e APIError) Error() string {
	return fmt.Sprintf("PayMaster отклонил запрос (HTTP %d)", e.Status)
}
func DefinitiveRejection(err error) bool {
	var e APIError
	return errors.As(err, &e) && (e.Status == 400 || e.Status == 401 || e.Status == 403 || e.Status == 404 || e.Status == 422)
}

type Client struct {
	Base, Token, Merchant string
	HTTP                  *http.Client
}
type Amount struct {
	Value    float64 `json:"value"`
	Currency string  `json:"currency"`
}
type Payment struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Merchant string `json:"merchantId"`
	Test     bool   `json:"testMode"`
	Amount   Amount `json:"amount"`
	Invoice  struct {
		Order string `json:"orderNo"`
	} `json:"invoice"`
	PaymentData struct {
		Method string `json:"paymentMethod"`
	} `json:"paymentData"`
	Confirmation struct {
		Type string `json:"type"`
		URL  string `json:"paymentUrl"`
	} `json:"confirmation"`
}
type Refund struct {
	ID        string `json:"id"`
	PaymentID string `json:"paymentId"`
	Status    string `json:"status"`
	Amount    Amount `json:"amount"`
}

func FromSettings(v map[string]string) (*Client, error) {
	c := &Client{Base: "https://paymaster.ru", Token: strings.TrimSpace(v[storage.SettingPaymasterToken]), Merchant: strings.TrimSpace(v[storage.SettingPaymasterMerchant]), HTTP: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if c.Token == "" || c.Merchant == "" {
		return nil, fmt.Errorf("укажите API-токен и ID магазина PayMaster в настройках СБП")
	}
	return c, nil
}
func (c *Client) request(ctx context.Context, method, path, key string, body, out any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("не удалось сформировать запрос СБП")
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("нет ответа PayMaster; результат платежа будет проверен повторно")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return APIError{res.StatusCode}
	}
	if out == nil {
		_, err = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
		return err
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("некорректный ответ PayMaster")
	}
	return nil
}
func (c *Client) Create(ctx context.Context, p storage.SBPPayment) (Payment, error) {
	var out Payment
	body := map[string]any{"merchantId": p.Merchant, "testMode": false, "dualMode": false, "invoice": map[string]any{"description": "Оплата услуг PRINTUS", "orderNo": p.OrderID, "expires": time.Unix(p.Expires, 0).UTC().Format(time.RFC3339)}, "amount": Amount{float64(p.Amount) / 100, "RUB"}, "paymentData": map[string]any{"paymentMethod": "sbp"}}
	err := c.request(ctx, "POST", "/api/v2/payments", p.Attempt, body, &out)
	if err == nil {
		err = c.Validate(p, out)
	}
	return out, err
}
func (c *Client) Get(ctx context.Context, p storage.SBPPayment) (Payment, error) {
	var out Payment
	err := c.request(ctx, "GET", "/api/v2/payments/"+url.PathEscape(p.PaymentID), "", nil, &out)
	if err == nil {
		err = c.Validate(p, out)
	}
	return out, err
}
func (c *Client) Validate(p storage.SBPPayment, out Payment) error {
	if out.ID == "" || (p.PaymentID != "" && out.ID != p.PaymentID) || out.Merchant != p.Merchant || out.Test || out.Amount.Currency != "RUB" || math.Abs(out.Amount.Value*100-float64(p.Amount)) > .01 || out.Invoice.Order != p.OrderID || !strings.EqualFold(out.PaymentData.Method, "sbp") {
		return fmt.Errorf("данные платежа СБП не совпадают с заказом")
	}
	return nil
}
func (c *Client) Cancel(ctx context.Context, p storage.SBPPayment) error {
	return c.request(ctx, "PUT", "/api/v2/payments/"+url.PathEscape(p.PaymentID)+"/cancel", "", nil, nil)
}
func PaymentURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil
}
func (c *Client) Refund(ctx context.Context, p storage.SBPPayment) (Refund, error) {
	var out Refund
	var err error
	if p.RefundID != "" {
		err = c.request(ctx, "GET", "/api/v2/refunds/"+url.PathEscape(p.RefundID), "", nil, &out)
	} else {
		err = c.request(ctx, "POST", "/api/v2/refunds", "refund-"+p.Attempt, map[string]any{"paymentId": p.PaymentID, "amount": Amount{float64(p.Amount) / 100, "RUB"}}, &out)
	}
	if err == nil && (out.ID == "" || (p.RefundID != "" && out.ID != p.RefundID) || out.PaymentID != p.PaymentID || out.Amount.Currency != "RUB" || math.Abs(out.Amount.Value*100-float64(p.Amount)) > .01) {
		err = fmt.Errorf("данные возврата СБП не совпадают с платежом")
	}
	return out, err
}

// Check verifies access to the merchant's payment list without creating a payment.
func (c *Client) Check(ctx context.Context) error {
	now := time.Now().UTC()
	q := url.Values{"merchantId": {c.Merchant}, "start": {now.Add(-time.Minute).Format(time.RFC3339)}, "end": {now.Format(time.RFC3339)}}
	var out struct {
		Items []Payment `json:"items"`
	}
	return c.request(ctx, "GET", "/api/v2/payments?"+q.Encode(), "", nil, &out)
}

// Find recovers a lost creation response using read-only queries. Never create a
// replacement payment once the original response has been lost.
func (c *Client) Find(ctx context.Context, p storage.SBPPayment) (Payment, bool, error) {
	cursor := ""
	for page := 0; page < 10; page++ {
		q := url.Values{"merchantId": {p.Merchant}, "start": {time.Unix(p.Expires-180, 0).UTC().Format(time.RFC3339)}, "end": {time.Now().UTC().Format(time.RFC3339)}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var out struct {
			Items  []Payment `json:"items"`
			Cursor string    `json:"cursor"`
		}
		if e := c.request(ctx, "GET", "/api/v2/payments?"+q.Encode(), "", nil, &out); e != nil {
			return Payment{}, false, e
		}
		for _, item := range out.Items {
			if item.Invoice.Order == p.OrderID {
				if e := c.Validate(p, item); e != nil {
					return Payment{}, false, e
				}
				return item, true, nil
			}
		}
		if out.Cursor == "" {
			return Payment{}, false, nil
		}
		cursor = out.Cursor
	}
	return Payment{}, false, fmt.Errorf("слишком много платежей для сверки; проверьте кабинет PayMaster")
}
