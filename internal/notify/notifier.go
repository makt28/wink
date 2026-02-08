package notify

import "context"

// AlertEvent represents a status change event to be sent via notifiers.
type AlertEvent struct {
	MonitorID   string
	MonitorName string
	Type        string // "down" or "up"
	Target      string
	Reason      string
	Timestamp   int64
	Timezone    string // IANA timezone name, e.g. "Asia/Shanghai"; empty = UTC
}

// Notifier is the interface that all notification channel implementations must satisfy.
type Notifier interface {
	// Type returns the notifier type identifier (e.g., "telegram", "webhook").
	Type() string

	// Send delivers an alert event. It should return an error if delivery fails.
	Send(ctx context.Context, event AlertEvent) error

	// Validate checks whether the notifier configuration is valid.
	Validate() error
}
