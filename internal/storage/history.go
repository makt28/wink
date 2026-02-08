package storage

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const CurrentHistoryVersion = 1

// incidentRetention is how long incidents are kept (30 days).
const incidentRetention = 30 * 24 * time.Hour

// HistoryData is the root structure persisted in history.json (latency only).
type HistoryData struct {
	Version      int                        `json:"version"`
	LastDumpTime int64                      `json:"last_dump_time"`
	Monitors     map[string]*MonitorHistory `json:"monitors"`
}

// IncidentsData is the root structure persisted in incidents.json.
type IncidentsData struct {
	Version      int                   `json:"version"`
	LastDumpTime int64                 `json:"last_dump_time"`
	Monitors     map[string][]Incident `json:"monitors"`
}

// MonitorHistory holds runtime state for a single monitor.
// Incidents are stored separately but merged into copies returned by Get methods.
type MonitorHistory struct {
	Uptime24h      float64        `json:"uptime_24h"`
	Uptime7d       float64        `json:"uptime_7d"`
	Uptime30d      float64        `json:"uptime_30d"`
	LatencyHistory []LatencyPoint `json:"latency_history"`
	Incidents      []Incident     `json:"incidents,omitempty"`
	LastCheckTime  int64          `json:"last_check_time"`
	IsUp           bool           `json:"is_up"`
}

// LatencyPoint is a single probe result with timestamp.
type LatencyPoint struct {
	Time    int64 `json:"t"`
	Latency int   `json:"v"`
	Up      bool  `json:"up"`
}

// Incident records a DOWN/UP state transition.
type Incident struct {
	Type       string `json:"type"`
	StartedAt  int64  `json:"started_at"`
	ResolvedAt *int64 `json:"resolved_at"`
	Duration   int64  `json:"duration"`
	Reason     string `json:"reason"`
}

// HistoryManager manages in-memory history state with periodic and event-driven persistence.
type HistoryManager struct {
	mu            sync.RWMutex
	data          HistoryData
	incidents     map[string][]Incident
	filePath      string
	incidentsPath string
	maxHistoryPts int
}

// NewHistoryManager loads history and incidents from disk or creates empty state.
func NewHistoryManager(filePath string, incidentsPath string, maxHistoryPoints int) (*HistoryManager, error) {
	hm := &HistoryManager{
		filePath:      filePath,
		incidentsPath: incidentsPath,
		maxHistoryPts: maxHistoryPoints,
		incidents:     make(map[string][]Incident),
	}

	// Load history.json
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		slog.Info("history file not found, starting fresh", "path", filePath)
		hm.data = HistoryData{
			Version:  CurrentHistoryVersion,
			Monitors: make(map[string]*MonitorHistory),
		}
	} else {
		if err := hm.loadHistory(); err != nil {
			return nil, fmt.Errorf("load history: %w", err)
		}
	}

	// Load incidents.json
	if _, err := os.Stat(incidentsPath); os.IsNotExist(err) {
		slog.Info("incidents file not found, migrating from history", "path", incidentsPath)
		hm.migrateIncidentsFromHistory()
	} else {
		if err := hm.loadIncidents(); err != nil {
			slog.Warn("failed to load incidents file, migrating from history", "error", err)
			hm.migrateIncidentsFromHistory()
		}
	}

	return hm, nil
}

// migrateIncidentsFromHistory extracts incidents from history.json monitors into the separate store.
func (hm *HistoryManager) migrateIncidentsFromHistory() {
	for id, h := range hm.data.Monitors {
		if len(h.Incidents) > 0 {
			hm.incidents[id] = h.Incidents
			h.Incidents = nil
		}
	}
}

// GetMonitor returns a copy of a monitor's history with incidents merged in (nil if not found).
func (hm *HistoryManager) GetMonitor(id string) *MonitorHistory {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	h, ok := hm.data.Monitors[id]
	if !ok {
		return nil
	}
	cp := *h
	cp.Incidents = hm.incidents[id]
	if cp.Incidents == nil {
		cp.Incidents = []Incident{}
	}
	return &cp
}

// GetAll returns a snapshot of all monitor histories with incidents merged in.
func (hm *HistoryManager) GetAll() map[string]MonitorHistory {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	result := make(map[string]MonitorHistory, len(hm.data.Monitors))
	for k, v := range hm.data.Monitors {
		cp := *v
		cp.Incidents = hm.incidents[k]
		if cp.Incidents == nil {
			cp.Incidents = []Incident{}
		}
		result[k] = cp
	}
	return result
}

// RecordProbe appends a latency point and updates status.
func (hm *HistoryManager) RecordProbe(monitorID string, latencyMs int, up bool) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	h := hm.ensureMonitor(monitorID)
	h.LatencyHistory = append(h.LatencyHistory, LatencyPoint{
		Time:    time.Now().Unix(),
		Latency: latencyMs,
		Up:      up,
	})

	// Ring buffer: trim to max
	if len(h.LatencyHistory) > hm.maxHistoryPts {
		excess := len(h.LatencyHistory) - hm.maxHistoryPts
		h.LatencyHistory = h.LatencyHistory[excess:]
	}

	h.LastCheckTime = time.Now().Unix()
	h.IsUp = up
	hm.recalcUptime(h)
}

