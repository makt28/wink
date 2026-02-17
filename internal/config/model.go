package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const CurrentConfigVersion = 1

// Config is the root configuration structure persisted in config.json.
type Config struct {
	Version       int                     `json:"version"`
	System        SystemConfig            `json:"system"`
	Auth          AuthConfig              `json:"auth"`
	ContactGroups map[string]ContactGroup `json:"contact_groups"`
	GroupOrder    []string                `json:"group_order,omitempty"`
	Notifiers     []NotifierConfig        `json:"notifiers"`
	Monitors      []Monitor               `json:"monitors"`
}

type SystemConfig struct {
	BindAddress      string `json:"bind_address"`
	CheckInterval    int    `json:"check_interval"`
	MaxHistoryPoints int    `json:"max_history_points"`
	DumpInterval     int    `json:"dump_interval"`
	SessionTTL       int    `json:"session_ttl"`
	LogLevel         string `json:"log_level"`
	MaxMonitors      int    `json:"max_monitors"`
	Timezone         string `json:"timezone,omitempty"`
}

type AuthConfig struct {
	Username         string    `json:"username"`
	PasswordHash     string    `json:"password_hash"`
	MaxLoginAttempts int       `json:"max_login_attempts"`
	LockoutDuration  int       `json:"lockout_duration"`
	SSO              SSOConfig `json:"sso"`
}

type SSOConfig struct {
	Enabled bool `json:"enabled"`
}

type ContactGroup struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Notifiers []NotifierConfig `json:"notifiers,omitempty"` // deprecated: migrated to top-level Notifiers
}

type NotifierConfig struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Remark   string `json:"remark,omitempty"`
	BotToken string `json:"bot_token,omitempty"`
	ChatID   string `json:"chat_id,omitempty"`
	URL      string `json:"url,omitempty"`
	Method   string `json:"method,omitempty"`
}

type Monitor struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Type             string   `json:"type"`
	Target           string   `json:"target"`
	GroupID          string   `json:"group_id"`
	Interval         int      `json:"interval"`
	Timeout          int      `json:"timeout"`
	MaxRetries       int      `json:"max_retries"`
	RetryInterval    int      `json:"retry_interval"`
	ReminderInterval int      `json:"reminder_interval"`
	IgnoreTLS        bool     `json:"ignore_tls"`
	Enabled          *bool    `json:"enabled,omitempty"`
	NotifierIDs      []string `json:"notifier_ids,omitempty"`
}

// IsEnabled returns whether the monitor is enabled (defaults to true).
func (m *Monitor) IsEnabled() bool {
	return m.Enabled == nil || *m.Enabled
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Version: CurrentConfigVersion,
		System: SystemConfig{
			BindAddress:      ":8080",
			CheckInterval:    60,
			MaxHistoryPoints: 1440,
			DumpInterval:     300,
			SessionTTL:       86400,
			LogLevel:         "info",
			MaxMonitors:      500,
			Timezone:         detectTimezone(),
		},
		Auth: AuthConfig{
			Username:         "admin",
			PasswordHash:     "$2a$10$8.FeSs3eopZT0s/fCTdMWuE8U4f/Dv.ERy10fqrb9QnpHNknp8i/q", // 123456
			MaxLoginAttempts: 5,
			LockoutDuration:  900,
		},
		ContactGroups: make(map[string]ContactGroup),
		Notifiers:     []NotifierConfig{},
		Monitors:      []Monitor{},
	}
}

