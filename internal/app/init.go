package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// RunInit creates a default config.yaml and performs the interactive Google OAuth flow.
func RunInit(ctx context.Context, configPath string) error {
	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return fmt.Errorf("resolving config path: %w", err)
	}

	if _, err := os.Stat(absConfigPath); err == nil {
		slog.Info("Configuration file already exists", "path", absConfigPath)
	} else {
		if err := os.WriteFile(absConfigPath, []byte(ExampleConfigYAML), 0644); err != nil {
			return fmt.Errorf("writing example config: %w", err)
		}
		slog.Info("Created example configuration file", "path", absConfigPath)
	}

	cfg, err := LoadConfig(absConfigPath)
	if err != nil {
		return fmt.Errorf("loading config for init: %w", err)
	}

	if err := PerformInitAuth(ctx, cfg); err != nil {
		return fmt.Errorf("performing init auth: %w", err)
	}
	return nil
}
