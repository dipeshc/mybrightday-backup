package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
)

// GooglePhotosInit sets up Google Photos credentials for the application.
func GooglePhotosInit(ctx context.Context, cfg *GooglePhotosInitConfig) error {
	if cfg.GooglePhotos.TokenSecret == "" {
		// We need a DownloadConfig-like struct or just adapt buildOAuthConfig to take GooglePhotosInitConfig.
		// Actually, GooglePhotosInitConfig has GooglePhotos, so we can just use it.
		// However, buildOAuthConfig and getOAuthClient are currently typed to *DownloadConfig.
		// Let's create a temporary DownloadConfig.
		fullCfg := &DownloadConfig{
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
