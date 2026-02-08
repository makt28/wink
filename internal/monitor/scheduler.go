package monitor

import (
	"context"
	"log/slog"
	"reflect"
	"sync"
	"time"

	"github.com/makt/wink/internal/config"
)

type runningMonitor struct {
	cancel context.CancelFunc
	cfg    config.Monitor
}

// Scheduler manages one goroutine per monitor and reacts to config changes.
type Scheduler struct {
	cfgMgr   *config.Manager
	analyzer *Analyzer

	mu       sync.Mutex
	running  map[string]*runningMonitor
	wg       sync.WaitGroup
	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewScheduler creates a new Scheduler.
func NewScheduler(cfgMgr *config.Manager, analyzer *Analyzer) *Scheduler {
	return &Scheduler{
		cfgMgr:   cfgMgr,
		analyzer: analyzer,
		running:  make(map[string]*runningMonitor),
		stopCh:   make(chan struct{}),
	}
}

// Start launches monitor goroutines and listens for config changes.
func (s *Scheduler) Start() {
	cfg := s.cfgMgr.Get()
	s.syncMonitors(cfg)

	s.wg.Add(1)
	go s.watchChanges()
}

// Stop cancels all monitor goroutines and waits for them to finish.
func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)

		s.mu.Lock()
		for id, rm := range s.running {
			rm.cancel()
			delete(s.running, id)
		}
		s.mu.Unlock()

		s.wg.Wait()
	})
}

func (s *Scheduler) watchChanges() {
	defer s.wg.Done()

	onChange := s.cfgMgr.Subscribe()
	for {
		select {
		case <-s.stopCh:
			return
		case <-onChange:
			cfg := s.cfgMgr.Get()
			slog.Info("config changed, syncing monitors")
			s.syncMonitors(cfg)
		}
	}
}

// syncMonitors diffs running goroutines against config and starts/stops as needed.
func (s *Scheduler) syncMonitors(cfg config.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()

	desired := make(map[string]config.Monitor)
	for _, m := range cfg.Monitors {
		if m.IsEnabled() {
			desired[m.ID] = m
		}
	}

	// Stop monitors removed or changed
	for id, rm := range s.running {
		dm, ok := desired[id]
		if !ok {
			slog.Info("stopping removed monitor", "id", id)
			rm.cancel()
			delete(s.running, id)
			s.analyzer.RemoveState(id)
		} else if !reflect.DeepEqual(rm.cfg, dm) {
			slog.Info("restarting changed monitor", "id", id)
			rm.cancel()
			delete(s.running, id)
		}
	}

	// Start new or restarted monitors
	for id, m := range desired {
		if _, ok := s.running[id]; !ok {
			s.startMonitor(m, cfg.System.CheckInterval)
		}
	}
}

func (s *Scheduler) startMonitor(m config.Monitor, defaultInterval int) {
	ctx, cancel := context.WithCancel(context.Background())
	s.running[m.ID] = &runningMonitor{cancel: cancel, cfg: m}

	interval := m.Interval
	if interval <= 0 {
		interval = defaultInterval
	}
	retryInterval := m.RetryInterval
	if retryInterval <= 0 {
		retryInterval = interval
	}
	timeout := m.Timeout

	prober := NewProber(m.Type, m.IgnoreTLS)

	s.wg.Add(1)
	go func(m config.Monitor, normalInterval, retryInterval, timeout int) {
		defer s.wg.Done()
		slog.Info("monitor started", "id", m.ID, "name", m.Name, "type", m.Type, "interval", normalInterval)

		currentInterval := normalInterval

		// First probe immediately
		ar := s.runProbe(ctx, prober, m, timeout)
		if ar.IsFailing && retryInterval < normalInterval {
			currentInterval = retryInterval
		}

		timer := time.NewTimer(time.Duration(currentInterval) * time.Second)
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				slog.Info("monitor stopped", "id", m.ID, "name", m.Name)
				return
			case <-timer.C:
				ar := s.runProbe(ctx, prober, m, timeout)
				if ar.IsFailing && retryInterval < normalInterval {
					currentInterval = retryInterval
				} else {
					currentInterval = normalInterval
				}
				timer.Reset(time.Duration(currentInterval) * time.Second)
			}
		}
	}(m, interval, retryInterval, timeout)
}

func (s *Scheduler) runProbe(ctx context.Context, prober Prober, m config.Monitor, timeout int) AnalyzeResult {
	probeCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	result := prober.Probe(probeCtx, m.Target)
	return s.analyzer.Process(m.ID, m.Name, m.Target, m.MaxRetries, m.ReminderInterval, result)
}
