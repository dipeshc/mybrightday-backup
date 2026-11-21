// Package oauthlogin holds the parts of a browser-based OAuth login that are
// the same for every provider: the pending-login state kept between printing
// a login link and receiving its callback, the local listener that catches a
// redirect, callback parsing, and secret-file handling.
package oauthlogin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dipeshc/mybrightday-backup/internal/browser"
)

// TTL is how long a login link stays usable. Authorization codes from both
// Auth0 and Google expire in about ten minutes, so the pending state does too.
const TTL = 10 * time.Minute

const pendingFile = "pending_login"

// Failure modes that deserve a specific message to the user.
var (
	ErrNoPendingLogin        = errors.New("no login in progress")
	ErrCallbackNoCode        = errors.New("callback address contains no login code")
	ErrCallbackStateMismatch = errors.New("callback address does not match the login link that was issued")
	ErrLoginLinkExpired      = errors.New("login link has expired or was already used")
)

// Hooks so tests can avoid real browsers and terminals.
var (
	OpenBrowser = browser.Open
	IsTerminal  = isTerminal
)

// Pending is the half of an in-progress login that must survive between
// printing the login URL and receiving the callback. It is written under the
// config directory so the two halves can run as separate processes, which is
// how the GitHub workflow bootstraps.
type Pending struct {
	Provider string    `json:"provider"`
	Verifier string    `json:"verifier"`
	State    string    `json:"state"`
	IssuedAt time.Time `json:"issued_at"`
}

// NewPending creates fresh PKCE and state values for provider.
func NewPending(provider string) Pending {
	return Pending{
		Provider: provider,
		Verifier: RandomString(32),
		State:    RandomString(16),
		IssuedAt: time.Now(),
	}
}

// Challenge is the S256 PKCE challenge for the verifier.
func (p Pending) Challenge() string {
	hash := sha256.Sum256([]byte(p.Verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// Expired reports whether the login link issued for this state is past TTL.
func (p Pending) Expired() bool {
	return time.Since(p.IssuedAt) > TTL
}

// RandomString returns n random bytes, base64url encoded without padding.
func RandomString(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// ConfigDir returns the secrets directory used by the config resolution
// system, so files written here are picked up on the next run without any
// extra wiring.
func ConfigDir() string {
	if dir := os.Getenv("CONFIG_FILES_DIR"); dir != "" {
		return dir
	}
	return "config"
}

// SectionPath returns the path of file inside section under the config
// directory, e.g. SectionPath("mybrightday", "session").
func SectionPath(section, file string) string {
	return filepath.Join(ConfigDir(), section, file)
}

func pendingPath(section string) string {
	return SectionPath(section, pendingFile)
}

// SavePending records an in-progress login for section.
func SavePending(section string, p Pending) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return WriteSecretFile(pendingPath(section), data)
}

// LoadPending returns the in-progress login for section, or
// ErrNoPendingLogin. Unreadable state is removed and reported the same way.
func LoadPending(section string) (Pending, error) {
	data, err := os.ReadFile(pendingPath(section))
	if errors.Is(err, os.ErrNotExist) {
		return Pending{}, ErrNoPendingLogin
	}
	if err != nil {
		return Pending{}, fmt.Errorf("reading pending login: %w", err)
	}
	var p Pending
	if err := json.Unmarshal(data, &p); err != nil || p.Verifier == "" || p.State == "" {
		DeletePending(section)
		return Pending{}, ErrNoPendingLogin
	}
	return p, nil
}

// DeletePending forgets any in-progress login for section.
func DeletePending(section string) {
	_ = RemoveSecretFile(pendingPath(section))
}

// ParseCallback extracts the authorization code from the address the browser
// landed on, checking it belongs to the login that was issued.
func ParseCallback(raw, wantState string) (string, error) {
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

// CallbackMatches reports whether raw is a callback address for the given
// redirect URI (same host and path), used to route pasted addresses to the
// right provider.
func CallbackMatches(raw, redirectURI string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	r, err := url.Parse(redirectURI)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host) && u.Path == r.Path
}

// WaitForCallback serves the redirect endpoint on ln until a callback with
// the expected state arrives, the context ends, or the timeout passes. The
// page it serves reflects nothing from the request.
func WaitForCallback(ctx context.Context, ln net.Listener, path, wantState string, timeout time.Duration) (string, error) {
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		code, err := ParseCallback(r.URL.String(), wantState)
		if err != nil {
			// A provider reporting an error for this login (for example the
			// user declined) is worth surfacing rather than waiting out the
			// timeout; anything else is ignored as noise.
			if errors.Is(err, ErrCallbackNoCode) && r.URL.Query().Get("state") == wantState {
				if provErr := r.URL.Query().Get("error"); provErr != "" {
					http.Error(w, "Login was not completed ("+provErr+"). You can close this tab.", http.StatusBadRequest)
					select {
					case errCh <- fmt.Errorf("login was not completed: provider returned %q", provErr):
					default:
					}
					return
				}
			}
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
	case err := <-errCh:
		return "", err
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(timeout):
		return "", fmt.Errorf("no login received within %s", timeout)
	}
}

// WriteSecretFile writes data to path with owner-only permissions. The write
// is atomic (temp file + rename) so a crash cannot leave a truncated secret
// behind, and leftovers of an earlier interrupted write are removed first.
func WriteSecretFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	removeStaleTempFiles(path)
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { tmp.Close(); os.Remove(tmpName) }

	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("setting permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("closing %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}

// RemoveSecretFile deletes path and any temp copies left by an interrupted
// write. A missing file is not an error.
func RemoveSecretFile(path string) error {
	removeStaleTempFiles(path)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// ReadSecretFile returns the trimmed contents of path, or "" if it does not
// exist.
func ReadSecretFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func removeStaleTempFiles(path string) {
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".*"))
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

// isTerminal reports whether a human is at the keyboard and a browser can
// reasonably be opened here: stdin and stdout are character devices and
// this is not an SSH session (where a browser would open on the far side,
// unreachable, and the login link should be printed instead).
func isTerminal() bool {
	for _, v := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		if os.Getenv(v) != "" {
			return false
		}
	}
	for _, f := range []*os.File{os.Stdin, os.Stdout} {
		info, err := f.Stat()
		if err != nil || info.Mode()&os.ModeCharDevice == 0 {
			return false
		}
	}
	return true
}
