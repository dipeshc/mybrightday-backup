package googlephotos

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

	"github.com/dipesh/mybrightday-backup/internal/storage/googlephotos/credential"
)

// oauthScopes defines the Google API scopes needed by this tool.
var oauthScopes = []string{
	"https://www.googleapis.com/auth/photoslibrary.appendonly",
	"https://www.googleapis.com/auth/photoslibrary.readonly.appcreateddata",
}

// buildOAuthConfig returns an OAuth2 config using the client_secret JSON string if provided,
// otherwise falls back to the obfuscated credentials embedded in the binary.
func buildOAuthConfig(cfg Config) (*oauth2.Config, error) {
	if cfg.ClientSecret != "" {
		oauthCfg, err := google.ConfigFromJSON([]byte(cfg.ClientSecret), oauthScopes...)
		if err != nil {
			return nil, fmt.Errorf("parsing client secret JSON: %w", err)
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

// getOAuthClient creates an authenticated HTTP client using OAuth2.
// It requires a valid token to exist in cfg.TokenSecret.
func getOAuthClient(ctx context.Context, cfg Config) (*http.Client, error) {
	oauthCfg, err := buildOAuthConfig(cfg)
	if err != nil {
		return nil, err
	}

	if cfg.TokenSecret == "" {
		binaryName := filepath.Base(os.Args[0])
		return nil, fmt.Errorf("token_secret missing — run '%s google-photos init' to authenticate", binaryName)
	}

	tok := &oauth2.Token{}
	if err := json.Unmarshal([]byte(cfg.TokenSecret), tok); err != nil {
		return nil, fmt.Errorf("decoding token_secret JSON: %w", err)
	}

	return oauthCfg.Client(ctx, tok), nil
}

// PerformInitAuth performs the interactive authorization flow and returns the obtained token.
func PerformInitAuth(ctx context.Context, cfg Config) (*oauth2.Token, error) {
	oauthCfg, err := buildOAuthConfig(cfg)
	if err != nil {
		return nil, err
	}

	tok, err := getTokenFromWeb(ctx, oauthCfg)
	if err != nil {
		return nil, fmt.Errorf("obtaining token: %w", err)
	}

	return tok, nil
}

// Init performs the interactive Google Photos OAuth2 flow and saves the token to disk.
// The token is written to {CONFIG_FILES_DIR}/google_photos/token_secret, which matches
// the path that the config resolution system reads from automatically.
func Init(ctx context.Context, cfg Config) error {
	if cfg.TokenSecret != "" {
		slog.Info("Google token already configured")
		return nil
	}

	tok, err := PerformInitAuth(ctx, cfg)
	if err != nil {
		return fmt.Errorf("google photos authentication: %w", err)
	}
	data, err := json.Marshal(tok)
	if err != nil {
		return fmt.Errorf("serializing google token: %w", err)
	}

	configDir := os.Getenv("CONFIG_FILES_DIR")
	if configDir == "" {
		configDir = "config"
	}
	tokenPath := filepath.Join(configDir, "google_photos", "token_secret")
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	if err := os.WriteFile(tokenPath, data, 0600); err != nil {
		return fmt.Errorf("saving google token: %w", err)
	}
	slog.Info("Google token saved", "path", tokenPath)
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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	state := generateRandomState()
	authURL := config.AuthCodeURL(state, oauth2.AccessTypeOffline)
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

func generateRandomState() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "state-fallback"
	}
	return hex.EncodeToString(b)
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
