package vendista

import (
	"context"
	"fmt"
	"print-kiosk/internal/storage"
	"strconv"
)

// ProcessRefunds sends each cancellation at most once. Lost responses are
// reconciled via the original transaction; they never trigger a second POST.
func (c *Client) ProcessRefunds(ctx context.Context, r *storage.SettingsRepo) error {
	if !paymentMu.TryLock() {
		return fmt.Errorf("выполняется оплата")
	}
	defer paymentMu.Unlock()
	rows, e := r.ScanRefunds()
	if e != nil {
		return e
	}
	for _, p := range rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		if p.TerminalID != c.Terminal {
			continue
		}
		if p.RefundState == "queued" {
			claimed, e := r.ClaimRefund(p.OrderID)
			if e != nil {
				return e
			}
			if !claimed {
				continue
			}
			var out struct {
				Success bool `json:"success"`
			}
			e = c.request(ctx, "POST", "/transactions/"+strconv.FormatInt(p.TransactionID, 10)+"/cancel", nil, nil, &out)
			state, message := "sent", ""
			if e != nil {
				state, message = "unknown", e.Error()
			}
			if e = r.SetRefundState(p.OrderID, state, message); e != nil {
				return e
			}
		}
		var out struct {
			Item struct {
				ID       int64  `json:"id"`
				Terminal int64  `json:"term_id"`
				Status   int    `json:"status"`
				Left     *int64 `json:"left_sum"`
			} `json:"item"`
		}
		e = c.request(ctx, "GET", "/transactions/"+strconv.FormatInt(p.TransactionID, 10), nil, nil, &out)
		if e != nil {
			continue
		}
		terminal, _ := strconv.ParseInt(p.TerminalID, 10, 64)
		if out.Item.ID == p.TransactionID && out.Item.Terminal == terminal && out.Item.Status == 2 && out.Item.Left != nil && *out.Item.Left == 0 {
			if e = r.ConfirmRefund(p.OrderID); e != nil {
				return e
			}
		}
	}
	return nil
}
