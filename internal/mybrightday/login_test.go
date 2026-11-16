package mybrightday

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a bytes.Buffer safe to read from another goroutine.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// startLoginServer fakes Auth0's token endpoint for the authorization_code
// grant. It records the code and verifier it received.
type loginServerKnobs struct {
	codeStatus   int
	gotCode      string
	gotVerifier  string
	gotRedirect  string
	refreshToken string
}

func startLoginServer(t *testing.T, knobs *loginServerKnobs) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if got := r.PostForm.Get("grant_type"); got != "authorization_code" {
			t.Errorf("grant_type = %q, want authorization_code", got)
		}
		knobs.gotCode = r.PostForm.Get("code")
		knobs.gotVerifier = r.PostForm.Get("code_verifier")
		knobs.gotRedirect = r.PostForm.Get("redirect_uri")
		if knobs.codeStatus != 0 {
			w.WriteHeader(knobs.codeStatus)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant", "error_description": "Invalid authorization code"})
			return
		}
		rt := knobs.refreshToken
		if rt == "" {
			rt = "new-refresh-token"
		}
		json.NewEncoder(w).Encode(map[string]string{"access_token": "fic-jwt", "refresh_token": rt})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	orig := ficAuth0BaseURL
	ficAuth0BaseURL = ts.URL
	t.Cleanup(func() { ficAuth0BaseURL = orig })
	t.Setenv("CONFIG_FILES_DIR", t.TempDir())
}

func readToken(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(RefreshTokenPath())
	if err != nil {
		t.Fatalf("reading saved token: %v", err)
	}
	return string(data)
}

func TestAuthorizeURLCarriesPKCEChallengeNotVerifier(t *testing.T) {
	p := newPKCEState()
	u, err := url.Parse(p.authorizeURL())
	if err != nil {
		t.Fatalf("parsing authorize URL: %v", err)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		t.Errorf("missing PKCE challenge: %v", q)
	}
	if q.Get("redirect_uri") != loginRedirectURI {
		t.Errorf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	if q.Get("state") != p.State {
		t.Errorf("state = %q, want %q", q.Get("state"), p.State)
	}
	if !strings.Contains(q.Get("scope"), "offline_access") {
		t.Errorf("scope %q lacks offline_access", q.Get("scope"))
	}
	if strings.Contains(p.authorizeURL(), p.Verifier) {
		t.Error("authorize URL leaks the PKCE verifier")
	}
}

func TestParseCallback(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr error
	}{
		{"ok", "http://localhost:4200/okta/callback?code=abc&state=s1", nil},
		{"ok with whitespace", "  http://localhost:4200/okta/callback?code=abc&state=s1\n", nil},
		{"no code", "http://localhost:4200/okta/callback?state=s1", ErrCallbackNoCode},
		{"not a url", "just some text", ErrCallbackNoCode},
		{"wrong state", "http://localhost:4200/okta/callback?code=abc&state=other", ErrCallbackStateMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, err := parseCallback(tt.raw, "s1")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err == nil && code != "abc" {
				t.Errorf("code = %q, want abc", code)
			}
		})
	}
}

