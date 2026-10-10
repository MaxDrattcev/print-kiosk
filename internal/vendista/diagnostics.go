package vendista

import (
	"context"
	"strconv"
	"time"
)

// Diagnose confirms delivery of a fresh read-only settings request, never a payment.
func (c *Client) Diagnose(ctx context.Context) map[string]any {
	result := c.Check(ctx)
	result["status"] = "warn"
	result["label"] = "Связь с терминалом не подтверждена"
	items, err := c.transactions(ctx, "")
	if err != nil {
		result["last_operation_error"] = err.Error()
	} else {
		result["last_operation_time"] = ""
		var newest time.Time
		for _, tx := range items {
			if strconv.FormatInt(tx.Terminal, 10) != c.Terminal {
				continue
			}
			t, e := parseTime(tx.Time)
			if e == nil && t.After(newest) {
				newest = t
				result["last_operation_time"] = tx.Time
			}
		}
	}
	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var created struct {
		Item struct {
			ID int64 `json:"id"`
		} `json:"item"`
	}
	err = c.request(checkCtx, "POST", "/terminals/"+c.Terminal+"/commands", nil, map[string]any{"command_id": 25, "life_time_interval": "00:00:15"}, &created)
	if err != nil {
		result["label"] = err.Error()
		return result
	}
	if created.Item.ID <= 0 {
		result["label"] = "Vendista не вернула ID диагностической команды"
		return result
	}
	interval := c.Poll
	if interval <= 0 {
		interval = time.Second
	}
	for {
		var response struct {
			Item struct {
				ID        int64  `json:"id"`
				Terminal  int64  `json:"terminal_id"`
				Command   int    `json:"command_id"`
				Delivered string `json:"time_delivered"`
			} `json:"item"`
		}
		err = c.request(checkCtx, "GET", "/terminals/"+c.Terminal+"/commands/"+strconv.FormatInt(created.Item.ID, 10), nil, nil, &response)
		if err != nil {
			result["label"] = "Не удалось подтвердить доставку: " + err.Error()
			return result
		}
		item := response.Item
		if item.ID != created.Item.ID || strconv.FormatInt(item.Terminal, 10) != c.Terminal || item.Command != 25 {
			result["label"] = "API вернула другую диагностическую команду"
			return result
		}
		if delivered, e := parseTime(item.Delivered); e == nil && !delivered.IsZero() && delivered.Year() > 2000 {
			result["status"] = "ok"
			result["label"] = "Связь с терминалом подтверждена"
			result["diagnostic_delivered_at"] = item.Delivered
			return result
		}
		select {
		case <-checkCtx.Done():
			result["label"] = "Терминал не подтвердил связь за 15 секунд"
			return result
		case <-time.After(interval):
		}
	}
}
