package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/dipesh/daycare-photos/internal/credential"
)

// buildOAuthConfig returns an OAuth2 config using a client_secret.json file if configured,
// otherwise falls back to the obfuscated credentials embedded in the binary.
func buildOAuthConfig(cfg *Config) (*oauth2.Config, error) {
	if cfg.Auth.ClientSecretFile != "" {
		data, err := os.ReadFile(cfg.Auth.ClientSecretFile)
		if err != nil {
			return nil, fmt.Errorf("reading client secret file: %w", err)
		}
		oauthCfg, err := google.ConfigFromJSON(data, oauthScopes...)
		if err != nil {
			return nil, fmt.Errorf("parsing client secret file: %w", err)
		}
		return oauthCfg, nil
	}
	return &oauth2.Config{
		ClientID:     credential.ClientID,
		ClientSecret: credential.ClientSecret(),
		Endpoint:     google.Endpoint,
		Scopes:       oauthScopes,
	}, nil
}

// oauthScopes defines the Google API scopes needed by this tool.
var oauthScopes = []string{
	"https://www.googleapis.com/auth/gmail.readonly",
	"https://www.googleapis.com/auth/photoslibrary.appendonly",
	"https://www.googleapis.com/auth/photoslibrary.readonly.appcreateddata",
}

// getOAuthClient creates an authenticated HTTP client using OAuth2.
// It requires a valid token to exist, otherwise it returns an error.
func getOAuthClient(ctx context.Context, cfg *Config) (*http.Client, error) {
	oauthCfg, err := buildOAuthConfig(cfg)
	if err != nil {
		return nil, err
	}

	tok, err := loadToken(cfg.Auth.TokenFile)
	if err != nil {
		binaryName := filepath.Base(os.Args[0])
		return nil, fmt.Errorf("token file missing or invalid. Please run '%s init' to authenticate", binaryName)
	}

	return oauthCfg.Client(ctx, tok), nil
}

// PerformInitAuth performs the interactive authorization flow and saves the token.
// If a token already exists, it logs this but proceeds to obtain a new one.
func PerformInitAuth(ctx context.Context, cfg *Config) error {
	oauthCfg, err := buildOAuthConfig(cfg)
	if err != nil {
		return err
	}

	if _, err := loadToken(cfg.Auth.TokenFile); err == nil {
		slog.Info("Token file already exists. Proceeding to obtain a new one...", "path", cfg.Auth.TokenFile)
	}

	tok, err := getTokenFromWeb(ctx, oauthCfg)
	if err != nil {
		return fmt.Errorf("obtaining token: %w", err)
	}
	if err := saveToken(cfg.Auth.TokenFile, tok); err != nil {
		return fmt.Errorf("saving token: %w", err)
	}

	slog.Info("Token successfully saved", "path", cfg.Auth.TokenFile)
	return nil
}

// getTokenFromWeb performs the OAuth2 authorization code flow by starting a temporary
// local server and automatically opening the user's browser.
func getTokenFromWeb(ctx context.Context, config *oauth2.Config) (*oauth2.Token, error) {
	// Find an available local port.
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, fmt.Errorf("starting local listener: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	config.RedirectURL = fmt.Sprintf("http://localhost:%d", port)

	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				w.WriteHeader(http.StatusNotFound)
				return
			}

			code := r.URL.Query().Get("code")
			if code == "" {
				errStr := r.URL.Query().Get("error")
				select {
				case errChan <- fmt.Errorf("google returned error: %s", errStr):
				default:
				}
				fmt.Fprint(w, "Authentication failed. You can close this window.")
				return
			}

			select {
			case codeChan <- code:
			default:
			}
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "Authentication successful! You can close this window now.")
		}),
	}

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			select {
			case errChan <- fmt.Errorf("http server error: %w", err):
			default:
			}
		}
	}()
	defer func() {
		// Use a very short timeout for shutdown to avoid visible delay.
		// For this local one-shot server, we don't need a long graceful period.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	slog.Info("Opening browser for authorization...")
	if err := openBrowser(authURL); err != nil {
		slog.Error("Failed to open browser", "error", err)
		fmt.Printf("Please open this URL manually:\n\n%s\n\n", authURL)
	}

	select {
	case code := <-codeChan:
		tok, err := config.Exchange(ctx, code)
		if err == nil {
			slog.Info("Successfully authenticated and retrieved token!")
		}
		return tok, err
	case err := <-errChan:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("timeout waiting for authorization")
	}
}

// openBrowser opens the specified URL in the user's default browser.
func openBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default: // Linux and others
		cmd = "xdg-open"
		args = []string{url}
	}

	return exec.Command(cmd, args...).Start()
}

// loadToken reads an OAuth2 token from a JSON file.
func loadToken(path string) (*oauth2.Token, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	tok := &oauth2.Token{}
	if err := json.NewDecoder(f).Decode(tok); err != nil {
		return nil, fmt.Errorf("decoding token: %w", err)
	}

	return tok, nil
}

// saveToken writes an OAuth2 token to a JSON file with restricted permissions.
func saveToken(path string, token *oauth2.Token) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("creating token file: %w", err)
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(token); err != nil {
		return fmt.Errorf("encoding token: %w", err)
	}

	return nil
}
