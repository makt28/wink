package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// TelegramNotifier sends alerts via the Telegram Bot API.
type TelegramNotifier struct {
	BotToken string
	ChatID   string
	Remark   string
}

func (t *TelegramNotifier) Type() string { return "telegram" }

func (t *TelegramNotifier) Validate() error {
	if t.BotToken == "" {
		return errors.New("telegram: bot_token is required")
	}
	if t.ChatID == "" {
		return errors.New("telegram: chat_id is required")
	}
	return nil
}

func (t *TelegramNotifier) Send(ctx context.Context, event AlertEvent) error {
	text := formatTelegramMessage(event, t.Remark)

	payload := map[string]interface{}{
		"chat_id":    t.ChatID,
		"text":       text,
		"parse_mode": "HTML",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("telegram: marshal payload: %w", err)
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.BotToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func formatTelegramMessage(event AlertEvent, remark string) string {
	var icon, status string
	if event.Type == "down" {
		icon = "🔴"
		status = "DOWN"
	} else {
		icon = "🟢"
		status = "UP"
	}

	var msg string
	if remark != "" {
		msg = fmt.Sprintf("📌 <b>[%s]</b>\n", remark)
	}

	msg += fmt.Sprintf("%s <b>[%s] %s</b>\nTarget: <code>%s</code>",
		icon, status, event.MonitorName, event.Target)

	if event.Reason != "" {
		msg += fmt.Sprintf("\nReason: %s", event.Reason)
	}

	t := time.Unix(event.Timestamp, 0)
	tzLabel := "UTC"
	if event.Timezone != "" {
		if loc, err := time.LoadLocation(event.Timezone); err == nil {
			t = t.In(loc)
			tzLabel = event.Timezone
		}
	}
	msg += fmt.Sprintf("\nTime: %s %s", t.Format("2006-01-02 15:04:05"), tzLabel)

	return msg
}
