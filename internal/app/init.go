package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
)

// RunGooglePhotosInit sets up Google Photos credentials for the application.
func RunGooglePhotosInit(ctx context.Context, cfg *InitConfig) error {
	if cfg.GooglePhotos.TokenSecret == "" {
		// We need a RunConfig-like struct or just adapt buildOAuthConfig to take InitConfig.
		// Actually, InitConfig has both MyBrightDay and GooglePhotos, so we can just use it.
		// However, buildOAuthConfig and getOAuthClient are currently typed to *Config.
		// Let's change them to use an interface or a more generic type if possible.
		// Or just create a temporary RunConfig.
		fullCfg := &RunConfig{
			MyBrightDay:  cfg.MyBrightDay,
			GooglePhotos: cfg.GooglePhotos,
		}
		tok, err := PerformInitAuth(ctx, fullCfg)
		if err != nil {
			return fmt.Errorf("google photos authentication: %w", err)
		}
		data, err := json.Marshal(tok)
		if err != nil {
			return fmt.Errorf("serializing google token: %w", err)
		}
		if err := os.WriteFile("google_photos_token_secret", data, 0600); err != nil {
			return fmt.Errorf("saving google token: %w", err)
		}
		slog.Info("Google token saved to google_photos_token_secret")
	} else {
		slog.Info("Google token already configured")
	}

	return nil
}
