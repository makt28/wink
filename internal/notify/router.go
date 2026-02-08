package notify

import (
	"context"
	"log/slog"
	"time"

	"github.com/makt/wink/internal/config"
)

// Router routes alert events to the appropriate contact group's notifiers.
type Router struct {
	cfgMgr *config.Manager
}

// NewRouter creates a new notification router.
func NewRouter(cfgMgr *config.Manager) *Router {
	return &Router{cfgMgr: cfgMgr}
}

// Notify sends an alert event to notifiers selected by the monitor's notifier_ids.
// Groups are purely visual — notification routing uses the global notifier pool.
// If notifier_ids is empty, no notifications are sent.
func (r *Router) Notify(event AlertEvent) {
	cfg := r.cfgMgr.Get()

	// Find the monitor to get its notifier_ids
	var notifierIDs []string
	for _, m := range cfg.Monitors {
		if m.ID == event.MonitorID {
			notifierIDs = m.NotifierIDs
			break
		}
	}

	if len(notifierIDs) == 0 {
		slog.Debug("monitor has no notifier_ids, skipping notification", "monitor_id", event.MonitorID)
		return
	}

	// Build notifier lookup: ID -> NotifierConfig
	globalNotifiers := make(map[string]config.NotifierConfig, len(cfg.Notifiers))
	for _, nc := range cfg.Notifiers {
		globalNotifiers[nc.ID] = nc
	}

	// Set timezone from config
	event.Timezone = cfg.System.Timezone

	// Fan-out to matched notifiers
	for _, id := range notifierIDs {
		nc, ok := globalNotifiers[id]
		if !ok {
			slog.Warn("notifier not found", "notifier_id", id, "monitor_id", event.MonitorID)
			continue
		}
		notifier := BuildNotifier(nc)
		if notifier == nil {
			slog.Error("unknown notifier type", "type", nc.Type, "notifier_id", id)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := notifier.Send(ctx, event); err != nil {
			slog.Error("notification send failed",
				"type", nc.Type,
				"notifier_id", id,
				"monitor_id", event.MonitorID,
				"error", err,
			)
		} else {
			slog.Info("notification sent",
				"type", nc.Type,
				"notifier_id", id,
				"monitor_id", event.MonitorID,
				"event_type", event.Type,
			)
		}
		cancel()
	}
}

// BuildNotifier constructs a Notifier from a NotifierConfig.
func BuildNotifier(nc config.NotifierConfig) Notifier {
	switch nc.Type {
	case "telegram":
		return &TelegramNotifier{
			BotToken: nc.BotToken,
			ChatID:   nc.ChatID,
			Remark:   nc.Remark,
		}
	case "webhook":
		method := nc.Method
		if method == "" {
			method = "POST"
		}
		return &WebhookNotifier{
			URL:    nc.URL,
			Method: method,
			Remark: nc.Remark,
		}
	default:
		return nil
	}
}
