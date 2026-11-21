package mybrightday

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dipeshc/mybrightday-backup/internal/oauthlogin"
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

func TestLoginURLCarriesPKCEChallengeNotVerifier(t *testing.T) {
	p := oauthlogin.NewPending(configSection)
	u, err := url.Parse(LoginURL(p))
	if err != nil {
		t.Fatalf("parsing login URL: %v", err)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") != p.Challenge() {
		t.Errorf("PKCE challenge wrong: %v", q)
	}
	if q.Get("redirect_uri") != loginRedirectURI || q.Get("state") != p.State {
		t.Errorf("redirect/state wrong: %v", q)
	}
	if !strings.Contains(q.Get("scope"), "offline_access") {
		t.Errorf("scope %q lacks offline_access", q.Get("scope"))
	}
	if strings.Contains(LoginURL(p), p.Verifier) {
		t.Error("login URL leaks the PKCE verifier")
	}
	if !IsCallback("http://localhost:4200/okta/callback?code=x&state=y") || IsCallback("http://localhost:4201/google/callback?code=x") {
		t.Error("IsCallback routing wrong")
	}
}

func TestBeginThenFinishLogin(t *testing.T) {
	knobs := &authServerKnobs{}
	startAuthServer(t, knobs)

	loginURL, err := BeginLogin()
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(loginURL)
	state := u.Query().Get("state")

	if err := FinishLogin(context.Background(), "http://localhost:4200/okta/callback?code=the-code&state="+state); err != nil {
		t.Fatalf("FinishLogin: %v", err)
	}
	if knobs.gotCode != "the-code" || knobs.gotVerifier == "" {
		t.Errorf("exchange sent code=%q verifier present=%v", knobs.gotCode, knobs.gotVerifier != "")
	}
	if got := LoadSession(); got != "final-session" {
		t.Errorf("saved session = %q", got)
	}
	if _, err := oauthlogin.LoadPending(configSection); !errors.Is(err, oauthlogin.ErrNoPendingLogin) {
		t.Error("pending login still present after success")
	}
}

func TestFinishLoginFailures(t *testing.T) {
	startAuthServer(t, &authServerKnobs{})

	t.Run("no pending login", func(t *testing.T) {
		err := FinishLogin(context.Background(), "http://localhost:4200/okta/callback?code=x&state=y")
		if !errors.Is(err, oauthlogin.ErrNoPendingLogin) {
			t.Errorf("err = %v, want ErrNoPendingLogin", err)
		}
	})
	t.Run("state mismatch keeps pending login", func(t *testing.T) {
		if _, err := BeginLogin(); err != nil {
			t.Fatal(err)
		}
		err := FinishLogin(context.Background(), "http://localhost:4200/okta/callback?code=x&state=wrong")
		if !errors.Is(err, oauthlogin.ErrCallbackStateMismatch) {
			t.Errorf("err = %v, want ErrCallbackStateMismatch", err)
		}
		if _, err := oauthlogin.LoadPending(configSection); err != nil {
			t.Error("pending login removed on a mismatch; the user should be able to retry")
		}
	})
	t.Run("expired pending login", func(t *testing.T) {
		p := oauthlogin.NewPending(configSection)
		p.IssuedAt = time.Now().Add(-2 * oauthlogin.TTL)
		if err := oauthlogin.SavePending(configSection, p); err != nil {
			t.Fatal(err)
		}
		err := FinishLogin(context.Background(), "http://localhost:4200/okta/callback?code=x&state="+p.State)
		if !errors.Is(err, oauthlogin.ErrLoginLinkExpired) {
			t.Errorf("err = %v, want ErrLoginLinkExpired", err)
		}
		if _, err := oauthlogin.LoadPending(configSection); !errors.Is(err, oauthlogin.ErrNoPendingLogin) {
			t.Error("expired pending login not cleaned up")
		}
	})
}

func TestFinishLoginCodeRejected(t *testing.T) {
	startAuthServer(t, &authServerKnobs{codeStatus: http.StatusForbidden})
	loginURL, _ := BeginLogin()
	u, _ := url.Parse(loginURL)
	err := FinishLogin(context.Background(), "http://localhost:4200/okta/callback?code=stale&state="+u.Query().Get("state"))
	if !errors.Is(err, oauthlogin.ErrLoginLinkExpired) {
		t.Errorf("err = %v, want ErrLoginLinkExpired", err)
	}
	if LoadSession() != "" {
		t.Error("a session was written despite the rejected code")
	}
}

// stubListener makes InteractiveLogin bind an ephemeral port and reports it.
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

func TestInteractiveLoginViaListener(t *testing.T) {
	knobs := &authServerKnobs{}
	startAuthServer(t, knobs)
	addr := stubListener(t, false)

	browserBody := make(chan string, 1)
	origOpen := oauthlogin.OpenBrowser
	oauthlogin.OpenBrowser = func(loginURL string) error {
		u, err := url.Parse(loginURL)
		if err != nil {
			return err
		}
		go func() {
			resp, err := http.Get("http://" + addr.String() + "/okta/callback?code=browser-code&state=" + u.Query().Get("state"))
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
	t.Cleanup(func() { oauthlogin.OpenBrowser = origOpen })

	var out bytes.Buffer
	if err := InteractiveLogin(context.Background(), strings.NewReader(""), &out); err != nil {
		t.Fatalf("InteractiveLogin: %v", err)
	}
	if knobs.gotCode != "browser-code" {
		t.Errorf("exchanged code = %q, want browser-code", knobs.gotCode)
	}
	if got := LoadSession(); got != "final-session" {
		t.Errorf("saved session = %q", got)
	}
	if body := <-browserBody; !strings.Contains(body, "Login complete") {
		t.Errorf("browser saw %q, want the completion page", body)
	}
	if strings.Contains(out.String(), "final-session") {
		t.Error("terminal output leaks the session")
	}
}

func TestInteractiveLoginPortBusyFallsBackToPaste(t *testing.T) {
	knobs := &authServerKnobs{}
	startAuthServer(t, knobs)
	stubListener(t, true)

	origOpen := oauthlogin.OpenBrowser
	oauthlogin.OpenBrowser = func(string) error {
		t.Error("browser must not be opened automatically when the port is busy")
		return nil
	}
	t.Cleanup(func() { oauthlogin.OpenBrowser = origOpen })

	pr, pw := net.Pipe()
	var out syncBuffer
	done := make(chan error, 1)
	go func() { done <- InteractiveLogin(context.Background(), pr, &out) }()

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
		t.Fatalf("InteractiveLogin: %v", err)
	}
	if !strings.Contains(out.String(), "port 4200") {
		t.Errorf("output does not explain the port conflict: %q", out.String())
	}
	if knobs.gotCode != "pasted-code" {
		t.Errorf("exchanged code = %q, want pasted-code", knobs.gotCode)
	}
	if got := LoadSession(); got != "final-session" {
		t.Errorf("saved session = %q", got)
	}
	_ = os.Remove
}
