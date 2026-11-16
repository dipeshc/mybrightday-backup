package mybrightday

import (
	"context"
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
)

// authServerKnobs lets each test break one stage of the otherwise-happy flow.
type authServerKnobs struct {
	tokenStatus       int    // non-zero overrides /oauth/token status
	tokenErrorCode    string // Auth0 "error" field for non-200 responses (default invalid_grant)
	tokenError        string // Auth0 error_description for non-200 responses
	accessToken       string // "-" means omit
	rotatedToken      string // "-" means omit
	oktaStatus        int    // non-zero overrides oktaservice status
	oktaUnauthorized  bool   // oktaservice answers isAuthorized=false
	mbdTokenStatus    int    // non-zero overrides mbdtoken status
	omitSessionCookie bool

	// Populated by the fake: the idToken the client registered.
	gotIDToken string

	// Populated by the fake: the refresh_token value the client sent.
	gotRefreshToken string
}

// startAuthServer stands up a fake for the refresh grant and the two
// MyBrightDay stages, and points the package URL vars at it. Tests must not
// run in parallel.
func startAuthServer(t *testing.T, knobs *authServerKnobs) {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parsing token form: %v", err)
		}
		if got := r.PostForm.Get("grant_type"); got != "refresh_token" {
			t.Errorf("grant_type = %q, want refresh_token", got)
		}
		if got := r.PostForm.Get("client_id"); got != ficClientID {
			t.Errorf("client_id = %q, want %q", got, ficClientID)
		}
		knobs.gotRefreshToken = r.PostForm.Get("refresh_token")

		if knobs.tokenStatus != 0 {
			code := knobs.tokenErrorCode
			if code == "" {
				code = "invalid_grant"
			}
			w.WriteHeader(knobs.tokenStatus)
			if code == "-" {
				io.WriteString(w, "upstream unavailable")
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": knobs.tokenError})
			return
		}
		resp := map[string]string{"access_token": "fic-jwt", "id_token": "id-jwt", "refresh_token": "rotated-token"}
		if knobs.accessToken == "-" {
			delete(resp, "access_token")
		}
		if knobs.rotatedToken == "-" {
			delete(resp, "refresh_token")
		} else if knobs.rotatedToken != "" {
			resp["refresh_token"] = knobs.rotatedToken
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
}

func TestAuthenticateHappyPath(t *testing.T) {
	knobs := &authServerKnobs{}
	startAuthServer(t, knobs)

	access, err := RefreshAccess(context.Background(), "old-token")
	if err != nil {
		t.Fatalf("RefreshAccess: %v", err)
	}
	if access.RefreshToken != "rotated-token" {
		t.Errorf("rotated = %q, want rotated-token", access.RefreshToken)
	}
	if access.Token != "fic-jwt" || access.IDToken != "id-jwt" {
		t.Errorf("access = %+v, want fic-jwt / id-jwt", access)
	}
	cookie, err := SessionFromAccess(context.Background(), access)
	if err != nil {
		t.Fatalf("SessionFromAccess: %v", err)
	}
	if cookie != "final-session" {
		t.Errorf("cookie = %q, want final-session", cookie)
	}
	if knobs.gotIDToken != "id-jwt" {
		t.Errorf("gateway received idToken %q, want id-jwt: the login must be registered before mbdtoken", knobs.gotIDToken)
	}
	if knobs.gotRefreshToken != "old-token" {
		t.Errorf("server received refresh_token %q, want old-token", knobs.gotRefreshToken)
	}
}

func TestAuthenticateKeepsTokenWhenNotRotated(t *testing.T) {
	startAuthServer(t, &authServerKnobs{rotatedToken: "-"})

	access, err := RefreshAccess(context.Background(), "old-token")
	if err != nil {
		t.Fatalf("RefreshAccess: %v", err)
	}
	if access.RefreshToken != "old-token" {
		t.Errorf("rotated = %q, want the original token when Auth0 omits a new one", access.RefreshToken)
	}
}

func TestAuthenticateRejectedToken(t *testing.T) {
	startAuthServer(t, &authServerKnobs{tokenStatus: http.StatusForbidden, tokenError: "Unknown or invalid refresh token."})

	_, err := RefreshAccess(context.Background(), "dead-token")
	if !errors.Is(err, ErrRefreshTokenRejected) {
		t.Fatalf("err = %v, want ErrRefreshTokenRejected", err)
	}
	if !strings.Contains(err.Error(), "Unknown or invalid refresh token") {
		t.Errorf("err = %v, want Auth0's description included", err)
	}
	if strings.Contains(err.Error(), "dead-token") {
		t.Errorf("err = %v, must not include the token", err)
	}
}

func TestRefreshAccessFailures(t *testing.T) {
	tests := []struct {
		name        string
		knobs       authServerKnobs
		errContains string
	}{
		{
			name:        "token endpoint unavailable",
			knobs:       authServerKnobs{tokenStatus: http.StatusBadGateway, tokenErrorCode: "-"},
			errContains: "status 502",
		},
		{
			name:        "server error claiming invalid_grant is not a rejection",
			knobs:       authServerKnobs{tokenStatus: http.StatusBadGateway, tokenErrorCode: "invalid_grant"},
			errContains: "status 502 (invalid_grant)",
		},
		{
			name:        "rate limited is not a rejection",
			knobs:       authServerKnobs{tokenStatus: http.StatusTooManyRequests, tokenErrorCode: "too_many_requests"},
			errContains: "status 429 (too_many_requests)",
		},
		{
			name:        "access denied is not a rejection",
			knobs:       authServerKnobs{tokenStatus: http.StatusForbidden, tokenErrorCode: "access_denied"},
			errContains: "status 403 (access_denied)",
		},
		{
			name:        "empty access token",
			knobs:       authServerKnobs{accessToken: "-"},
			errContains: "no access token",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			knobs := tt.knobs
			startAuthServer(t, &knobs)

			_, err := RefreshAccess(context.Background(), "old-token")
			if err == nil || !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("err = %v, want containing %q", err, tt.errContains)
			}
			if errors.Is(err, ErrRefreshTokenRejected) {
				t.Errorf("err = %v, must not be classified as a rejected token", err)
			}
		})
	}
}

