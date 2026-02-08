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

// WebhookNotifier sends alerts via an HTTP webhook.
type WebhookNotifier struct {
	URL    string
	Method string
	Remark string
}

func (w *WebhookNotifier) Type() string { return "webhook" }

func (w *WebhookNotifier) Validate() error {
	if w.URL == "" {
		return errors.New("webhook: url is required")
	}
	if w.Method == "" {
		return errors.New("webhook: method is required")
	}
	return nil
}

func (w *WebhookNotifier) Send(ctx context.Context, event AlertEvent) error {
	payload := map[string]interface{}{
		"monitor_id":   event.MonitorID,
		"monitor_name": event.MonitorName,
		"type":         event.Type,
		"target":       event.Target,
		"reason":       event.Reason,
		"timestamp":    event.Timestamp,
	}
	if w.Remark != "" {
		payload["remark"] = w.Remark
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("webhook: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, w.Method, w.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook: unexpected status %d", resp.StatusCode)
	}
	return nil
}
