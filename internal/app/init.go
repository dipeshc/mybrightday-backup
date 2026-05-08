package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// RunInit sets up credentials for the application.
func RunInit(ctx context.Context, googlePhotos bool, cfg *InitConfig) error {
	if cfg.MyBrightDay.SessionCookieSecret == "" {
		fmt.Print("Enter your MyBrightDay session cookie (from browser DevTools): ")
		reader := bufio.NewReader(os.Stdin)
		cookie, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("reading input: %w", err)
		}
		cookie = strings.TrimSpace(cookie)
		if cookie == "" {
			return fmt.Errorf("session cookie cannot be empty")
		}

		if err := os.WriteFile("mybrightday_session_cookie_secret", []byte(cookie), 0600); err != nil {
			return fmt.Errorf("saving session cookie: %w", err)
		}
		slog.Info("Session cookie saved to mybrightday_session_cookie_secret")
	} else {
		slog.Info("MyBrightDay session cookie already configured")
	}

	if googlePhotos {
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
	}

	return nil
}
