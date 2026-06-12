package mybrightday

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// authServerKnobs lets each test break one stage of the otherwise-happy flow.
type authServerKnobs struct {
	authorizeRedirect    string // overrides the stage-1 redirect target path
	omitState            bool
	omitPasswordRedirect bool
	omitCode             bool
	tokenStatus          int    // non-zero overrides /oauth/token status
	accessToken          string // stage-4 access token ("-" means omit)
	mbdTokenStatus       int    // non-zero overrides mbdtoken status
	omitSessionCookie    bool
}

// startAuthServer stands up a fake for all six auth stages and points the
// package URL vars at it. Tests must not run in parallel.
func startAuthServer(t *testing.T, knobs authServerKnobs) {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		target := "/u/login/identifier?state=test-state"
		if knobs.omitState {
			target = "/u/login/identifier"
		}
		if knobs.authorizeRedirect != "" {
			target = knobs.authorizeRedirect
		}
		http.Redirect(w, r, target, http.StatusFound)
	})

	mux.HandleFunc("/u/login/identifier", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("POST /u/login/password", func(w http.ResponseWriter, r *http.Request) {
		if knobs.omitPasswordRedirect {
			w.WriteHeader(http.StatusOK)
			return
		}
		location := ficRedirectURI + "?code=test-code"
		if knobs.omitCode {
			location = ficRedirectURI
		}
		// Set the header directly: http.Redirect would also be fine, but the
		// client must NOT follow this hop (CheckRedirect stops at ficRedirectURI).
		w.Header().Set("Location", location)
		w.WriteHeader(http.StatusFound)
	})

	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		if knobs.tokenStatus != 0 {
			http.Error(w, "token denied", knobs.tokenStatus)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parsing token form: %v", err)
		}
		if got := r.PostForm.Get("code"); got != "test-code" {
			t.Errorf("token exchange code = %q, want test-code", got)
		}
		if r.PostForm.Get("code_verifier") == "" {
			t.Error("token exchange missing PKCE code_verifier")
		}
		token := knobs.accessToken
		if token == "" {
			token = "fic-jwt"
		}
		if token == "-" {
			token = ""
		}
		json.NewEncoder(w).Encode(map[string]string{"access_token": token})
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

	origAuth0, origToken, origRedirect := ficAuth0BaseURL, mbdTokenURL, mbdSessionRedirectURL
	ficAuth0BaseURL = ts.URL
	mbdTokenURL = ts.URL + "/api/account/mbdtoken"
	mbdSessionRedirectURL = ts.URL + "/auth/jwt/redirect"
	t.Cleanup(func() {
		ficAuth0BaseURL, mbdTokenURL, mbdSessionRedirectURL = origAuth0, origToken, origRedirect
	})
}

func TestAuthenticateHappyPath(t *testing.T) {
	startAuthServer(t, authServerKnobs{})

	cookie, err := Authenticate(context.Background(), "user@example.com", "hunter2")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if cookie != "final-session" {
		t.Errorf("cookie = %q, want final-session", cookie)
	}
}

func TestAuthenticateFailures(t *testing.T) {
	tests := []struct {
		name        string
		knobs       authServerKnobs
		errContains string
	}{
		{
			name:        "unexpected authorize redirect",
			knobs:       authServerKnobs{authorizeRedirect: "/somewhere/else"},
			errContains: "unexpected authorize redirect",
		},
		{
			name:        "missing auth0 state",
			knobs:       authServerKnobs{omitState: true},
			errContains: "could not find Auth0 state",
		},
		{
			name:        "missing callback redirect",
			knobs:       authServerKnobs{omitPasswordRedirect: true},
			errContains: "missing callback redirect",
		},
		{
			name:        "missing authorization code",
			knobs:       authServerKnobs{omitCode: true},
			errContains: "no authorization code",
		},
		{
			name:        "token exchange rejected",
			knobs:       authServerKnobs{tokenStatus: http.StatusForbidden},
			errContains: "token exchange failed",
		},
		{
			name:        "empty access token",
			knobs:       authServerKnobs{accessToken: "-"},
			errContains: "no valid token",
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
			startAuthServer(t, tt.knobs)

			_, err := Authenticate(context.Background(), "user@example.com", "hunter2")
			if err == nil || !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("err = %v, want containing %q", err, tt.errContains)
			}
		})
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
