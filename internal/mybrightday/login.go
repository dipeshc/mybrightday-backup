package mybrightday

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dipeshc/mybrightday-backup/internal/browser"
)

// Bright Horizons' Auth0 client only allows registered redirect URIs, and
// http://localhost:4200/okta/callback (their own dev-server address) is the
// only localhost one. The port therefore cannot be chosen freely.
const (
	loginRedirectURI = "http://localhost:4200/okta/callback"
	loginListenAddr  = "localhost:4200"
	loginCallbackTTL = 10 * time.Minute
	pkceStateFile    = "pkce_state"
)

// Hooks so tests can avoid real browsers and fixed ports.
var (
	openBrowserFn = browser.Open
	listenFn      = func() (net.Listener, error) { return net.Listen("tcp", loginListenAddr) }
	loginWaitFor  = loginCallbackTTL
)

// Login failure modes that deserve a specific message to the user.
var (
	ErrNoPendingLogin        = errors.New("no login in progress")
	ErrCallbackNoCode        = errors.New("callback address contains no login code")
	ErrCallbackStateMismatch = errors.New("callback address does not match the login link that was issued")
	ErrLoginLinkExpired      = errors.New("login link has expired or was already used")
)

// pkceState is the half of an in-progress login that must survive between
// printing the login URL and receiving the callback. It is written to the
// config directory so the two halves can run as separate processes (the
// GitHub workflow's two-run bootstrap).
type pkceState struct {
	Verifier string    `json:"verifier"`
	State    string    `json:"state"`
	IssuedAt time.Time `json:"issued_at"`
}

func newPKCEState() pkceState {
	return pkceState{
		Verifier: generateRandomString(32),
		State:    generateRandomString(16),
		IssuedAt: time.Now(),
	}
}

// authorizeURL builds the Auth0 login link carrying this state's PKCE
// challenge. Everything in it is safe to show: the verifier never leaves the
// machine.
func (p pkceState) authorizeURL() string {
	hash := sha256.Sum256([]byte(p.Verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])
	q := url.Values{
		"client_id":             {ficClientID},
		"scope":                 {"openid offline_access profile email"},
		"audience":              {ficAudience},
		"redirect_uri":          {loginRedirectURI},
		"response_type":         {"code"},
		"response_mode":         {"query"},
		"state":                 {p.State},
		"nonce":                 {generateRandomString(16)},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	return ficAuth0BaseURL + "/authorize?" + q.Encode()
}

func (p pkceState) expired() bool {
	return time.Since(p.IssuedAt) > loginCallbackTTL
}

func pkceStatePath() string {
	return filepath.Join(mybrightdayDir(), pkceStateFile)
}

func savePKCEState(p pkceState) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return writeSecretFile(pkceStatePath(), data)
}

func loadPKCEState() (pkceState, error) {
	data, err := os.ReadFile(pkceStatePath())
	if errors.Is(err, os.ErrNotExist) {
		return pkceState{}, ErrNoPendingLogin
	}
	if err != nil {
		return pkceState{}, fmt.Errorf("reading pending login: %w", err)
	}
	var p pkceState
	if err := json.Unmarshal(data, &p); err != nil || p.Verifier == "" || p.State == "" {
		deletePKCEState()
		return pkceState{}, ErrNoPendingLogin
	}
	return p, nil
}

func deletePKCEState() {
	removeStaleTempFiles(pkceStatePath())
	_ = os.Remove(pkceStatePath())
}

// parseCallback extracts the authorization code from the address the browser
// landed on, checking it belongs to the login we issued.
func parseCallback(raw, wantState string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", ErrCallbackNoCode
	}
	code := u.Query().Get("code")
	if code == "" {
		return "", ErrCallbackNoCode
	}
	if u.Query().Get("state") != wantState {
		return "", ErrCallbackStateMismatch
	}
	return code, nil
}

// exchangeCode redeems an authorization code for a refresh token.
func exchangeCode(ctx context.Context, code, verifier string) (string, error) {
	client, _ := newAuthClient()
	tok, err := tokenGrant(ctx, client, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {ficClientID},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {loginRedirectURI},
	})
	if errors.Is(err, ErrRefreshTokenRejected) {
		// Auth0 answers a stale or reused code with invalid_grant, the same
		// shape as a bad refresh token; for a login the user-facing meaning is
		// "this link is dead".
		return "", ErrLoginLinkExpired
	}
	if err != nil {
		return "", err
	}
	if tok.RefreshToken == "" {
		return "", errors.New("Auth0 did not return a refresh token")
	}
	return tok.RefreshToken, nil
}

