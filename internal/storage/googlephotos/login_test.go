package googlephotos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/dipeshc/mybrightday-backup/internal/oauthlogin"
)

// startGoogleTokenServer fakes Google's token endpoint and points the
// embedded OAuth config's endpoint at it.
type googleKnobs struct {
	status                            int
	errorCode                         string
	omitRefresh                       bool
	gotCode, gotVerifier, gotRedirect string
}

func startGoogleTokenServer(t *testing.T, knobs *googleKnobs) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		knobs.gotCode = r.PostForm.Get("code")
		knobs.gotVerifier = r.PostForm.Get("code_verifier")
		knobs.gotRedirect = r.PostForm.Get("redirect_uri")
		if knobs.status != 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(knobs.status)
			json.NewEncoder(w).Encode(map[string]string{"error": knobs.errorCode, "error_description": "bad"})
			return
		}
		resp := map[string]any{"access_token": "g-access", "token_type": "Bearer", "expires_in": 3600, "refresh_token": "g-refresh"}
		if knobs.omitRefresh {
			delete(resp, "refresh_token")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	orig := google.Endpoint
	google.Endpoint = oauth2.Endpoint{AuthURL: ts.URL + "/auth", TokenURL: ts.URL + "/token"}
	t.Cleanup(func() { google.Endpoint = orig })
	t.Setenv("CONFIG_FILES_DIR", t.TempDir())
}

func TestLoginURLUsesFixedRedirectAndPKCE(t *testing.T) {
	startGoogleTokenServer(t, &googleKnobs{})
	p := oauthlogin.NewPending(configSection)
	raw, err := LoginURL(Config{}, p)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(raw)
	q := u.Query()
	if q.Get("redirect_uri") != loginRedirectURI {
		t.Errorf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") != p.Challenge() {
		t.Errorf("PKCE wrong: %v", q)
	}
	if q.Get("state") != p.State || q.Get("access_type") != "offline" || q.Get("prompt") != "consent" {
		t.Errorf("params wrong: %v", q)
	}
	if strings.Contains(raw, p.Verifier) {
		t.Error("login URL leaks the verifier")
	}
	if !IsCallback("http://localhost:4201/google/callback?code=x&state=y") || IsCallback("http://localhost:4200/okta/callback?code=x") {
		t.Error("IsCallback routing wrong")
	}
}

func TestBeginThenFinishLogin(t *testing.T) {
	knobs := &googleKnobs{}
	startGoogleTokenServer(t, knobs)

	raw, err := BeginLogin(Config{})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(raw)
	state := u.Query().Get("state")

	if err := FinishLogin(context.Background(), Config{}, "http://localhost:4201/google/callback?code=g-code&state="+state); err != nil {
		t.Fatalf("FinishLogin: %v", err)
	}
	if knobs.gotCode != "g-code" || knobs.gotVerifier == "" || knobs.gotRedirect != loginRedirectURI {
		t.Errorf("exchange sent code=%q verifier present=%v redirect=%q", knobs.gotCode, knobs.gotVerifier != "", knobs.gotRedirect)
	}
	if got := LoadRefreshToken(); got != "g-refresh" {
		t.Errorf("saved token = %q", got)
	}
	info, _ := os.Stat(RefreshTokenPath())
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token file mode = %o, want 600", perm)
	}
	if RefreshTokenPath() != filepath.Join(os.Getenv("CONFIG_FILES_DIR"), "google_photos", "refresh_token") {
		t.Errorf("RefreshTokenPath = %q", RefreshTokenPath())
	}
	if _, err := oauthlogin.LoadPending(configSection); !errors.Is(err, oauthlogin.ErrNoPendingLogin) {
		t.Error("pending login still present after success")
	}
}

func TestFinishLoginFailures(t *testing.T) {
	t.Run("no pending login", func(t *testing.T) {
		startGoogleTokenServer(t, &googleKnobs{})
		err := FinishLogin(context.Background(), Config{}, "http://localhost:4201/google/callback?code=x&state=y")
		if !errors.Is(err, oauthlogin.ErrNoPendingLogin) {
			t.Errorf("err = %v, want ErrNoPendingLogin", err)
		}
	})
	t.Run("stale code", func(t *testing.T) {
		startGoogleTokenServer(t, &googleKnobs{status: http.StatusBadRequest, errorCode: "invalid_grant"})
		raw, _ := BeginLogin(Config{})
		u, _ := url.Parse(raw)
		err := FinishLogin(context.Background(), Config{}, "http://localhost:4201/google/callback?code=stale&state="+u.Query().Get("state"))
		if !errors.Is(err, oauthlogin.ErrLoginLinkExpired) {
			t.Errorf("err = %v, want ErrLoginLinkExpired", err)
		}
		if LoadRefreshToken() != "" {
			t.Error("token written despite rejected code")
		}
	})
	t.Run("expired pending login", func(t *testing.T) {
		startGoogleTokenServer(t, &googleKnobs{})
		p := oauthlogin.NewPending(configSection)
		p.IssuedAt = time.Now().Add(-2 * oauthlogin.TTL)
		oauthlogin.SavePending(configSection, p)
		err := FinishLogin(context.Background(), Config{}, "http://localhost:4201/google/callback?code=x&state="+p.State)
		if !errors.Is(err, oauthlogin.ErrLoginLinkExpired) {
			t.Errorf("err = %v, want ErrLoginLinkExpired", err)
		}
	})
	t.Run("no refresh token in response", func(t *testing.T) {
		startGoogleTokenServer(t, &googleKnobs{omitRefresh: true})
		raw, _ := BeginLogin(Config{})
		u, _ := url.Parse(raw)
		err := FinishLogin(context.Background(), Config{}, "http://localhost:4201/google/callback?code=x&state="+u.Query().Get("state"))
		if err == nil || !strings.Contains(err.Error(), "did not return a refresh token") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestInteractiveLoginSavesToken(t *testing.T) {
	t.Setenv("CONFIG_FILES_DIR", t.TempDir())
	stubInitAuth(t, &oauth2.Token{RefreshToken: "g-interactive"}, nil)
	if err := InteractiveLogin(context.Background(), Config{}); err != nil {
		t.Fatal(err)
	}
	if got := LoadRefreshToken(); got != "g-interactive" {
		t.Errorf("saved token = %q", got)
	}
}

func TestIsTokenRejected(t *testing.T) {
	rerr := &oauth2.RetrieveError{ErrorCode: "invalid_grant"}
	wrapped := &url.Error{Op: "Get", URL: "https://photoslibrary.googleapis.com/x", Err: rerr}
	if !IsTokenRejected(wrapped) {
		t.Error("invalid_grant inside a url.Error must count as rejection")
	}
	if IsTokenRejected(&oauth2.RetrieveError{ErrorCode: "temporarily_unavailable"}) {
		t.Error("a non-invalid_grant token error must not count as rejection")
	}
	if IsTokenRejected(&oauth2.RetrieveError{ErrorCode: "invalid_grant", Response: &http.Response{StatusCode: http.StatusBadGateway}}) {
		t.Error("a 5xx claiming invalid_grant must not count as rejection")
	}
	if !IsTokenRejected(&oauth2.RetrieveError{ErrorCode: "invalid_grant", Response: &http.Response{StatusCode: http.StatusBadRequest}}) {
		t.Error("a 400 invalid_grant must count as rejection")
	}
	if IsTokenRejected(errors.New("dial tcp: boom")) {
		t.Error("a transport error must not count as rejection")
	}
	if !IsTokenRejected(fmt.Errorf("wrapped: %w", ErrTokenRejected)) {
		t.Error("the sentinel must count")
	}
}
