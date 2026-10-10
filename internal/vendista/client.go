// Package vendista implements the documented Retail server payment API.
package vendista

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
	"strconv"
	"strings"
	"sync"
	"time"

	"print-kiosk/internal/storage"
)

const BaseURL = "https://api.vendista.ru:99"

var paymentMu sync.Mutex
var ErrUncertain = errors.New("Результат оплаты пока не подтверждён. Не оплачивайте повторно. Обратитесь к администратору для проверки платежа")

type Client struct {
	Base, Token, Terminal string
	HTTP                  *http.Client
	Timeout               time.Duration
	Poll                  time.Duration
}

func FromSettings(v map[string]string) (*Client, error) {
	id := strings.TrimSpace(v[storage.SettingVendistaTerminalID])
	n, e := strconv.ParseInt(id, 10, 64)
	token := strings.TrimSpace(v[storage.SettingVendistaToken])
	if e != nil || n <= 0 || token == "" {
		return nil, errors.New("Укажите ID терминала и API-токен Vendista в настройках оплаты")
	}
	sec, _ := strconv.Atoi(v[storage.SettingVendistaTimeout])
	if sec < 70 || sec > 180 {
		sec = 90
	}
	return &Client{Base: BaseURL, Token: token, Terminal: id, HTTP: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, Timeout: time.Duration(sec) * time.Second, Poll: 2 * time.Second}, nil
}
func (c *Client) request(ctx context.Context, method, path string, q url.Values, body any, out any) error {
	if q == nil {
		q = url.Values{}
	}
	q.Set("token", c.Token)
	var b io.Reader
	if body != nil {
		data, e := json.Marshal(body)
		if e != nil {
			return e
		}
		b = bytes.NewReader(data)
	}
	req, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Base, "/")+path+"?"+q.Encode(), b)
	if e != nil {
		return errors.New("Некорректный адрес API Vendista")
	}
	req.Header.Set("Content-Type", "application/json")
	res, e := c.HTTP.Do(req)
	if e != nil {
		return errors.New("Нет ответа от API Vendista")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("API Vendista: HTTP %d", res.StatusCode)
	}
	data, e := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if e != nil {
		return errors.New("Ошибка чтения ответа Vendista")
	}
	var envelope struct {
		Success bool `json:"success"`
	}
	if json.Unmarshal(data, &envelope) != nil {
		return errors.New("Некорректный ответ Vendista")
	}
	if !envelope.Success {
		return errors.New("Vendista отклонила запрос. Проверьте токен, доступ к терминалу и его настройки")
	}
	if json.Unmarshal(data, out) != nil {
		return errors.New("Некорректные данные Vendista")
	}
	return nil
}

type Transaction struct {
	ID       int64  `json:"id"`
	Terminal int64  `json:"term_id"`
	Amount   int64  `json:"sum"`
	Status   int    `json:"status"`
	Time     string `json:"time"`
}

func (c *Client) transactions(ctx context.Context, from string) ([]Transaction, error) {
	q := url.Values{"TermId": {c.Terminal}, "OrderDesc": {"true"}, "ItemsOnPage": {"100"}, "OrderByColumn": {"-1"}}
	if from != "" {
		q.Set("DateFrom", from)
	}
	var out struct {
		Items []Transaction `json:"items"`
		Count int           `json:"items_count"`
	}
	e := c.request(ctx, "GET", "/transactions", q, nil, &out)
	// Never silently ignore transactions when the reconciliation window overflows.
	if from != "" && out.Count > 100 {
		return nil, errors.New("Слишком много транзакций для автоматической сверки")
	}
	return out.Items, e
}
func (c *Client) command(ctx context.Context, command int, amount int64) (int64, error) {
	var out struct {
		Item struct {
			ID int64 `json:"id"`
		} `json:"item"`
	}
	e := c.request(ctx, "POST", "/terminals/"+c.Terminal+"/commands", nil, map[string]any{"command_id": command, "parameter1": amount, "life_time_interval": "00:01:00"}, &out)
	if e == nil && out.Item.ID <= 0 {
		return 0, errors.New("Vendista не вернула ID команды")
	}
	return out.Item.ID, e
}
func parseTime(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05.999999999"} {
		t, e := time.ParseInLocation(layout, s, time.FixedZone("Vendista", 3*3600))
		if e == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("Неизвестный формат времени Vendista")
}
func (c *Client) Check(ctx context.Context) map[string]any {
	var out struct {
		Item struct {
			ID       int64  `json:"id"`
			Last     string `json:"last_online_time"`
			Operator string `json:"gsm_operator"`
			RSSI     int    `json:"gsm_rssi"`
		} `json:"item"`
	}
	status := map[string]any{"status": "warn", "label": "Связь не проверена", "terminal_id": c.Terminal}
	if e := c.request(ctx, "GET", "/terminals/"+c.Terminal, nil, nil, &out); e != nil {
		status["label"] = e.Error()
		return status
	}
	if strconv.FormatInt(out.Item.ID, 10) != c.Terminal {
		status["label"] = "API вернула другой терминал"
		return status
	}
	status["last_online_time"] = out.Item.Last
	status["gsm_operator"] = out.Item.Operator
	last, e := parseTime(out.Item.Last)
	if e == nil && time.Since(last) >= -time.Minute && time.Since(last) <= 5*time.Minute {
		status["status"] = "ok"
		status["label"] = "Vendista · недавно на связи"
	} else {
		status["label"] = "Vendista · давно не выходила на связь"
	}
	return status
}