// RecordDown creates an open incident.
func (hm *HistoryManager) RecordDown(monitorID string, reason string) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	h := hm.ensureMonitor(monitorID)
	h.IsUp = false

	hm.incidents[monitorID] = append(hm.incidents[monitorID], Incident{
		Type:      "down",
		StartedAt: time.Now().Unix(),
		Reason:    reason,
	})
}

// RecordUp resolves the latest open incident.
func (hm *HistoryManager) RecordUp(monitorID string) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	h := hm.ensureMonitor(monitorID)
	h.IsUp = true

	incs := hm.incidents[monitorID]
	now := time.Now().Unix()
	for i := len(incs) - 1; i >= 0; i-- {
		if incs[i].ResolvedAt == nil {
			incs[i].ResolvedAt = &now
			incs[i].Duration = now - incs[i].StartedAt
			break
		}
	}
}

// RemoveMonitor deletes history and incidents for a removed monitor.
func (hm *HistoryManager) RemoveMonitor(id string) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	delete(hm.data.Monitors, id)
	delete(hm.incidents, id)
}

// Dump persists current state to disk atomically (both history.json and incidents.json).
func (hm *HistoryManager) Dump() error {
	hm.mu.RLock()
	now := time.Now().Unix()

	// Copy history data (without incidents)
	dataCopy := HistoryData{
		Version:      hm.data.Version,
		LastDumpTime: now,
		Monitors:     make(map[string]*MonitorHistory, len(hm.data.Monitors)),
	}
	for k, v := range hm.data.Monitors {
		cp := *v
		cp.Incidents = nil // incidents go in separate file
		dataCopy.Monitors[k] = &cp
	}

	// Copy incidents with 30-day eviction
	cutoff := now - int64(incidentRetention.Seconds())
	incidentsCopy := IncidentsData{
		Version:      CurrentHistoryVersion,
		LastDumpTime: now,
		Monitors:     make(map[string][]Incident, len(hm.incidents)),
	}
	for k, incs := range hm.incidents {
		var kept []Incident
		for _, inc := range incs {
			// Keep if started within retention window OR still unresolved
			if inc.StartedAt >= cutoff || inc.ResolvedAt == nil {
				kept = append(kept, inc)
			}
		}
		if len(kept) > 0 {
			incidentsCopy.Monitors[k] = kept
		}
	}
	hm.mu.RUnlock()

	// Write both files
	if err := atomicWriteJSON(hm.filePath, dataCopy); err != nil {
		return fmt.Errorf("dump history: %w", err)
	}
	if err := atomicWriteJSON(hm.incidentsPath, incidentsCopy); err != nil {
		return fmt.Errorf("dump incidents: %w", err)
	}
	return nil
}

func (hm *HistoryManager) ensureMonitor(id string) *MonitorHistory {
	h, ok := hm.data.Monitors[id]
	if !ok {
		h = &MonitorHistory{
			IsUp:           true,
			LatencyHistory: make([]LatencyPoint, 0),
		}
		hm.data.Monitors[id] = h
	}
	if hm.incidents[id] == nil {
		hm.incidents[id] = make([]Incident, 0)
	}
	return h
}

func (hm *HistoryManager) recalcUptime(h *MonitorHistory) {
	now := time.Now().Unix()
	h.Uptime24h = calcUptimeWindow(h.LatencyHistory, now, 24*3600)
	h.Uptime7d = calcUptimeWindow(h.LatencyHistory, now, 7*24*3600)
	h.Uptime30d = calcUptimeWindow(h.LatencyHistory, now, 30*24*3600)
}

func calcUptimeWindow(points []LatencyPoint, now int64, windowSec int64) float64 {
	cutoff := now - windowSec
	total := 0
	up := 0
	for _, p := range points {
		if p.Time >= cutoff {
			total++
			if p.Up {
				up++
			}
		}
	}
	if total == 0 {
		return 100.0
	}
	return float64(up) / float64(total) * 100.0
}

func (hm *HistoryManager) loadHistory() error {
	data, err := os.ReadFile(hm.filePath)
	if err != nil {
		return err
	}

	var hd HistoryData
	if err := json.Unmarshal(data, &hd); err != nil {
		return fmt.Errorf("parse history JSON: %w", err)
	}

	if hd.Monitors == nil {
		hd.Monitors = make(map[string]*MonitorHistory)
	}
	hm.data = hd
	return nil
}

func (hm *HistoryManager) loadIncidents() error {
	data, err := os.ReadFile(hm.incidentsPath)
	if err != nil {
		return err
	}

	var id IncidentsData
	if err := json.Unmarshal(data, &id); err != nil {
		return fmt.Errorf("parse incidents JSON: %w", err)
	}

	if id.Monitors == nil {
		id.Monitors = make(map[string][]Incident)
	}
	hm.incidents = id.Monitors
	return nil
}

// atomicWriteJSON writes data as JSON to a file atomically.
func atomicWriteJSON(filePath string, data interface{}) error {
	bs, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(filePath)
	tmp, err := os.CreateTemp(dir, filepath.Base(filePath)+".tmp.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	defer func() {
		if tmp != nil {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(bs); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	tmp = nil

	return os.Rename(tmpName, filePath)
}
