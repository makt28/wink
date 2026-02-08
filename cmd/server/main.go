package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/makt/wink/internal/config"
	"github.com/makt/wink/internal/monitor"
	"github.com/makt/wink/internal/notify"
	"github.com/makt/wink/internal/storage"
	"github.com/makt/wink/internal/web"
)

func main() {
	// --- 1. Load Config ---
	storage.MigrateConfigFile("config.json")

	cfgMgr, err := config.NewManager("config.json")
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	cfg := cfgMgr.Get()

	// --- 2. Setup Logger ---
	setupLogger(cfg.System.LogLevel)
	slog.Info("starting Wink", "bind", cfg.System.BindAddress)

	// --- 3. Load History ---
	storage.MigrateHistoryFile("history.json")

	histMgr, err := storage.NewHistoryManager("history.json", "incidents.json", cfg.System.MaxHistoryPoints)
	if err != nil {
		slog.Error("failed to load history", "error", err)
		os.Exit(1)
	}

	// --- 4. Init Notification Router ---
	notifier := notify.NewRouter(cfgMgr)

	// --- 5. Init Analyzer & Scheduler ---
	analyzer := monitor.NewAnalyzer(histMgr, notifier)
	scheduler := monitor.NewScheduler(cfgMgr, analyzer)
	scheduler.Start()

	// --- 6. Start periodic history dump ---
	stopCh := make(chan struct{})
	go periodicDump(histMgr, time.Duration(cfg.System.DumpInterval)*time.Second, stopCh)

	// --- 7. HTTP Server ---
	router := web.NewRouter(cfgMgr, histMgr, stopCh)
	currentAddr := cfg.System.BindAddress
	srv := &http.Server{
		Addr:    currentAddr,
		Handler: router,
	}

	go func() {
		slog.Info("Wink is running", "address", currentAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// --- 8. Watch for bind address changes ---
	bindChange := cfgMgr.Subscribe()
	go func() {
		for {
			select {
			case <-stopCh:
				return
			case <-bindChange:
				newCfg := cfgMgr.Get()
				if newCfg.System.BindAddress != currentAddr {
					slog.Info("bind address changed, restarting listener",
						"old", currentAddr, "new", newCfg.System.BindAddress)
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					srv.Shutdown(ctx)
					cancel()
					currentAddr = newCfg.System.BindAddress
					srv = &http.Server{
						Addr:    currentAddr,
						Handler: router,
					}
					go func() {
						slog.Info("Wink is running", "address", currentAddr)
						if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
							slog.Error("server error", "error", err)
						}
					}()
				}
			}
		}
	}()

	// --- 9. Graceful Shutdown ---
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	slog.Info("received shutdown signal", "signal", sig)

	close(stopCh)
	scheduler.Stop()

	if err := histMgr.Dump(); err != nil {
		slog.Error("failed to dump history on shutdown", "error", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server forced shutdown", "error", err)
	}

	slog.Info("Wink stopped gracefully")
}

func setupLogger(level string) {
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})
	slog.SetDefault(slog.New(handler))
}

func periodicDump(histMgr *storage.HistoryManager, interval time.Duration, stopCh <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			if err := histMgr.Dump(); err != nil {
				slog.Error("periodic history dump failed", "error", err)
			} else {
				slog.Debug("periodic history dump complete")
			}
		}
	}
}
