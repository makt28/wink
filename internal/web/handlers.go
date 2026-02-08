package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/makt/wink/internal/config"
	"github.com/makt/wink/internal/notify"
	"github.com/makt/wink/internal/storage"
	"golang.org/x/crypto/bcrypt"
)

// Handlers holds the HTMX page handlers.
type Handlers struct {
	cfgMgr  *config.Manager
	histMgr *storage.HistoryManager
	tmpl    *TemplateRenderer
}

// NewHandlers creates page handlers.
func NewHandlers(cfgMgr *config.Manager, histMgr *storage.HistoryManager, tmpl *TemplateRenderer) *Handlers {
	return &Handlers{
		cfgMgr:  cfgMgr,
		histMgr: histMgr,
		tmpl:    tmpl,
	}
}

// Dashboard renders the main monitor list page.
// Data is minimal — the JS client fetches monitor data via /api/monitors.
func (h *Handlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	cfg := h.cfgMgr.Get()
	lang := getLang(r)
	theme := getTheme(r)

	data := map[string]interface{}{
		"Total":       len(cfg.Monitors),
		"Lang":        lang,
		"Theme":       theme,
		"Version":     version,
		"I18nStrings": buildJSI18n(lang),
	}

	h.tmpl.Render(w, "dashboard.html", data)
}

// apiMonitorView is the JSON representation of a monitor for the API.
type apiMonitorView struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Type         string                 `json:"type"`
	Target       string                 `json:"target"`
	Interval     int                    `json:"interval"`
	Enabled      bool                   `json:"enabled"`
	GroupID      string                 `json:"group_id"`
	GroupName    string                 `json:"group_name"`
	IsUp         bool                   `json:"is_up"`
	HasHistory   bool                   `json:"has_history"`
	Uptime24h    float64                `json:"uptime_24h"`
	Uptime7d     float64                `json:"uptime_7d"`
	Uptime30d    float64                `json:"uptime_30d"`
	LastCheck    int64                  `json:"last_check"`
	ResponseTime int                    `json:"response_time"`
	Heartbeats   []storage.LatencyPoint `json:"heartbeats"`
}

// apiDetailView extends apiMonitorView with incidents and config fields.
type apiDetailView struct {
	apiMonitorView
	MaxRetries       int                `json:"max_retries"`
	RetryInterval    int                `json:"retry_interval"`
	ReminderInterval int                `json:"reminder_interval"`
	Timeout          int                `json:"timeout"`
	IgnoreTLS        bool               `json:"ignore_tls"`
	GroupID          string             `json:"group_id"`
	Incidents        []storage.Incident `json:"incidents"`
}

// getPoints reads the "points" query param, clamped to [1, 200], default 90.
func getPoints(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("points"))
	if err != nil || n <= 0 {
		return 90
	}
	if n > 200 {
		return 200
	}
	return n
}

// roundUptime rounds to 2 decimal places.
func roundUptime(v float64) float64 {
	return math.Round(v*100) / 100
}

// lastLatency returns the most recent latency value, or 0.
func lastLatency(pts []storage.LatencyPoint) int {
	if len(pts) == 0 {
		return 0
	}
	return pts[len(pts)-1].Latency
}

// tailPoints returns the last n points from a slice.
func tailPoints(pts []storage.LatencyPoint, n int) []storage.LatencyPoint {
	if len(pts) <= n {
		return pts
	}
	return pts[len(pts)-n:]
}

