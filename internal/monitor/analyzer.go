package monitor

import (
	"log/slog"
	"sync"
	"time"

	"github.com/makt28/wink/internal/notify"
	"github.com/makt28/wink/internal/storage"
)

// monitorState tracks the runtime state for flapping control.
type monitorState struct {
	isUp          bool
	failCount     int
	reminderCount int // failures since last alert (used after DOWN)
}

// AnalyzeResult is returned to the scheduler to allow dynamic interval switching.
type AnalyzeResult struct {
	IsFailing bool // true if probe failed (regardless of UP/DOWN state)
}

// Analyzer processes probe results, implements flapping control, and triggers notifications.
type Analyzer struct {
	mu       sync.Mutex
	states   map[string]*monitorState
	histMgr  *storage.HistoryManager
	notifier *notify.Router
}

// NewAnalyzer creates a new Analyzer.
func NewAnalyzer(histMgr *storage.HistoryManager, notifier *notify.Router) *Analyzer {
	return &Analyzer{
		states:   make(map[string]*monitorState),
		histMgr:  histMgr,
		notifier: notifier,
	}
}

// Process handles a probe result with flapping control and reminder alerts.
func (a *Analyzer) Process(monitorID, monitorName, target string, maxRetries, reminderInterval int, result ProbeResult) AnalyzeResult {
	a.mu.Lock()
	defer a.mu.Unlock()

	state := a.ensureState(monitorID)
	latencyMs := int(result.Latency.Milliseconds())

	a.histMgr.RecordProbe(monitorID, latencyMs, result.Up)

	if result.Up {
		// --- Success path ---
		prevDown := !state.isUp
		state.failCount = 0
		state.reminderCount = 0

		if prevDown {
			state.isUp = true
			a.histMgr.RecordUp(monitorID)

			slog.Info("monitor recovered", "id", monitorID, "name", monitorName)
			if err := a.histMgr.Dump(); err != nil {
				slog.Error("failed to dump history on recovery", "error", err)
			}

			a.notifier.Notify(notify.AlertEvent{
				MonitorID:   monitorID,
				MonitorName: monitorName,
				Type:        "up",
				Target:      target,
				Timestamp:   time.Now().Unix(),
			})
		}
		return AnalyzeResult{IsFailing: false}
	}

	// --- Failure path ---
	state.failCount++

	slog.Debug("probe failed",
		"id", monitorID,
		"name", monitorName,
		"fail_count", state.failCount,
		"max_retries", maxRetries,
		"error", result.Error,
	)

	if state.isUp && state.failCount >= maxRetries {
		// Transition: UP -> DOWN (initial alert)
		state.isUp = false
		state.reminderCount = 0
		a.histMgr.RecordDown(monitorID, result.Error)

		slog.Warn("monitor is DOWN", "id", monitorID, "name", monitorName, "reason", result.Error)
		if err := a.histMgr.Dump(); err != nil {
			slog.Error("failed to dump history on down", "error", err)
		}

		a.notifier.Notify(notify.AlertEvent{
			MonitorID:   monitorID,
			MonitorName: monitorName,
			Type:        "down",
			Target:      target,
			Reason:      result.Error,
			Timestamp:   time.Now().Unix(),
		})
	} else if !state.isUp && reminderInterval > 0 {
		// Already DOWN: check if we should resend alert
		state.reminderCount++
		if state.reminderCount >= reminderInterval {
			state.reminderCount = 0

			slog.Warn("monitor still DOWN (reminder)", "id", monitorID, "name", monitorName)
			a.notifier.Notify(notify.AlertEvent{
				MonitorID:   monitorID,
				MonitorName: monitorName,
				Type:        "down",
				Target:      target,
				Reason:      result.Error,
				Timestamp:   time.Now().Unix(),
			})
		}
	}

	return AnalyzeResult{IsFailing: true}
}

// RemoveState cleans up state for a removed monitor.
func (a *Analyzer) RemoveState(monitorID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.states, monitorID)
}

func (a *Analyzer) ensureState(id string) *monitorState {
	s, ok := a.states[id]
	if !ok {
		s = &monitorState{isUp: true}
		a.states[id] = s
	}
	return s
}
