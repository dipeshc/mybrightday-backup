package googlephotos

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/oauth2"

	"github.com/dipeshc/mybrightday-backup/internal/httpx"
	"github.com/dipeshc/mybrightday-backup/internal/storage/googlephotos/credential"
)

// installedClientSecretJSON builds a client_secret.json whose token endpoint
// points at the given URL, keeping the OAuth exchange entirely local.
func installedClientSecretJSON(tokenURL string) string {
	return fmt.Sprintf(`{
		"installed": {
			"client_id": "test-client-id",
			"client_secret": "test-client-secret",
			"redirect_uris": ["http://localhost"],
			"auth_uri": "https://accounts.google.test/o/oauth2/auth",
			"token_uri": %q
		}
	}`, tokenURL)
}

func TestBuildOAuthConfig(t *testing.T) {
	t.Run("from client secret json", func(t *testing.T) {
		cfg, err := buildOAuthConfig(Config{ClientSecret: installedClientSecretJSON("https://token.test")})
		if err != nil {
			t.Fatalf("buildOAuthConfig: %v", err)
		}
		if cfg.ClientID != "test-client-id" || cfg.Endpoint.TokenURL != "https://token.test" {
			t.Errorf("config = %+v", cfg)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		_, err := buildOAuthConfig(Config{ClientSecret: "{broken"})
		if err == nil || !strings.Contains(err.Error(), "parsing client secret") {
			t.Errorf("err = %v, want parsing client secret", err)
		}
	})

	t.Run("embedded credential fallback", func(t *testing.T) {
		cfg, err := buildOAuthConfig(Config{})
		if err != nil {
			t.Fatalf("buildOAuthConfig: %v", err)
		}
		if cfg.ClientID != credential.ClientID {
			t.Errorf("ClientID = %q, want embedded default", cfg.ClientID)
		}
		if len(cfg.Scopes) != len(oauthScopes) {
			t.Errorf("scopes = %v", cfg.Scopes)
		}
	})
}

func TestGetOAuthClient(t *testing.T) {
	t.Run("missing refresh token", func(t *testing.T) {
		_, err := getOAuthClient(context.Background(), Config{})
		if err == nil || !strings.Contains(err.Error(), "refresh_token missing") {
			t.Errorf("err = %v, want refresh_token missing", err)
		}
	})

	t.Run("wraps transport with retries", func(t *testing.T) {
		client, err := getOAuthClient(context.Background(), Config{RefreshToken: "rt"})
		if err != nil {
			t.Fatalf("getOAuthClient: %v", err)
		}
		if _, ok := client.Transport.(*httpx.RetryTransport); !ok {
			t.Errorf("transport = %T, want *httpx.RetryTransport", client.Transport)
		}
	})
}

// stubBrowser replaces the browser hook with fn and restores it afterwards.
// Tests using it must not run in parallel.
func stubBrowser(t *testing.T, fn func(url string) error) {
	t.Helper()
	original := openBrowserFn
	openBrowserFn = fn
	t.Cleanup(func() { openBrowserFn = original })
}

// completeAuthInBrowser simulates the user approving access: it parses the
// auth URL the flow would open and calls the local redirect endpoint back.
func completeAuthInBrowser(t *testing.T, query string) func(string) error {
	t.Helper()
	return func(authURL string) error {
		u, err := url.Parse(authURL)
		if err != nil {
			t.Errorf("parsing auth URL: %v", err)
			return nil
		}
		redirect := u.Query().Get("redirect_uri")
		if redirect == "" {
			t.Error("auth URL missing redirect_uri")
			return nil
		}
		go func() {
			resp, err := http.Get(redirect + "/?" + query)
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}
}

func TestGetTokenFromWeb(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseForm(); err != nil {
				t.Errorf("parsing token form: %v", err)
			}
			if got := r.PostForm.Get("code"); got != "test-code" {
				t.Errorf("code = %q, want test-code", got)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "at",
				"refresh_token": "rt",
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
		}))
		defer tokenServer.Close()

		stubBrowser(t, completeAuthInBrowser(t, "code=test-code"))

		cfg := &oauth2.Config{
			ClientID:     "id",
			ClientSecret: "sec",
			Endpoint:     oauth2.Endpoint{AuthURL: "https://auth.test/auth", TokenURL: tokenServer.URL},
		}
		tok, err := getTokenFromWeb(context.Background(), cfg)
		if err != nil {
			t.Fatalf("getTokenFromWeb: %v", err)
		}
		if tok.RefreshToken != "rt" {
			t.Errorf("refresh token = %q, want rt", tok.RefreshToken)
		}
	})

	t.Run("user denies access", func(t *testing.T) {
		stubBrowser(t, completeAuthInBrowser(t, "error=access_denied"))

		cfg := &oauth2.Config{Endpoint: oauth2.Endpoint{AuthURL: "https://auth.test/auth", TokenURL: "https://unused.test"}}
		_, err := getTokenFromWeb(context.Background(), cfg)
		if err == nil || !strings.Contains(err.Error(), "access_denied") {
			t.Errorf("err = %v, want access_denied", err)
		}
	})

	t.Run("context cancelled", func(t *testing.T) {
		// A failing browser hook also exercises the manual-URL fallback path.
		stubBrowser(t, func(string) error { return fmt.Errorf("no browser") })

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		cfg := &oauth2.Config{Endpoint: oauth2.Endpoint{AuthURL: "https://auth.test/auth", TokenURL: "https://unused.test"}}
		_, err := getTokenFromWeb(ctx, cfg)
		if err != context.Canceled {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	})
}

