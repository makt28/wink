package web

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/makt28/wink/internal/config"
)

var startTime = time.Now()

const version = "0.1.0"

// HealthHandler serves the /healthz endpoint.
type HealthHandler struct {
	cfgMgr *config.Manager
}

func NewHealthHandler(cfgMgr *config.Manager) *HealthHandler {
	return &HealthHandler{cfgMgr: cfgMgr}
}

func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cfg := h.cfgMgr.Get()
	resp := map[string]interface{}{
		"status":         "ok",
		"version":        version,
		"uptime_seconds": int(time.Since(startTime).Seconds()),
		"monitor_count":  len(cfg.Monitors),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