// APIMonitors returns JSON data for all monitors.
func (h *Handlers) APIMonitors(w http.ResponseWriter, r *http.Request) {
	cfg := h.cfgMgr.Get()
	histories := h.histMgr.GetAll()
	points := getPoints(r)

	views := make([]apiMonitorView, 0, len(cfg.Monitors))
	for _, m := range cfg.Monitors {
		groupName := ""
		if g, ok := cfg.ContactGroups[m.GroupID]; ok {
			groupName = g.Name
		}
		mv := apiMonitorView{
			ID:        m.ID,
			Name:      m.Name,
			Type:      m.Type,
			Target:    m.Target,
			Interval:  m.Interval,
			Enabled:   m.IsEnabled(),
			GroupID:   m.GroupID,
			GroupName: groupName,
			IsUp:      true,
		}
		if hist, ok := histories[m.ID]; ok {
			mv.HasHistory = true
			mv.IsUp = hist.IsUp
			mv.Uptime24h = roundUptime(hist.Uptime24h)
			mv.Uptime7d = roundUptime(hist.Uptime7d)
			mv.Uptime30d = roundUptime(hist.Uptime30d)
			mv.LastCheck = hist.LastCheckTime
			mv.Heartbeats = tailPoints(hist.LatencyHistory, points)
			mv.ResponseTime = lastLatency(hist.LatencyHistory)
		}
		if mv.Heartbeats == nil {
			mv.Heartbeats = []storage.LatencyPoint{}
		}
		views = append(views, mv)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"monitors": views,
		"total":    len(cfg.Monitors),
	})
}

// APIMonitorDetail returns JSON data for a single monitor with incidents.
func (h *Handlers) APIMonitorDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cfg := h.cfgMgr.Get()
	points := getPoints(r)

	var found *config.Monitor
	for i := range cfg.Monitors {
		if cfg.Monitors[i].ID == id {
			found = &cfg.Monitors[i]
			break
		}
	}

	if found == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		return
	}

	dv := apiDetailView{
		apiMonitorView: apiMonitorView{
			ID:       found.ID,
			Name:     found.Name,
			Type:     found.Type,
			Target:   found.Target,
			Interval: found.Interval,
			Enabled:  found.IsEnabled(),
			IsUp:     true,
		},
		MaxRetries:       found.MaxRetries,
		RetryInterval:    found.RetryInterval,
		ReminderInterval: found.ReminderInterval,
		Timeout:          found.Timeout,
		IgnoreTLS:        found.IgnoreTLS,
		GroupID:          found.GroupID,
	}

	hist := h.histMgr.GetMonitor(id)
	if hist != nil {
		dv.HasHistory = true
		dv.IsUp = hist.IsUp
		dv.Uptime24h = roundUptime(hist.Uptime24h)
		dv.Uptime7d = roundUptime(hist.Uptime7d)
		dv.Uptime30d = roundUptime(hist.Uptime30d)
		dv.LastCheck = hist.LastCheckTime
		dv.Heartbeats = tailPoints(hist.LatencyHistory, points)
		dv.ResponseTime = lastLatency(hist.LatencyHistory)
		dv.Incidents = hist.Incidents
	}
	if dv.Heartbeats == nil {
		dv.Heartbeats = []storage.LatencyPoint{}
	}
	if dv.Incidents == nil {
		dv.Incidents = []storage.Incident{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(dv)
}

// MonitorForm renders the add monitor form.
func (h *Handlers) MonitorForm(w http.ResponseWriter, r *http.Request) {
	cfg := h.cfgMgr.Get()
	lang := getLang(r)
	data := map[string]interface{}{
		"Groups":       cfg.ContactGroups,
		"IsEdit":       false,
		"Lang":         lang,
		"Theme":        getTheme(r),
		"Version":      version,
		"AllNotifiers": flattenNotifiers(cfg),
		"SelectedNIDs": map[string]bool{},
	}
	h.tmpl.Render(w, "monitor_form.html", data)
}

// notifierInfo is a flat view of a notifier for the form and settings page.
type notifierInfo struct {
	ID       string
	Type     string
	Label    string
	Remark   string
	BotToken string
	ChatID   string
	URL      string
	Method   string
}

// EditMonitorForm renders the edit monitor form pre-filled with data.
func (h *Handlers) EditMonitorForm(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cfg := h.cfgMgr.Get()
	lang := getLang(r)

	var found *config.Monitor
	for i := range cfg.Monitors {
		if cfg.Monitors[i].ID == id {
			found = &cfg.Monitors[i]
			break
		}
	}

	if found == nil {
		http.Error(w, "Monitor not found", http.StatusNotFound)
		return
	}

	selectedNIDs := make(map[string]bool, len(found.NotifierIDs))
	for _, nid := range found.NotifierIDs {
		selectedNIDs[nid] = true
	}

	data := map[string]interface{}{
		"Groups":       cfg.ContactGroups,
		"IsEdit":       true,
		"Monitor":      *found,
		"Lang":         lang,
		"Theme":        getTheme(r),
		"Version":      version,
		"AllNotifiers": flattenNotifiers(cfg),
		"SelectedNIDs": selectedNIDs,
	}
	h.tmpl.Render(w, "monitor_form.html", data)
}

// respondError returns a JSON error for AJAX requests, or a plain http.Error fallback.
func respondError(w http.ResponseWriter, r *http.Request, msg string, status int) {
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "message": msg})
		return
	}
	http.Error(w, msg, status)
}