// Pay persists the attempt BEFORE sending the non-idempotent command. An uncertain
// response is reconciled on retry; it never causes another command or a new charge.
func (c *Client) Pay(ctx context.Context, repo *storage.SettingsRepo, order string, amount float64) error {
	if !paymentMu.TryLock() {
		return errors.New("Другая оплата уже проверяется")
	}
	defer paymentMu.Unlock()
	kopecks := int64(math.Round(amount * 100))
	if math.IsNaN(amount) || math.IsInf(amount, 0) || kopecks <= 0 || kopecks > 2147483647 {
		return errors.New("Некорректная сумма оплаты")
	}
	p, e := repo.Payment(order)
	if e != nil && !storage.IsNoPayment(e) {
		return e
	}
	if e == nil {
		if p.TerminalID != c.Terminal || p.Amount != kopecks {
			return errors.New("Параметры заказа изменились после начала оплаты")
		}
		if p.State == "paid" {
			return nil
		}
		if p.State != "pending" {
			return errors.New("Оплата отклонена или возвращена. Создайте новый заказ")
		}
	} else {
		pending, pe := repo.PendingPayment()
		if pe == nil {
			return fmt.Errorf("Сначала проверьте незавершённую оплату %s в кабинете", pending.OrderID)
		}
		if !storage.IsNoPayment(pe) {
			return pe
		}
		rows, e := c.transactions(ctx, "")
		if e != nil {
			return e
		}
		var baseline int64
		for _, t := range rows {
			if t.ID > baseline {
				baseline = t.ID
			}
		}
		p = storage.Payment{OrderID: order, TerminalID: c.Terminal, Amount: kopecks, Baseline: baseline, StartedAt: time.Now().UTC().Format(time.RFC3339Nano), State: "pending"}
		if e = repo.CreatePayment(p); e != nil {
			return e
		}
		id, e := c.command(ctx, 32, kopecks)
		if e != nil {
			return ErrUncertain
		}
		if e = repo.PaymentCommand(order, id); e != nil {
			return ErrUncertain
		}
	}
	deadline := time.NewTimer(c.Timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(c.Poll)
	defer ticker.Stop()
	for {
		done, e := c.reconcile(ctx, repo, p)
		if e == nil && done {
			return nil
		}
		if e != nil && !errors.Is(e, ErrUncertain) {
			return e
		}
		select {
		case <-ctx.Done():
			return ErrUncertain
		case <-deadline.C:
			// StandBy ends waiting; it does not reverse a completed transaction.
			cancelCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, _ = c.command(cancelCtx, 78, 0)
			cancel()
			return ErrUncertain
		case <-ticker.C:
		}
	}
}
func (c *Client) reconcile(ctx context.Context, r *storage.SettingsRepo, p storage.Payment) (bool, error) {
	started, e := parseTime(p.StartedAt)
	if e != nil {
		return false, ErrUncertain
	}
	rows, e := c.transactions(ctx, started.Add(-time.Minute).Format(time.RFC3339))
	if e != nil {
		return false, ErrUncertain
	}
	var matches []Transaction
	id, _ := strconv.ParseInt(p.TerminalID, 10, 64)
	for _, t := range rows {
		when, e := parseTime(t.Time)
		if e == nil && t.ID > p.Baseline && t.Terminal == id && t.Amount == p.Amount && !when.Before(started.Add(-2*time.Second)) && when.Before(started.Add(3*time.Minute)) {
			matches = append(matches, t)
		}
	}
	if len(matches) != 1 {
		return false, ErrUncertain
	}
	t := matches[0]
	switch t.Status {
	case 1:
		if e := r.FinishPayment(p.OrderID, "paid", t.ID); e != nil {
			return false, ErrUncertain
		}
		return true, nil
	case 2, 3:
		if e := r.FinishPayment(p.OrderID, "failed", t.ID); e != nil {
			return false, ErrUncertain
		}
		return false, errors.New("Оплата отклонена или возвращена")
	}
	return false, ErrUncertain
}

// ReconcilePending is read-only towards the terminal: no payment command is sent.
func (c *Client) ReconcilePending(ctx context.Context, r *storage.SettingsRepo) error {
	if !paymentMu.TryLock() {
		return errors.New("Оплата сейчас выполняется")
	}
	defer paymentMu.Unlock()
	p, e := r.PendingPayment()
	if storage.IsNoPayment(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if p.TerminalID != c.Terminal {
		return errors.New("ID терминала отличается от незавершённой оплаты")
	}
	_, e = c.reconcile(ctx, r, p)
	return e
}

// ResolveNoCharge requires an explicit operator decision after checking the
// acquiring account; it is never invoked automatically on a network timeout.
func (c *Client) ResolveNoCharge(ctx context.Context, r *storage.SettingsRepo) error {
	if !paymentMu.TryLock() {
		return errors.New("Оплата сейчас выполняется")
	}
	defer paymentMu.Unlock()
	p, e := r.PendingPayment()
	if storage.IsNoPayment(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if p.TerminalID != c.Terminal {
		return errors.New("ID терминала отличается от незавершённой оплаты")
	}
	started, e := parseTime(p.StartedAt)
	if e != nil || time.Since(started) < 3*time.Minute {
		return errors.New("Подождите не менее 3 минут после начала оплаты")
	}
	rows, e := c.transactions(ctx, started.Add(-time.Minute).Format(time.RFC3339))
	if e != nil {
		return e
	}
	terminal, _ := strconv.ParseInt(p.TerminalID, 10, 64)
	for _, t := range rows {
		if t.ID > p.Baseline && t.Terminal == terminal && t.Amount == p.Amount && (t.Status == 1 || t.Status == 2) {
			return errors.New("Найдена операция списания. Сначала выполните сверку и проверьте возврат")
		}
	}
	return r.FinishPayment(p.OrderID, "failed", 0)
}