// LoginPrintURL starts a login without a browser: it records the PKCE state
// and writes the login link to w. LoginFinish completes it, possibly from a
// different process.
func LoginPrintURL(w io.Writer) error {
	p := newPKCEState()
	if err := savePKCEState(p); err != nil {
		return fmt.Errorf("saving pending login: %w", err)
	}
	_, err := fmt.Fprintln(w, p.authorizeURL())
	return err
}

// LoginFinish completes a login started by LoginPrintURL using the address
// the browser landed on, and saves the resulting refresh token.
func LoginFinish(ctx context.Context, callbackURL string) error {
	p, err := loadPKCEState()
	if err != nil {
		return err
	}
	if p.expired() {
		deletePKCEState()
		return ErrLoginLinkExpired
	}
	code, err := parseCallback(callbackURL, p.State)
	if err != nil {
		return err
	}
	token, err := exchangeCode(ctx, code, p.Verifier)
	if err != nil {
		if errors.Is(err, ErrLoginLinkExpired) {
			deletePKCEState()
		}
		return err
	}
	if err := SaveRefreshToken(token); err != nil {
		return fmt.Errorf("saving refresh token: %w", err)
	}
	deletePKCEState()
	return nil
}

// LoginInteractive runs the whole login at a terminal. It prefers to catch
// the browser redirect on port 4200; if that port is taken it falls back to
// asking the user to paste the address they land on.
func LoginInteractive(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	p := newPKCEState()
	loginURL := p.authorizeURL()

	ln, err := listenFn()
	if err != nil {
		return loginByPaste(ctx, p, loginURL, stdin, stdout)
	}
	defer ln.Close()

	fmt.Fprintln(stdout, "Opening your browser to log in to MyBrightDay...")
	fmt.Fprintf(stdout, "If it doesn't open, visit: %s\n", loginURL)
	if err := openBrowserFn(loginURL); err != nil {
		fmt.Fprintln(stdout, "Could not open a browser automatically; use the link above.")
	}
	fmt.Fprintln(stdout, "Waiting for login...")

	code, err := waitForCallback(ctx, ln, p.State)
	if err != nil {
		return err
	}
	token, err := exchangeCode(ctx, code, p.Verifier)
	if err != nil {
		return err
	}
	if err := SaveRefreshToken(token); err != nil {
		return fmt.Errorf("saving refresh token: %w", err)
	}
	fmt.Fprintln(stdout, "Saved. MyBrightDay is connected. Run `mbdb` to back up your photos.")
	return nil
}

func loginByPaste(ctx context.Context, p pkceState, loginURL string, stdin io.Reader, stdout io.Writer) error {
	fmt.Fprintf(stdout, `MyBrightDay login needs port 4200 on this machine, but something is already using it.
Bright Horizons only allows that port, so it can't be changed.

Either free the port and run this again, or finish the login by hand:

  1. Open this link and log in:
     %s
  2. You'll land on a "This site can't be reached" page. That's expected.
     Copy the whole address from the address bar.
  3. Paste it here.

Callback address: `, loginURL)

	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && line == "" {
		return errors.New("no callback address entered")
	}
	code, err := parseCallback(line, p.State)
	if err != nil {
		return err
	}
	token, err := exchangeCode(ctx, code, p.Verifier)
	if err != nil {
		return err
	}
	if err := SaveRefreshToken(token); err != nil {
		return fmt.Errorf("saving refresh token: %w", err)
	}
	fmt.Fprintln(stdout, "\nSaved. MyBrightDay is connected. Run `mbdb` to back up your photos.")
	return nil
}

// waitForCallback serves the redirect endpoint on ln until a callback with the
// expected state arrives, the context ends, or the login window closes.
func waitForCallback(ctx context.Context, ln net.Listener, wantState string) (string, error) {
	codeCh := make(chan string, 1)
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/okta/callback" {
			http.NotFound(w, r)
			return
		}
		code, err := parseCallback(r.URL.String(), wantState)
		if err != nil {
			http.Error(w, "Login failed: "+err.Error()+". Return to the terminal and try again.", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "Login complete. You can close this tab.")
		select {
		case codeCh <- code:
		default:
		}
	})}
	go srv.Serve(ln)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	select {
	case code := <-codeCh:
		return code, nil
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(loginWaitFor):
		return "", fmt.Errorf("no login received within %s", loginWaitFor)
	}
}
