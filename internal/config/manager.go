package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// Manager handles loading, saving and broadcasting config changes.
type Manager struct {
	mu       sync.RWMutex
	cfg      Config
	filePath string

	subMu sync.Mutex
	subs  []chan struct{}
}

// NewManager creates a Manager and loads config from the given file path.
// If the file does not exist, a default config is used (but not persisted).
func NewManager(filePath string) (*Manager, error) {
	m := &Manager{
		filePath: filePath,
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		slog.Warn("config file not found, using defaults", "path", filePath)
		m.cfg = DefaultConfig()
		return m, nil
	}

	if err := m.load(); err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return m, nil
}

// Get returns a copy of the current config (safe for concurrent reads).
func (m *Manager) Get() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// Save validates, atomically writes config to disk, and broadcasts a change event.
func (m *Manager) Save(cfg Config) error {
	cfg.Version = CurrentConfigVersion
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.atomicWrite(cfg); err != nil {
		return fmt.Errorf("atomic write config: %w", err)
	}
	m.cfg = cfg

	// Broadcast to all subscribers
	m.subMu.Lock()
	for _, ch := range m.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	m.subMu.Unlock()

	return nil
}

// Subscribe returns a new channel that receives a signal whenever config is saved.
// Each subscriber gets its own channel so multiple goroutines can independently
// listen for changes.
func (m *Manager) Subscribe() <-chan struct{} {
	ch := make(chan struct{}, 1)
	m.subMu.Lock()
	m.subs = append(m.subs, ch)
	m.subMu.Unlock()
	return ch
}

func (m *Manager) load() error {
	data, err := os.ReadFile(m.filePath)
	if err != nil {
		return err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config JSON: %w", err)
	}

	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return err
	}

	m.cfg = cfg
	return nil
}

func (m *Manager) atomicWrite(cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(m.filePath)
	tmp, err := os.CreateTemp(dir, "config-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	defer func() {
		// Clean up temp file on failure
		if tmp != nil {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	tmp = nil // prevent cleanup from double-closing

	return os.Rename(tmpName, m.filePath)
}
