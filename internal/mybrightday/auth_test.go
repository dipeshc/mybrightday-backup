package mybrightday

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dipeshc/mybrightday-backup/internal/oauthlogin"
)

// authServerKnobs lets each test break one stage of the otherwise-happy login.
type authServerKnobs struct {
	codeStatus        int    // non-zero overrides /oauth/token status
	codeErrorCode     string // Auth0 "error" field for non-200 responses (default invalid_grant; "-" = non-JSON body)
	accessToken       string // "-" means omit
	oktaStatus        int    // non-zero overrides oktaservice status
	oktaUnauthorized  bool   // oktaservice answers isAuthorized=false
	mbdTokenStatus    int    // non-zero overrides mbdtoken status
	omitSessionCookie bool

	// Populated by the fake.
	gotCode, gotVerifier, gotRedirect, gotIDToken string
}

// startAuthServer stands up a fake for the code exchange and the two
// MyBrightDay stages, and points the package URL vars at it. Tests must not
// run in parallel.
func startAuthServer(t *testing.T, knobs *authServerKnobs) {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if got := r.PostForm.Get("grant_type"); got != "authorization_code" {
			t.Errorf("grant_type = %q, want authorization_code", got)
		}
		if got := r.PostForm.Get("client_id"); got != ficClientID {
			t.Errorf("client_id = %q, want %q", got, ficClientID)
		}
		knobs.gotCode = r.PostForm.Get("code")
		knobs.gotVerifier = r.PostForm.Get("code_verifier")
		knobs.gotRedirect = r.PostForm.Get("redirect_uri")

		if knobs.codeStatus != 0 {
			code := knobs.codeErrorCode
			if code == "" {
				code = "invalid_grant"
			}
			w.WriteHeader(knobs.codeStatus)
			if code == "-" {
				io.WriteString(w, "upstream unavailable")
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": "nope"})
			return
		}
		resp := map[string]string{"access_token": "fic-jwt", "id_token": "id-jwt", "refresh_token": "unused-refresh-token"}
		if knobs.accessToken == "-" {
			delete(resp, "access_token")
		}
		json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("POST /api/account/oktaservice", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer fic-jwt" {
			t.Errorf("oktaservice authorization = %q, want Bearer fic-jwt", got)
		}
		var body struct {
			IDToken string `json:"idToken"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		knobs.gotIDToken = body.IDToken
		if knobs.oktaStatus != 0 {
			w.WriteHeader(knobs.oktaStatus)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"isAuthorized": !knobs.oktaUnauthorized, "errorCode": nil, "actionId": 1})
	})

	mux.HandleFunc("GET /api/account/mbdtoken", func(w http.ResponseWriter, r *http.Request) {
		if knobs.mbdTokenStatus != 0 {
			http.Error(w, "mbd denied", knobs.mbdTokenStatus)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fic-jwt" {
			t.Errorf("authorization = %q, want Bearer fic-jwt", got)
		}
		json.NewEncoder(w).Encode(map[string]string{"token": "mbd-jwt"})
	})

	mux.HandleFunc("GET /auth/jwt/redirect", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("jwt"); got != "mbd-jwt" {
			t.Errorf("jwt = %q, want mbd-jwt", got)
		}
		if !knobs.omitSessionCookie {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "final-session", Path: "/"})
		}
		w.WriteHeader(http.StatusOK)
	})

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	origAuth0, origGateway, origRedirect := ficAuth0BaseURL, mbdGatewayBaseURL, mbdSessionRedirectURL
	ficAuth0BaseURL = ts.URL
	mbdGatewayBaseURL = ts.URL
	mbdSessionRedirectURL = ts.URL + "/auth/jwt/redirect"
	t.Cleanup(func() {
		ficAuth0BaseURL, mbdGatewayBaseURL, mbdSessionRedirectURL = origAuth0, origGateway, origRedirect
	})
	t.Setenv("CONFIG_FILES_DIR", t.TempDir())
}

func TestCompleteLoginHappyPath(t *testing.T) {
	knobs := &authServerKnobs{}
	startAuthServer(t, knobs)

	session, err := CompleteLogin(context.Background(), "the-code", "the-verifier")
	if err != nil {
		t.Fatalf("CompleteLogin: %v", err)
	}
	if session != "final-session" {
		t.Errorf("session = %q, want final-session", session)
	}
	if knobs.gotCode != "the-code" || knobs.gotVerifier != "the-verifier" || knobs.gotRedirect != loginRedirectURI {
		t.Errorf("exchange sent code=%q verifier=%q redirect=%q", knobs.gotCode, knobs.gotVerifier, knobs.gotRedirect)
	}
	if knobs.gotIDToken != "id-jwt" {
		t.Errorf("gateway received idToken %q, want id-jwt: the login must be registered before mbdtoken", knobs.gotIDToken)
	}
}

func TestCompleteLoginFailures(t *testing.T) {
	tests := []struct {
		name        string
		knobs       authServerKnobs
		errContains string
		wantExpired bool
	}{
		{name: "stale code", knobs: authServerKnobs{codeStatus: http.StatusForbidden}, wantExpired: true},
		{name: "rate limited is not expiry", knobs: authServerKnobs{codeStatus: http.StatusTooManyRequests, codeErrorCode: "too_many_requests"}, errContains: "status 429 (too_many_requests)"},
		{name: "server error claiming invalid_grant is not expiry", knobs: authServerKnobs{codeStatus: http.StatusBadGateway, codeErrorCode: "invalid_grant"}, errContains: "status 502 (invalid_grant)"},
		{name: "token endpoint unavailable", knobs: authServerKnobs{codeStatus: http.StatusBadGateway, codeErrorCode: "-"}, errContains: "status 502"},
		{name: "empty access token", knobs: authServerKnobs{accessToken: "-"}, errContains: "no access token"},
		{name: "login registration fails", knobs: authServerKnobs{oktaStatus: http.StatusUnauthorized}, errContains: "registering login with MBD gateway failed"},
		{name: "login not authorized", knobs: authServerKnobs{oktaUnauthorized: true}, errContains: "did not authorize the login"},
		{name: "mbd token rejected", knobs: authServerKnobs{mbdTokenStatus: http.StatusForbidden}, errContains: "MBD token exchange failed"},
		{name: "missing session cookie", knobs: authServerKnobs{omitSessionCookie: true}, errContains: "session cookie not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			knobs := tt.knobs
			startAuthServer(t, &knobs)

			_, err := CompleteLogin(context.Background(), "the-code", "the-verifier")
			if err == nil {
				t.Fatal("CompleteLogin succeeded, want failure")
			}
			if tt.wantExpired != errors.Is(err, oauthlogin.ErrLoginLinkExpired) {
				t.Errorf("err = %v, ErrLoginLinkExpired = %v, want %v", err, !tt.wantExpired, tt.wantExpired)
			}
			if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("err = %v, want containing %q", err, tt.errContains)
			}
			if strings.Contains(err.Error(), "the-code") || strings.Contains(err.Error(), "the-verifier") {
				t.Errorf("err = %v, must not include the code or verifier", err)
			}
		})
	}
}

func TestSessionErrorsNeverIncludeTheJWT(t *testing.T) {
	// Point the session redirect at a closed port so client.Do fails with a
	// url.Error, whose text would otherwise include the ?jwt= query.
	startAuthServer(t, &authServerKnobs{})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedAddr := ln.Addr().String()
	ln.Close()
	mbdSessionRedirectURL = "http://" + closedAddr + "/auth/jwt/redirect"

	_, err = CompleteLogin(context.Background(), "the-code", "the-verifier")
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if strings.Contains(err.Error(), "mbd-jwt") || strings.Contains(err.Error(), "jwt=") {
		t.Errorf("error leaks the MBD JWT: %v", err)
	}
	if !strings.Contains(err.Error(), "initializing MBD session") {
		t.Errorf("err = %v, want the session stage named", err)
	}
}

func TestSessionFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CONFIG_FILES_DIR", dir)
	if err := SaveSession("sess-1"); err != nil {
		t.Fatal(err)
	}
	if SessionPath() != filepath.Join(dir, "mybrightday", "session") {
		t.Errorf("SessionPath = %q", SessionPath())
	}
	info, _ := os.Stat(SessionPath())
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("session file mode = %o, want 600", perm)
	}
	if got := LoadSession(); got != "sess-1" {
		t.Errorf("LoadSession = %q", got)
	}
	if err := DeleteSession(); err != nil {
		t.Fatal(err)
	}
	if got := LoadSession(); got != "" {
		t.Errorf("LoadSession after delete = %q", got)
	}
}

func fakeSessionCookie(expires float64) string {
	payload, _ := json.Marshal(map[string]any{"is_logged_in": true, "user_id": "u", "expires": expires})
	return base64.RawURLEncoding.EncodeToString(payload) + ".Zz.sig"
}

func TestSessionExpiry(t *testing.T) {
	want := time.Now().Add(300 * 24 * time.Hour).Truncate(time.Second)
	got, ok := SessionExpiry(fakeSessionCookie(float64(want.Unix())))
	if !ok || !got.Equal(want) {
		t.Errorf("SessionExpiry = %v, %v; want %v, true", got, ok, want)
	}
	if _, ok := SessionExpiry("not-a-cookie"); ok {
		t.Error("garbage reported an expiry")
	}
	if _, ok := SessionExpiry(base64.RawURLEncoding.EncodeToString([]byte(`{"no":"expires"}`)) + ".x.y"); ok {
		t.Error("payload without expires reported an expiry")
	}
}