func TestLoginPrintURLThenFinish(t *testing.T) {
	knobs := &loginServerKnobs{}
	startLoginServer(t, knobs)

	var out bytes.Buffer
	if err := LoginPrintURL(&out); err != nil {
		t.Fatalf("LoginPrintURL: %v", err)
	}
	printed := strings.TrimSpace(out.String())
	u, err := url.Parse(printed)
	if err != nil || u.Path != "/authorize" {
		t.Fatalf("printed %q, want an authorize URL", printed)
	}

	// The pending-login file must exist with owner-only permissions.
	info, err := os.Stat(pkceStatePath())
	if err != nil {
		t.Fatalf("pkce state not written: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("pkce state mode = %o, want 600", perm)
	}

	state := u.Query().Get("state")
	callback := "http://localhost:4200/okta/callback?code=the-code&state=" + state
	if err := LoginFinish(context.Background(), callback); err != nil {
		t.Fatalf("LoginFinish: %v", err)
	}

	if knobs.gotCode != "the-code" {
		t.Errorf("exchanged code = %q, want the-code", knobs.gotCode)
	}
	if knobs.gotVerifier == "" {
		t.Error("exchange sent no code_verifier")
	}
	if knobs.gotRedirect != loginRedirectURI {
		t.Errorf("exchange redirect_uri = %q", knobs.gotRedirect)
	}
	if got := readToken(t); got != "new-refresh-token" {
		t.Errorf("saved token = %q", got)
	}
	if _, err := os.Stat(pkceStatePath()); !errors.Is(err, os.ErrNotExist) {
		t.Error("pkce state still present after a successful login")
	}
}

func TestLoginFinishFailures(t *testing.T) {
	startLoginServer(t, &loginServerKnobs{})

	t.Run("no pending login", func(t *testing.T) {
		err := LoginFinish(context.Background(), "http://localhost:4200/okta/callback?code=x&state=y")
		if !errors.Is(err, ErrNoPendingLogin) {
			t.Errorf("err = %v, want ErrNoPendingLogin", err)
		}
	})

	t.Run("state mismatch keeps pending login", func(t *testing.T) {
		if err := LoginPrintURL(&bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		err := LoginFinish(context.Background(), "http://localhost:4200/okta/callback?code=x&state=wrong")
		if !errors.Is(err, ErrCallbackStateMismatch) {
			t.Errorf("err = %v, want ErrCallbackStateMismatch", err)
		}
		if _, err := os.Stat(pkceStatePath()); err != nil {
			t.Error("pkce state removed on a mismatch; the user should be able to retry with the right address")
		}
	})

	t.Run("expired pending login", func(t *testing.T) {
		p := newPKCEState()
		p.IssuedAt = time.Now().Add(-2 * loginCallbackTTL)
		if err := savePKCEState(p); err != nil {
			t.Fatal(err)
		}
		err := LoginFinish(context.Background(), "http://localhost:4200/okta/callback?code=x&state="+p.State)
		if !errors.Is(err, ErrLoginLinkExpired) {
			t.Errorf("err = %v, want ErrLoginLinkExpired", err)
		}
		if _, err := os.Stat(pkceStatePath()); !errors.Is(err, os.ErrNotExist) {
			t.Error("expired pkce state not cleaned up")
		}
	})
}

func TestLoginFinishCodeRejected(t *testing.T) {
	startLoginServer(t, &loginServerKnobs{codeStatus: http.StatusForbidden})

	var out bytes.Buffer
	if err := LoginPrintURL(&out); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(strings.TrimSpace(out.String()))
	err := LoginFinish(context.Background(), "http://localhost:4200/okta/callback?code=stale&state="+u.Query().Get("state"))
	if !errors.Is(err, ErrLoginLinkExpired) {
		t.Errorf("err = %v, want ErrLoginLinkExpired", err)
	}
	if _, err := os.Stat(RefreshTokenPath()); !errors.Is(err, os.ErrNotExist) {
		t.Error("a token file was written despite the rejected code")
	}
}

// stubListener makes LoginInteractive bind an ephemeral port and reports it.
func stubListener(t *testing.T, fail bool) *net.TCPAddr {
	t.Helper()
	var addr *net.TCPAddr
	origListen, origWait := listenFn, loginWaitFor
	if fail {
		listenFn = func() (net.Listener, error) { return nil, errors.New("address already in use") }
	} else {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr = ln.Addr().(*net.TCPAddr)
		listenFn = func() (net.Listener, error) { return ln, nil }
	}
	loginWaitFor = 5 * time.Second
	t.Cleanup(func() { listenFn, loginWaitFor = origListen, origWait })
	return addr
}

func TestLoginInteractiveViaListener(t *testing.T) {
	knobs := &loginServerKnobs{}
	startLoginServer(t, knobs)
	addr := stubListener(t, false)

	// Stand in for the browser: follow the login link's state straight to
	// the local callback, as Auth0 would after a successful login.
	browserBody := make(chan string, 1)
	origOpen := openBrowserFn
	openBrowserFn = func(loginURL string) error {
		u, err := url.Parse(loginURL)
		if err != nil {
			return err
		}
		go func() {
			cb := "http://" + addr.String() + "/okta/callback?code=browser-code&state=" + u.Query().Get("state")
			resp, err := http.Get(cb)
			if err != nil {
				browserBody <- "request failed: " + err.Error()
				return
			}
			defer resp.Body.Close()
			var buf bytes.Buffer
			buf.ReadFrom(resp.Body)
			browserBody <- buf.String()
		}()
		return nil
	}
	t.Cleanup(func() { openBrowserFn = origOpen })

	var out bytes.Buffer
	if err := LoginInteractive(context.Background(), strings.NewReader(""), &out); err != nil {
		t.Fatalf("LoginInteractive: %v", err)
	}
	if knobs.gotCode != "browser-code" {
		t.Errorf("exchanged code = %q, want browser-code", knobs.gotCode)
	}
	if got := readToken(t); got != "new-refresh-token" {
		t.Errorf("saved token = %q", got)
	}
	if !strings.Contains(out.String(), "MyBrightDay is connected") {
		t.Errorf("output lacks success message: %q", out.String())
	}
	if body := <-browserBody; !strings.Contains(body, "Login complete") {
		t.Errorf("browser saw %q, want the completion page", body)
	}
	if strings.Contains(out.String(), "new-refresh-token") {
		t.Error("terminal output leaks the refresh token")
	}
}

func TestLoginInteractiveListenerRejectsWrongState(t *testing.T) {
	startLoginServer(t, &loginServerKnobs{})
	addr := stubListener(t, false)
	loginWaitFor = 500 * time.Millisecond

	origOpen := openBrowserFn
	openBrowserFn = func(string) error {
		go func() {
			resp, err := http.Get("http://" + addr.String() + "/okta/callback?code=x&state=forged")
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}
	t.Cleanup(func() { openBrowserFn = origOpen })

	err := LoginInteractive(context.Background(), strings.NewReader(""), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "no login received") {
		t.Errorf("err = %v, want timeout: a forged callback must not complete the login", err)
	}
}

func TestLoginInteractivePortBusyFallsBackToPaste(t *testing.T) {
	knobs := &loginServerKnobs{}
	startLoginServer(t, knobs)
	stubListener(t, true)

	origOpen := openBrowserFn
	openBrowserFn = func(string) error {
		t.Error("browser must not be opened automatically when the port is busy")
		return nil
	}
	t.Cleanup(func() { openBrowserFn = origOpen })

	// The state is only known once the prompt is printed, so feed stdin
	// through a pipe and answer after reading the prompt.
	pr, pw := net.Pipe()
	var out syncBuffer
	done := make(chan error, 1)
	go func() { done <- LoginInteractive(context.Background(), pr, &out) }()

	// Wait for the prompt, then extract the state from the printed link.
	var state string
	deadline := time.Now().Add(5 * time.Second)
	for state == "" && time.Now().Before(deadline) {
		if i := strings.Index(out.String(), "state="); i >= 0 {
			rest := out.String()[i+len("state="):]
			state = strings.FieldsFunc(rest, func(r rune) bool { return r == '&' || r == '\n' || r == ' ' })[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	if state == "" {
		t.Fatalf("prompt never printed a login link: %q", out.String())
	}
	pw.Write([]byte("http://localhost:4200/okta/callback?code=pasted-code&state=" + state + "\n"))
	pw.Close()

	if err := <-done; err != nil {
		t.Fatalf("LoginInteractive: %v", err)
	}
	if !strings.Contains(out.String(), "port 4200") {
		t.Errorf("output does not explain the port conflict: %q", out.String())
	}
	if knobs.gotCode != "pasted-code" {
		t.Errorf("exchanged code = %q, want pasted-code", knobs.gotCode)
	}
	if got := readToken(t); got != "new-refresh-token" {
		t.Errorf("saved token = %q", got)
	}
}

func TestPKCEStateRoundTrip(t *testing.T) {
	t.Setenv("CONFIG_FILES_DIR", t.TempDir())
	p := newPKCEState()
	if err := savePKCEState(p); err != nil {
		t.Fatal(err)
	}
	got, err := loadPKCEState()
	if err != nil {
		t.Fatal(err)
	}
	if got.Verifier != p.Verifier || got.State != p.State {
		t.Errorf("round trip = %+v, want %+v", got, p)
	}
	if filepath.Dir(pkceStatePath()) != filepath.Dir(RefreshTokenPath()) {
		t.Error("pkce state and refresh token live in different directories")
	}

	// Garbage on disk reads as "no pending login" rather than a crash, and is
	// cleaned up.
	os.WriteFile(pkceStatePath(), []byte("{not json"), 0o600)
	if _, err := loadPKCEState(); !errors.Is(err, ErrNoPendingLogin) {
		t.Errorf("corrupt state err = %v, want ErrNoPendingLogin", err)
	}
	if _, err := os.Stat(pkceStatePath()); !errors.Is(err, os.ErrNotExist) {
		t.Error("corrupt pkce state left on disk")
	}
}