func TestSessionFromAccessFailures(t *testing.T) {
	tests := []struct {
		name        string
		knobs       authServerKnobs
		errContains string
	}{
		{
			name:        "login registration fails",
			knobs:       authServerKnobs{oktaStatus: http.StatusUnauthorized},
			errContains: "registering login with MBD gateway failed",
		},
		{
			name:        "login not authorized",
			knobs:       authServerKnobs{oktaUnauthorized: true},
			errContains: "did not authorize the login",
		},
		{
			name:        "mbd token rejected",
			knobs:       authServerKnobs{mbdTokenStatus: http.StatusForbidden},
			errContains: "MBD token exchange failed",
		},
		{
			name:        "missing session cookie",
			knobs:       authServerKnobs{omitSessionCookie: true},
			errContains: "session cookie not found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			knobs := tt.knobs
			startAuthServer(t, &knobs)

			_, err := SessionFromAccess(context.Background(), Access{Token: "fic-jwt", IDToken: "id-jwt"})
			if err == nil || !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("err = %v, want containing %q", err, tt.errContains)
			}
		})
	}
}

func TestSaveAndDeleteRefreshToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CONFIG_FILES_DIR", dir)

	if err := SaveRefreshToken("tok-1"); err != nil {
		t.Fatalf("SaveRefreshToken: %v", err)
	}
	path := filepath.Join(dir, "mybrightday", "refresh_token")
	if RefreshTokenPath() != path {
		t.Errorf("RefreshTokenPath = %q, want %q", RefreshTokenPath(), path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading token: %v", err)
	}
	if string(data) != "tok-1" {
		t.Errorf("token file = %q, want tok-1", data)
	}
	info, _ := os.Stat(path)
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token file mode = %o, want 600", perm)
	}

	// Overwrite replaces atomically and leaves no temp files behind.
	if err := SaveRefreshToken("tok-2"); err != nil {
		t.Fatalf("SaveRefreshToken (overwrite): %v", err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("config dir has %d entries after overwrite, want 1", len(entries))
	}

	if err := DeleteRefreshToken(); err != nil {
		t.Fatalf("DeleteRefreshToken: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("token file still exists after delete")
	}
	if err := DeleteRefreshToken(); err != nil {
		t.Errorf("DeleteRefreshToken on missing file = %v, want nil", err)
	}
}

func TestGenerateRandomString(t *testing.T) {
	a := generateRandomString(32)
	b := generateRandomString(32)
	if a == b {
		t.Error("two random strings are identical")
	}
	// 32 bytes base64url-encoded without padding is 43 characters.
	if len(a) != 43 {
		t.Errorf("len = %d, want 43", len(a))
	}
	if strings.ContainsAny(a, "+/=") {
		t.Errorf("string %q contains non-URL-safe characters", a)
	}
}

func TestSessionErrorsNeverIncludeTheJWT(t *testing.T) {
	// Point the session redirect at a closed port so client.Do fails with a
	// url.Error, whose text would otherwise include the ?jwt= query.
	knobs := &authServerKnobs{}
	startAuthServer(t, knobs)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedAddr := ln.Addr().String()
	ln.Close()
	mbdSessionRedirectURL = "http://" + closedAddr + "/auth/jwt/redirect"

	_, err = SessionFromAccess(context.Background(), Access{Token: "fic-jwt", IDToken: "id-jwt"})
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

func TestDeleteRefreshTokenRemovesStaleTempFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CONFIG_FILES_DIR", dir)
	if err := SaveRefreshToken("tok"); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(dir, "mybrightday", ".refresh_token.123456")
	if err := os.WriteFile(stray, []byte("tok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := DeleteRefreshToken(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stray); !errors.Is(err, os.ErrNotExist) {
		t.Error("stale temp copy of the token survived DeleteRefreshToken")
	}
}