// ApplyDefaults fills zero-value fields with defaults.
func (c *Config) ApplyDefaults() {
	d := DefaultConfig()
	if c.System.BindAddress == "" {
		c.System.BindAddress = d.System.BindAddress
	}
	if c.System.CheckInterval <= 0 {
		c.System.CheckInterval = d.System.CheckInterval
	}
	if c.System.MaxHistoryPoints <= 0 {
		c.System.MaxHistoryPoints = d.System.MaxHistoryPoints
	}
	if c.System.DumpInterval <= 0 {
		c.System.DumpInterval = d.System.DumpInterval
	}
	if c.System.SessionTTL <= 0 {
		c.System.SessionTTL = d.System.SessionTTL
	}
	if c.System.LogLevel == "" {
		c.System.LogLevel = d.System.LogLevel
	}
	if c.System.MaxMonitors <= 0 {
		c.System.MaxMonitors = d.System.MaxMonitors
	}
	if c.System.Timezone == "" {
		c.System.Timezone = detectTimezone()
	}
	if c.Auth.MaxLoginAttempts <= 0 {
		c.Auth.MaxLoginAttempts = d.Auth.MaxLoginAttempts
	}
	if c.Auth.LockoutDuration <= 0 {
		c.Auth.LockoutDuration = d.Auth.LockoutDuration
	}
	if c.ContactGroups == nil {
		c.ContactGroups = make(map[string]ContactGroup)
	}
	if c.Notifiers == nil {
		c.Notifiers = []NotifierConfig{}
	}
	if c.Monitors == nil {
		c.Monitors = []Monitor{}
	}
	// Migrate notifiers from contact groups to top-level (legacy format)
	for gid, group := range c.ContactGroups {
		if len(group.Notifiers) > 0 {
			c.Notifiers = append(c.Notifiers, group.Notifiers...)
			group.Notifiers = nil
			c.ContactGroups[gid] = group
		}
	}
	// Remove _default group (was only used for flat notifier storage)
	delete(c.ContactGroups, "_default")
	// Ensure all notifiers have IDs
	for i := range c.Notifiers {
		if c.Notifiers[i].ID == "" {
			c.Notifiers[i].ID = generateID()
		}
	}
	// Reconcile GroupOrder: remove stale IDs, append missing IDs
	if c.GroupOrder == nil {
		c.GroupOrder = make([]string, 0, len(c.ContactGroups))
		for id := range c.ContactGroups {
			c.GroupOrder = append(c.GroupOrder, id)
		}
	} else {
		existing := make(map[string]bool, len(c.ContactGroups))
		for id := range c.ContactGroups {
			existing[id] = true
		}
		// Remove stale IDs
		clean := make([]string, 0, len(c.GroupOrder))
		seen := make(map[string]bool, len(c.GroupOrder))
		for _, id := range c.GroupOrder {
			if existing[id] && !seen[id] {
				clean = append(clean, id)
				seen[id] = true
			}
		}
		// Append missing IDs
		for id := range c.ContactGroups {
			if !seen[id] {
				clean = append(clean, id)
			}
		}
		c.GroupOrder = clean
	}
}

// detectTimezone returns the system's IANA timezone name, falling back to "UTC".
func detectTimezone() string {
	name := time.Now().Location().String()
	if name == "" || name == "Local" {
		return "UTC"
	}
	return name
}

func generateID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Validate checks the config for logical errors.
func (c *Config) Validate() error {
	var errs []string

	if c.System.CheckInterval < 5 {
		errs = append(errs, "system.check_interval must be >= 5 seconds")
	}
	if c.Auth.Username == "" {
		errs = append(errs, "auth.username is required")
	}

	validLogLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLogLevels[c.System.LogLevel] {
		errs = append(errs, fmt.Sprintf("system.log_level must be one of: debug, info, warn, error (got %q)", c.System.LogLevel))
	}

	if len(c.Monitors) > c.System.MaxMonitors {
		errs = append(errs, fmt.Sprintf("monitors count (%d) exceeds max_monitors (%d)", len(c.Monitors), c.System.MaxMonitors))
	}

	seen := make(map[string]bool)
	for i, m := range c.Monitors {
		prefix := fmt.Sprintf("monitors[%d]", i)
		if m.ID == "" {
			errs = append(errs, prefix+".id is required")
		}
		if seen[m.ID] {
			errs = append(errs, prefix+".id is duplicate: "+m.ID)
		}
		seen[m.ID] = true

		if m.Name == "" {
			errs = append(errs, prefix+".name is required")
		}

		validTypes := map[string]bool{"http": true, "tcp": true, "ping": true}
		if !validTypes[m.Type] {
			errs = append(errs, fmt.Sprintf("%s.type must be http, tcp, or ping (got %q)", prefix, m.Type))
		}

		if m.Target == "" {
			errs = append(errs, prefix+".target is required")
		} else if m.Type == "http" {
			if u, err := url.Parse(m.Target); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
				errs = append(errs, prefix+".target must be a valid http(s) URL")
			}
		}

		if m.GroupID != "" {
			if _, ok := c.ContactGroups[m.GroupID]; !ok {
				errs = append(errs, fmt.Sprintf("%s.group_id references unknown contact group %q", prefix, m.GroupID))
			}
		}

		interval := m.Interval
		if interval <= 0 {
			interval = c.System.CheckInterval
		}
		if m.Timeout <= 0 {
			errs = append(errs, prefix+".timeout must be > 0")
		} else if m.Timeout >= interval {
			errs = append(errs, fmt.Sprintf("%s.timeout (%d) must be < interval (%d)", prefix, m.Timeout, interval))
		}

		if m.MaxRetries < 0 {
			errs = append(errs, prefix+".max_retries must be >= 0")
		}
		if m.RetryInterval < 0 {
			errs = append(errs, prefix+".retry_interval must be >= 0")
		}
		if m.ReminderInterval < 0 {
			errs = append(errs, prefix+".reminder_interval must be >= 0")
		}
	}

	if len(errs) > 0 {
		return errors.New("config validation failed:\n  " + strings.Join(errs, "\n  "))
	}
	return nil
}
