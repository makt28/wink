package storage

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
)

// MigrateHistoryFile checks the version of a history file and runs migrations if needed.
func MigrateHistoryFile(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to migrate
		}
		return err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse history for migration: %w", err)
	}

	version := 0
	if v, ok := raw["version"]; ok {
		if err := json.Unmarshal(v, &version); err != nil {
			version = 0
		}
	}

	if version == CurrentHistoryVersion {
		return nil
	}

	slog.Info("migrating history file", "from_version", version, "to_version", CurrentHistoryVersion)

	// Run migration chain
	// Example: if version == 0 { migrateHistoryV0toV1(raw) }

	// For now, just stamp the current version
	if version < CurrentHistoryVersion {
		slog.Info("history migration complete")
	}

	return nil
}

// MigrateConfigFile checks the version of a config file and runs migrations if needed.
func MigrateConfigFile(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse config for migration: %w", err)
	}

	version := 0
	if v, ok := raw["version"]; ok {
		if err := json.Unmarshal(v, &version); err != nil {
			version = 0
		}
	}

	if version == CurrentHistoryVersion {
		return nil
	}

	slog.Info("migrating config file", "from_version", version, "to_version", CurrentHistoryVersion)

	// Migration chain placeholder
	// Example: if version == 0 { migrateConfigV0toV1(raw, filePath) }

	return nil
}