func TestPerformInitAuth(t *testing.T) {
	t.Run("invalid client secret", func(t *testing.T) {
		_, err := PerformInitAuth(context.Background(), Config{ClientSecret: "{broken"})
		if err == nil || !strings.Contains(err.Error(), "parsing client secret") {
			t.Errorf("err = %v, want parsing client secret", err)
		}
	})

	t.Run("missing refresh token in response", func(t *testing.T) {
		tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"access_token": "at",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		}))
		defer tokenServer.Close()

		stubBrowser(t, completeAuthInBrowser(t, "code=test-code"))

		_, err := PerformInitAuth(context.Background(), Config{ClientSecret: installedClientSecretJSON(tokenServer.URL)})
		if err == nil || !strings.Contains(err.Error(), "refresh token") {
			t.Errorf("err = %v, want refresh token error", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "at",
				"refresh_token": "rt",
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
		}))
		defer tokenServer.Close()

		stubBrowser(t, completeAuthInBrowser(t, "code=test-code"))

		tok, err := PerformInitAuth(context.Background(), Config{ClientSecret: installedClientSecretJSON(tokenServer.URL)})
		if err != nil {
			t.Fatalf("PerformInitAuth: %v", err)
		}
		if tok.RefreshToken != "rt" {
			t.Errorf("refresh token = %q, want rt", tok.RefreshToken)
		}
	})
}

// stubInitAuth replaces the interactive auth hook and restores it afterwards.
func stubInitAuth(t *testing.T, tok *oauth2.Token, err error) {
	t.Helper()
	original := performInitAuthFn
	performInitAuthFn = func(ctx context.Context, cfg Config) (*oauth2.Token, error) {
		return tok, err
	}
	t.Cleanup(func() { performInitAuthFn = original })
}

func TestInit(t *testing.T) {
	t.Run("already configured", func(t *testing.T) {
		stubInitAuth(t, nil, fmt.Errorf("must not be called"))
		if err := Init(context.Background(), Config{RefreshToken: "existing"}); err != nil {
			t.Errorf("Init = %v, want nil for already-configured", err)
		}
	})

	t.Run("saves refresh token", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("CONFIG_FILES_DIR", dir)
		stubInitAuth(t, &oauth2.Token{RefreshToken: "new-rt"}, nil)

		if err := Init(context.Background(), Config{}); err != nil {
			t.Fatalf("Init: %v", err)
		}

		path := filepath.Join(dir, "google_photos", "refresh_token")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading saved token: %v", err)
		}
		if string(data) != "new-rt" {
			t.Errorf("token = %q, want new-rt", data)
		}
		if runtime.GOOS != "windows" {
			info, _ := os.Stat(path)
			if info.Mode().Perm() != 0o600 {
				t.Errorf("token file mode = %v, want 0600", info.Mode().Perm())
			}
		}
	})

	t.Run("auth failure propagates", func(t *testing.T) {
		stubInitAuth(t, nil, fmt.Errorf("user closed the window"))
		err := Init(context.Background(), Config{})
		if err == nil || !strings.Contains(err.Error(), "user closed the window") {
			t.Errorf("err = %v, want auth failure", err)
		}
	})
}

func TestGenerateRandomState(t *testing.T) {
	a := generateRandomState()
	b := generateRandomState()
	if a == b {
		t.Error("two states are identical")
	}
	if len(a) != 32 {
		t.Errorf("len = %d, want 32 hex chars", len(a))
	}
}