// CreateMonitor handles the form submission for adding a new monitor.
func (h *Handlers) CreateMonitor(w http.ResponseWriter, r *http.Request) {
	lang := getLang(r)
	if err := r.ParseForm(); err != nil {
		respondError(w, r, translate(lang, "settings.error_invalid_form"), http.StatusBadRequest)
		return
	}

	cfg := h.cfgMgr.Get()

	if len(cfg.Monitors) >= cfg.System.MaxMonitors {
		respondError(w, r, translate(lang, "form.error_max_monitors"), http.StatusBadRequest)
		return
	}

	m := config.Monitor{
		ID:               generateToken()[:8],
		Name:             r.FormValue("name"),
		Type:             r.FormValue("type"),
		Target:           r.FormValue("target"),
		GroupID:          r.FormValue("group_id"),
		Interval:         formInt(r, "interval", cfg.System.CheckInterval),
		Timeout:          formInt(r, "timeout", 5),
		MaxRetries:       formInt(r, "max_retries", 3),
		RetryInterval:    formInt(r, "retry_interval", 0),
		ReminderInterval: formInt(r, "reminder_interval", 0),
		IgnoreTLS:        r.FormValue("ignore_tls") == "on",
		NotifierIDs:      r.Form["notifier_ids"],
	}

	cfg.Monitors = append(cfg.Monitors, m)

	if err := h.cfgMgr.Save(cfg); err != nil {
		slog.Error("failed to save config", "error", err)
		respondError(w, r, translate(lang, "settings.error_save_failed")+": "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("monitor created", "id", m.ID, "name", m.Name)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// UpdateMonitor handles the form submission for editing an existing monitor.
func (h *Handlers) UpdateMonitor(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	lang := getLang(r)
	if err := r.ParseForm(); err != nil {
		respondError(w, r, translate(lang, "settings.error_invalid_form"), http.StatusBadRequest)
		return
	}

	cfg := h.cfgMgr.Get()

	idx := -1
	for i := range cfg.Monitors {
		if cfg.Monitors[i].ID == id {
			idx = i
			break
		}
	}

	if idx == -1 {
		respondError(w, r, translate(lang, "settings.error_not_found"), http.StatusNotFound)
		return
	}

	cfg.Monitors[idx].Name = r.FormValue("name")
	cfg.Monitors[idx].Type = r.FormValue("type")
	cfg.Monitors[idx].Target = r.FormValue("target")
	cfg.Monitors[idx].GroupID = r.FormValue("group_id")
	cfg.Monitors[idx].Interval = formInt(r, "interval", cfg.System.CheckInterval)
	cfg.Monitors[idx].Timeout = formInt(r, "timeout", 5)
	cfg.Monitors[idx].MaxRetries = formInt(r, "max_retries", 3)
	cfg.Monitors[idx].RetryInterval = formInt(r, "retry_interval", 0)
	cfg.Monitors[idx].ReminderInterval = formInt(r, "reminder_interval", 0)
	cfg.Monitors[idx].IgnoreTLS = r.FormValue("ignore_tls") == "on"
	cfg.Monitors[idx].NotifierIDs = r.Form["notifier_ids"]

	if err := h.cfgMgr.Save(cfg); err != nil {
		slog.Error("failed to save config", "error", err)
		respondError(w, r, translate(lang, "settings.error_save_failed")+": "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("monitor updated", "id", id, "name", cfg.Monitors[idx].Name)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// DeleteMonitor handles monitor deletion.
func (h *Handlers) DeleteMonitor(w http.ResponseWriter, r *http.Request) {
	id := r.FormValue("id")
	if id == "" {
		http.Error(w, "Missing monitor ID", http.StatusBadRequest)
		return
	}

	cfg := h.cfgMgr.Get()
	filtered := make([]config.Monitor, 0, len(cfg.Monitors))
	found := false
	for _, m := range cfg.Monitors {
		if m.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, m)
	}

	if !found {
		http.Error(w, "Monitor not found", http.StatusNotFound)
		return
	}

	cfg.Monitors = filtered
	if err := h.cfgMgr.Save(cfg); err != nil {
		slog.Error("failed to save config", "error", err)
		http.Error(w, "Failed to save", http.StatusInternalServerError)
		return
	}

	h.histMgr.RemoveMonitor(id)
	slog.Info("monitor deleted", "id", id)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// SettingsPage renders the settings page.
func (h *Handlers) SettingsPage(w http.ResponseWriter, r *http.Request) {
	cfg := h.cfgMgr.Get()
	lang := getLang(r)

	flash := ""
	flashType := ""
	if r.URL.Query().Get("saved") == "1" {
		flash = translate(lang, "settings.saved")
		flashType = "success"
	}

	data := map[string]interface{}{
		"System":       cfg.System,
		"Auth":         cfg.Auth,
		"Groups":       cfg.ContactGroups,
		"Lang":         lang,
		"Theme":        getTheme(r),
		"Version":      version,
		"Flash":        flash,
		"FlashType":    flashType,
		"AllNotifiers": flattenNotifiers(cfg),
		"I18nStrings":  buildJSI18n(lang),
	}
	h.tmpl.Render(w, "settings.html", data)
}

// renderSettingsWithError returns an error to the settings page.
// For AJAX requests it returns JSON; otherwise it re-renders the page with a flash.
func (h *Handlers) renderSettingsWithError(w http.ResponseWriter, r *http.Request, msg string) {
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "message": msg})
		return
	}
	cfg := h.cfgMgr.Get()
	lang := getLang(r)
	data := map[string]interface{}{
		"System":       cfg.System,
		"Auth":         cfg.Auth,
		"Groups":       cfg.ContactGroups,
		"Lang":         lang,
		"Theme":        getTheme(r),
		"Version":      version,
		"Flash":        msg,
		"FlashType":    "error",
		"AllNotifiers": flattenNotifiers(cfg),
		"I18nStrings":  buildJSI18n(lang),
	}
	h.tmpl.Render(w, "settings.html", data)
}

// SaveSystem handles saving system settings.
func (h *Handlers) SaveSystem(w http.ResponseWriter, r *http.Request) {
	lang := getLang(r)
	if err := r.ParseForm(); err != nil {
		h.renderSettingsWithError(w, r, translate(lang, "settings.error_invalid_form"))
		return
	}

	cfg := h.cfgMgr.Get()

	bindHost := r.FormValue("bind_host")
	bindPort := r.FormValue("bind_port")
	if bindHost == "" {
		cfg.System.BindAddress = ":" + bindPort
	} else {
		cfg.System.BindAddress = bindHost + ":" + bindPort
	}
	cfg.System.CheckInterval = formInt(r, "check_interval", 60)
	cfg.System.MaxHistoryPoints = formInt(r, "max_history_points", 1440)
	cfg.System.DumpInterval = formInt(r, "dump_interval", 300)
	cfg.System.SessionTTL = formInt(r, "session_ttl", 86400)
	cfg.System.LogLevel = r.FormValue("log_level")
	cfg.System.MaxMonitors = formInt(r, "max_monitors", 500)
	cfg.System.Timezone = r.FormValue("timezone")

	if err := h.cfgMgr.Save(cfg); err != nil {
		slog.Error("failed to save system settings", "error", err)
		h.renderSettingsWithError(w, r, translate(lang, "settings.error_save_failed")+": "+err.Error())
		return
	}

	slog.Info("system settings saved")
	http.Redirect(w, r, "/settings?saved=1", http.StatusSeeOther)
}

// SaveAuth handles saving authentication settings.
func (h *Handlers) SaveAuth(w http.ResponseWriter, r *http.Request) {
	lang := getLang(r)
	if err := r.ParseForm(); err != nil {
		h.renderSettingsWithError(w, r, translate(lang, "settings.error_invalid_form"))
		return
	}

	cfg := h.cfgMgr.Get()

	newUsername := r.FormValue("username")
	newPassword := r.FormValue("new_password")
	confirmPassword := r.FormValue("confirm_password")

	if newUsername != "" {
		cfg.Auth.Username = newUsername
	}

	if newPassword != "" {
		if newPassword != confirmPassword {
			h.renderSettingsWithError(w, r, translate(lang, "settings.password_mismatch"))
			return
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
		if err != nil {
			slog.Error("failed to hash password", "error", err)
			h.renderSettingsWithError(w, r, translate(lang, "settings.error_internal")+": "+err.Error())
			return
		}
		cfg.Auth.PasswordHash = string(hash)
	}

	if err := h.cfgMgr.Save(cfg); err != nil {
		slog.Error("failed to save auth settings", "error", err)
		h.renderSettingsWithError(w, r, translate(lang, "settings.error_save_failed")+": "+err.Error())
		return
	}

	slog.Info("auth settings saved", "username", cfg.Auth.Username)
	http.Redirect(w, r, "/settings?saved=1", http.StatusSeeOther)
}

// SaveSSO handles saving SSO settings.
func (h *Handlers) SaveSSO(w http.ResponseWriter, r *http.Request) {
	lang := getLang(r)
	if err := r.ParseForm(); err != nil {
		h.renderSettingsWithError(w, r, translate(lang, "settings.error_invalid_form"))
		return
	}

	cfg := h.cfgMgr.Get()

	cfg.Auth.SSO.Enabled = r.FormValue("sso_enabled") == "on"

	if err := h.cfgMgr.Save(cfg); err != nil {
		slog.Error("failed to save SSO settings", "error", err)
		h.renderSettingsWithError(w, r, translate(lang, "settings.error_save_failed")+": "+err.Error())
		return
	}

	slog.Info("SSO settings saved", "enabled", cfg.Auth.SSO.Enabled)
	http.Redirect(w, r, "/settings?saved=1", http.StatusSeeOther)
}

// CreateGroup handles creating a new contact group.
func (h *Handlers) CreateGroup(w http.ResponseWriter, r *http.Request) {
	lang := getLang(r)
	if err := r.ParseForm(); err != nil {
		h.renderSettingsWithError(w, r, translate(lang, "settings.error_invalid_form"))
		return
	}

	cfg := h.cfgMgr.Get()

	name := r.FormValue("group_name")
	if name == "" {
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}

	id := generateToken()[:8]
	cfg.ContactGroups[id] = config.ContactGroup{
		ID:   id,
		Name: name,
	}

	if err := h.cfgMgr.Save(cfg); err != nil {
		slog.Error("failed to save contact group", "error", err)
		h.renderSettingsWithError(w, r, translate(lang, "settings.error_save_failed")+": "+err.Error())
		return
	}

	slog.Info("contact group created", "id", id, "name", name)
	http.Redirect(w, r, "/settings?saved=1", http.StatusSeeOther)
}

// DeleteGroup handles deleting a contact group.
func (h *Handlers) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	lang := getLang(r)
	id := r.FormValue("group_id")
	if id == "" {
		h.renderSettingsWithError(w, r, translate(lang, "settings.error_missing_id"))
		return
	}

	cfg := h.cfgMgr.Get()

	if _, ok := cfg.ContactGroups[id]; !ok {
		h.renderSettingsWithError(w, r, translate(lang, "settings.error_not_found"))
		return
	}

	// Clear group_id references from monitors
	for i := range cfg.Monitors {
		if cfg.Monitors[i].GroupID == id {
			cfg.Monitors[i].GroupID = ""
		}
	}

	delete(cfg.ContactGroups, id)

	if err := h.cfgMgr.Save(cfg); err != nil {
		slog.Error("failed to delete contact group", "error", err)
		h.renderSettingsWithError(w, r, translate(lang, "settings.error_save_failed")+": "+err.Error())
		return
	}

	slog.Info("contact group deleted", "id", id)
	http.Redirect(w, r, "/settings?saved=1", http.StatusSeeOther)
}

// AddNotifierFlat adds a notifier to the top-level notifier list.
func (h *Handlers) AddNotifierFlat(w http.ResponseWriter, r *http.Request) {
	lang := getLang(r)
	if err := r.ParseForm(); err != nil {
		h.renderSettingsWithError(w, r, translate(lang, "settings.error_invalid_form"))
		return
	}

	nType := r.FormValue("type")
	cfg := h.cfgMgr.Get()

	nID := generateToken()[:8]
	remark := r.FormValue("remark")
	var nc config.NotifierConfig
	switch nType {
	case "telegram":
		nc = config.NotifierConfig{
			ID:       nID,
			Type:     "telegram",
			Remark:   remark,
			BotToken: r.FormValue("bot_token"),
			ChatID:   r.FormValue("chat_id"),
		}
		if nc.BotToken == "" || nc.ChatID == "" {
			h.renderSettingsWithError(w, r, translate(lang, "settings.error_missing_fields"))
			return
		}
	case "webhook":
		method := r.FormValue("webhook_method")
		if method == "" {
			method = "POST"
		}
		nc = config.NotifierConfig{
			ID:     nID,
			Type:   "webhook",
			Remark: remark,
			URL:    r.FormValue("webhook_url"),
			Method: method,
		}
		if nc.URL == "" {
			h.renderSettingsWithError(w, r, translate(lang, "settings.error_missing_fields"))
			return
		}
	default:
		h.renderSettingsWithError(w, r, translate(lang, "settings.error_invalid_type"))
		return
	}

	cfg.Notifiers = append(cfg.Notifiers, nc)

	if err := h.cfgMgr.Save(cfg); err != nil {
		slog.Error("failed to add notifier", "error", err)
		h.renderSettingsWithError(w, r, translate(lang, "settings.error_save_failed")+": "+err.Error())
		return
	}

	slog.Info("notifier added", "id", nID, "type", nType)
	http.Redirect(w, r, "/settings?saved=1", http.StatusSeeOther)
}

// DeleteNotifierByID removes a notifier by its ID from any contact group.
func (h *Handlers) DeleteNotifierByID(w http.ResponseWriter, r *http.Request) {
	lang := getLang(r)
	if err := r.ParseForm(); err != nil {
		h.renderSettingsWithError(w, r, translate(lang, "settings.error_invalid_form"))
		return
	}

	nID := r.FormValue("notifier_id")
	if nID == "" {
		h.renderSettingsWithError(w, r, translate(lang, "settings.error_missing_id"))
		return
	}

	cfg := h.cfgMgr.Get()
	found := false
	for i, nc := range cfg.Notifiers {
		if nc.ID == nID {
			cfg.Notifiers = append(cfg.Notifiers[:i], cfg.Notifiers[i+1:]...)
			found = true
			break
		}
	}

	if !found {
		h.renderSettingsWithError(w, r, translate(lang, "settings.error_not_found"))
		return
	}

	// Also remove from any monitor's notifier_ids
	for i := range cfg.Monitors {
		filtered := make([]string, 0, len(cfg.Monitors[i].NotifierIDs))
		for _, id := range cfg.Monitors[i].NotifierIDs {
			if id != nID {
				filtered = append(filtered, id)
			}
		}
		cfg.Monitors[i].NotifierIDs = filtered
	}

	if err := h.cfgMgr.Save(cfg); err != nil {
		slog.Error("failed to delete notifier", "error", err)
		h.renderSettingsWithError(w, r, translate(lang, "settings.error_save_failed")+": "+err.Error())
		return
	}

	slog.Info("notifier deleted", "id", nID)
	http.Redirect(w, r, "/settings?saved=1", http.StatusSeeOther)
}

// ToggleMonitor toggles a monitor's enabled state.
func (h *Handlers) ToggleMonitor(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cfg := h.cfgMgr.Get()

	idx := -1
	for i := range cfg.Monitors {
		if cfg.Monitors[i].ID == id {
			idx = i
			break
		}
	}

	if idx == -1 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		return
	}

	newState := !cfg.Monitors[idx].IsEnabled()
	cfg.Monitors[idx].Enabled = &newState

	if err := h.cfgMgr.Save(cfg); err != nil {
		slog.Error("failed to toggle monitor", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to save"})
		return
	}

	slog.Info("monitor toggled", "id", id, "enabled", newState)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"enabled": newState})
}

func flattenNotifiers(cfg config.Config) []notifierInfo {
	result := make([]notifierInfo, 0, len(cfg.Notifiers))
	for _, nc := range cfg.Notifiers {
		label := nc.Type
		switch nc.Type {
		case "telegram":
			label = "Telegram: " + nc.ChatID
		case "webhook":
			label = "Webhook: " + nc.URL
		}
		result = append(result, notifierInfo{
			ID:       nc.ID,
			Type:     nc.Type,
			Label:    label,
			Remark:   nc.Remark,
			BotToken: nc.BotToken,
			ChatID:   nc.ChatID,
			URL:      nc.URL,
			Method:   nc.Method,
		})
	}
	return result
}

func formInt(r *http.Request, key string, defaultVal int) int {
	val := r.FormValue(key)
	if val == "" {
		return defaultVal
	}
	var n int
	if _, err := fmt.Sscanf(val, "%d", &n); err != nil {
		return defaultVal
	}
	return n
}

// UpdateNotifier updates an existing notifier by ID.
func (h *Handlers) UpdateNotifier(w http.ResponseWriter, r *http.Request) {
	lang := getLang(r)
	if err := r.ParseForm(); err != nil {
		h.renderSettingsWithError(w, r, translate(lang, "settings.error_invalid_form"))
		return
	}

	nID := r.FormValue("notifier_id")
	nType := r.FormValue("type")
	if nID == "" || nType == "" {
		h.renderSettingsWithError(w, r, translate(lang, "settings.error_missing_fields"))
		return
	}

	cfg := h.cfgMgr.Get()
	idx := -1
	for i, nc := range cfg.Notifiers {
		if nc.ID == nID {
			idx = i
			break
		}
	}

	if idx == -1 {
		h.renderSettingsWithError(w, r, translate(lang, "settings.error_not_found"))
		return
	}

	cfg.Notifiers[idx].Type = nType
	cfg.Notifiers[idx].Remark = r.FormValue("remark")
	switch nType {
	case "telegram":
		cfg.Notifiers[idx].BotToken = r.FormValue("bot_token")
		cfg.Notifiers[idx].ChatID = r.FormValue("chat_id")
		cfg.Notifiers[idx].URL = ""
		cfg.Notifiers[idx].Method = ""
	case "webhook":
		method := r.FormValue("webhook_method")
		if method == "" {
			method = "POST"
		}
		cfg.Notifiers[idx].URL = r.FormValue("webhook_url")
		cfg.Notifiers[idx].Method = method
		cfg.Notifiers[idx].BotToken = ""
		cfg.Notifiers[idx].ChatID = ""
	}

	if err := h.cfgMgr.Save(cfg); err != nil {
		slog.Error("failed to update notifier", "error", err)
		h.renderSettingsWithError(w, r, translate(lang, "settings.error_save_failed")+": "+err.Error())
		return
	}

	slog.Info("notifier updated", "id", nID, "type", nType)
	http.Redirect(w, r, "/settings?saved=1", http.StatusSeeOther)
}

// TestNotifier sends a test notification via the specified notifier.
func (h *Handlers) TestNotifier(w http.ResponseWriter, r *http.Request) {
	nID := chi.URLParam(r, "id")
	cfg := h.cfgMgr.Get()

	var nc *config.NotifierConfig
	for i := range cfg.Notifiers {
		if cfg.Notifiers[i].ID == nID {
			nc = &cfg.Notifiers[i]
			break
		}
	}

	if nc == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": "notifier not found"})
		return
	}

	notifier := notify.BuildNotifier(*nc)
	if notifier == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": "unknown notifier type"})
		return
	}

	event := notify.AlertEvent{
		MonitorName: "Test",
		Type:        "up",
		Target:      "https://example.com",
		Reason:      "This is a test notification from Wink",
		Timestamp:   time.Now().Unix(),
		Timezone:    cfg.System.Timezone,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := notifier.Send(ctx, event); err != nil {
		slog.Error("test notification failed", "notifier_id", nID, "error", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

// TelegramGetUpdates fetches recent chats from the Telegram getUpdates API.
func (h *Handlers) TelegramGetUpdates(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BotToken string `json:"bot_token"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil || req.BotToken == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "bot_token required"})
		return
	}

	apiURL := "https://api.telegram.org/bot" + req.BotToken + "/getUpdates"
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"chats": []interface{}{}, "error": err.Error()})
		return
	}
	defer resp.Body.Close()

	var tgResp struct {
		OK     bool `json:"ok"`
		Result []struct {
			Message *struct {
				Chat struct {
					ID        int64  `json:"id"`
					Title     string `json:"title"`
					Type      string `json:"type"`
					FirstName string `json:"first_name"`
					LastName  string `json:"last_name"`
					Username  string `json:"username"`
				} `json:"chat"`
				Text string `json:"text"`
			} `json:"message"`
		} `json:"result"`
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tgResp); err != nil || !tgResp.OK {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"chats": []interface{}{}})
		return
	}

	type chatInfo struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Type    string `json:"type"`
		Message string `json:"message"`
	}

	seen := make(map[int64]bool)
	var chats []chatInfo
	// Iterate in reverse so newest messages come first
	for i := len(tgResp.Result) - 1; i >= 0; i-- {
		u := tgResp.Result[i]
		if u.Message == nil {
			continue
		}
		cid := u.Message.Chat.ID
		if seen[cid] {
			continue
		}
		seen[cid] = true
		title := u.Message.Chat.Title
		if title == "" {
			name := u.Message.Chat.FirstName
			if u.Message.Chat.LastName != "" {
				name += " " + u.Message.Chat.LastName
			}
			if name != "" {
				title = name
			} else if u.Message.Chat.Username != "" {
				title = "@" + u.Message.Chat.Username
			} else {
				title = fmt.Sprintf("Chat %d", cid)
			}
		}
		msg := u.Message.Text
		if len(msg) > 30 {
			msg = msg[:30] + "..."
		}
		chats = append(chats, chatInfo{
			ID:      fmt.Sprintf("%d", cid),
			Title:   title,
			Type:    u.Message.Chat.Type,
			Message: msg,
		})
	}

	// Limit to 5 most recent chats
	if len(chats) > 5 {
		chats = chats[:5]
	}

	if chats == nil {
		chats = []chatInfo{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"chats": chats})
}

// CheckUpdate checks GitHub for the latest release and caches the result for 1 hour.
var (
	updateCache     map[string]interface{}
	updateCacheTime time.Time
	updateCacheMu   sync.Mutex
)

func (h *Handlers) CheckUpdate(w http.ResponseWriter, r *http.Request) {
	updateCacheMu.Lock()
	if updateCache != nil && time.Since(updateCacheTime) < time.Hour {
		cached := updateCache
		updateCacheMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cached)
		return
	}
	updateCacheMu.Unlock()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/makt28/wink/releases/latest")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"current": version})
		return
	}
	defer resp.Body.Close()

	var gh struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&gh); err != nil || gh.TagName == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"current": version})
		return
	}

	latest := strings.TrimPrefix(gh.TagName, "v")
	hasUpdate := latest != version

	result := map[string]interface{}{
		"current":    version,
		"latest":     latest,
		"has_update": hasUpdate,
	}

	updateCacheMu.Lock()
	updateCache = result
	updateCacheTime = time.Now()
	updateCacheMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
