package mybrightday

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"

	"github.com/dipeshc/mybrightday-backup/internal/oauthlogin"
)

// Bright Horizons' Auth0 client only allows registered redirect URIs, and
// http://localhost:4200/okta/callback (their own dev-server address) is the
// only localhost one. The port therefore cannot be chosen freely.
const (
	loginRedirectURI = "http://localhost:4200/okta/callback"
	loginListenAddr  = "localhost:4200"
	loginCallback    = "/okta/callback"
)

// Hooks so tests can avoid fixed ports.
var (
	listenFn     = func() (net.Listener, error) { return net.Listen("tcp", loginListenAddr) }
	loginWaitFor = oauthlogin.TTL
)

// LoginURL builds the Auth0 login link carrying the pending login's PKCE
// challenge. Everything in it is safe to show: the verifier never leaves
// the machine.
func LoginURL(p oauthlogin.Pending) string {
	q := url.Values{
		"client_id":             {ficClientID},
		"scope":                 {"openid offline_access profile email"},
		"audience":              {ficAudience},
		"redirect_uri":          {loginRedirectURI},
		"response_type":         {"code"},
		"response_mode":         {"query"},
		"state":                 {p.State},
		"nonce":                 {oauthlogin.RandomString(16)},
		"code_challenge":        {p.Challenge()},
		"code_challenge_method": {"S256"},
	}
	return ficAuth0BaseURL + "/authorize?" + q.Encode()
}

// IsCallback reports whether raw is a MyBrightDay login callback address.
func IsCallback(raw string) bool {
	return oauthlogin.CallbackMatches(raw, loginRedirectURI)
}

// BeginLogin records a pending login and returns the link to open. The login
// is completed later, possibly by another process, with FinishLogin.
func BeginLogin() (string, error) {
	p := oauthlogin.NewPending(configSection)
	if err := oauthlogin.SavePending(configSection, p); err != nil {
		return "", fmt.Errorf("saving pending login: %w", err)
	}
	return LoginURL(p), nil
}

// FinishLogin completes a login started by BeginLogin using the address the
// browser landed on, and saves the resulting session.
func FinishLogin(ctx context.Context, callbackURL string) error {
	p, err := oauthlogin.LoadPending(configSection)
	if err != nil {
		return err
	}
	if p.Expired() {
		oauthlogin.DeletePending(configSection)
		return oauthlogin.ErrLoginLinkExpired
	}
	code, err := oauthlogin.ParseCallback(callbackURL, p.State)
	if err != nil {
		return err
	}
	session, err := CompleteLogin(ctx, code, p.Verifier)
	if err != nil {
		if errors.Is(err, oauthlogin.ErrLoginLinkExpired) {
			oauthlogin.DeletePending(configSection)
		}
		return err
	}
	if err := SaveSession(session); err != nil {
		return fmt.Errorf("saving session: %w", err)
	}
	oauthlogin.DeletePending(configSection)
	return nil
}

// InteractiveLogin runs the whole login at a terminal. It prefers to catch
// the browser redirect on port 4200; if that port is taken it falls back to
// asking the user to paste the address they land on.
func InteractiveLogin(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	p := oauthlogin.NewPending(configSection)
	loginURL := LoginURL(p)

	ln, err := listenFn()
	if err != nil {
		return loginByPaste(ctx, p, loginURL, stdin, stdout)
	}
	defer ln.Close()

	fmt.Fprintln(stdout, "Opening your browser to log in to MyBrightDay...")
	fmt.Fprintf(stdout, "If it doesn't open, visit: %s\n", loginURL)
	if err := oauthlogin.OpenBrowser(loginURL); err != nil {
		fmt.Fprintln(stdout, "Could not open a browser automatically; use the link above.")
	}
	fmt.Fprintln(stdout, "Waiting for login...")

	code, err := oauthlogin.WaitForCallback(ctx, ln, loginCallback, p.State, loginWaitFor)
	if err != nil {
		return err
	}
	session, err := CompleteLogin(ctx, code, p.Verifier)
	if err != nil {
		return err
	}
	if err := SaveSession(session); err != nil {
		return fmt.Errorf("saving session: %w", err)
	}
	fmt.Fprintln(stdout, "Logged in to MyBrightDay.")
	return nil
}

func loginByPaste(ctx context.Context, p oauthlogin.Pending, loginURL string, stdin io.Reader, stdout io.Writer) error {
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
	code, err := oauthlogin.ParseCallback(line, p.State)
	if err != nil {
		return err
	}
	session, err := CompleteLogin(ctx, code, p.Verifier)
	if err != nil {
		return err
	}
	if err := SaveSession(session); err != nil {
		return fmt.Errorf("saving session: %w", err)
	}
	fmt.Fprintln(stdout, "\nLogged in to MyBrightDay.")
	return nil
}
