package oauthlogin

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPendingRoundTrip(t *testing.T) {
	t.Setenv("CONFIG_FILES_DIR", t.TempDir())
	p := NewPending("prov")
	if len(p.Verifier) != 43 || len(p.State) != 22 {
		t.Errorf("verifier/state lengths = %d/%d", len(p.Verifier), len(p.State))
	}
	if strings.Contains(p.Challenge(), p.Verifier) || p.Challenge() == "" {
		t.Error("challenge must be a hash, not the verifier")
	}
	if err := SavePending("prov", p); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(pendingPath("prov"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("pending file mode = %o, want 600", perm)
	}
	got, err := LoadPending("prov")
	if err != nil {
		t.Fatal(err)
	}
	if got.Verifier != p.Verifier || got.State != p.State || got.Provider != "prov" {
		t.Errorf("round trip = %+v, want %+v", got, p)
	}
	DeletePending("prov")
	if _, err := LoadPending("prov"); !errors.Is(err, ErrNoPendingLogin) {
		t.Errorf("after delete err = %v, want ErrNoPendingLogin", err)
	}

	// Garbage on disk reads as "no pending login" and is cleaned up.
	os.WriteFile(pendingPath("prov"), []byte("{not json"), 0o600)
	if _, err := LoadPending("prov"); !errors.Is(err, ErrNoPendingLogin) {
		t.Errorf("corrupt state err = %v, want ErrNoPendingLogin", err)
	}
	if _, err := os.Stat(pendingPath("prov")); !errors.Is(err, os.ErrNotExist) {
		t.Error("corrupt pending state left on disk")
	}
}

func TestPendingExpired(t *testing.T) {
	p := NewPending("x")
	if p.Expired() {
		t.Error("fresh pending login reported expired")
	}
	p.IssuedAt = time.Now().Add(-2 * TTL)
	if !p.Expired() {
		t.Error("old pending login not reported expired")
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
			code, err := ParseCallback(tt.raw, "s1")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err == nil && code != "abc" {
				t.Errorf("code = %q, want abc", code)
			}
		})
	}
}

func TestCallbackMatches(t *testing.T) {
	r := "http://localhost:4200/okta/callback"
	if !CallbackMatches("http://localhost:4200/okta/callback?code=x&state=y", r) {
		t.Error("same host and path should match")
	}
	if !CallbackMatches("HTTP://LOCALHOST:4200/okta/callback?code=x", r) {
		t.Error("host comparison should ignore case")
	}
	if CallbackMatches("http://localhost:4201/google/callback?code=x", r) {
		t.Error("different port/path must not match")
	}
	if CallbackMatches("nonsense", r) {
		t.Error("garbage must not match")
	}
}

func TestWaitForCallback(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	base := "http://" + ln.Addr().String()

	go func() {
		// A forged callback with the wrong state must not complete the login,
		// and must not reflect anything back.
		resp, err := http.Get(base + "/cb?code=forged&state=bad")
		if err == nil {
			resp.Body.Close()
		}
		resp, err = http.Get(base + "/cb?code=good&state=s1")
		if err == nil {
			resp.Body.Close()
		}
	}()
	code, err := WaitForCallback(context.Background(), ln, "/cb", "s1", 5*time.Second)
	if err != nil {
		t.Fatalf("WaitForCallback: %v", err)
	}
	if code != "good" {
		t.Errorf("code = %q, want good", code)
	}
}

func TestWaitForCallbackTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, err = WaitForCallback(context.Background(), ln, "/cb", "s1", 200*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "no login received") {
		t.Errorf("err = %v, want timeout", err)
	}
}

func TestSecretFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sec", "value")
	if err := WriteSecretFile(path, []byte("one")); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
	if got := ReadSecretFile(path); got != "one" {
		t.Errorf("read = %q", got)
	}
	// A stray temp copy from an interrupted write is removed by the next write.
	stray := filepath.Join(dir, "sec", ".value.123")
	os.WriteFile(stray, []byte("one"), 0o600)
	if err := WriteSecretFile(path, []byte("two\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stray); !errors.Is(err, os.ErrNotExist) {
		t.Error("stray temp file survived a write")
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "sec"))
	if len(entries) != 1 {
		t.Errorf("dir has %d entries, want 1", len(entries))
	}
	if got := ReadSecretFile(path); got != "two" {
		t.Errorf("read = %q, want trimmed value", got)
	}
	if err := RemoveSecretFile(path); err != nil {
		t.Fatal(err)
	}
	if err := RemoveSecretFile(path); err != nil {
		t.Errorf("removing a missing file = %v, want nil", err)
	}
	if got := ReadSecretFile(path); got != "" {
		t.Errorf("read after remove = %q, want empty", got)
	}
}

func TestWaitForCallbackSurfacesProviderError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	base := "http://" + ln.Addr().String()
	go func() {
		// A provider error for a different state is noise; for ours it ends the wait.
		for _, q := range []string{"/cb?error=access_denied&state=other", "/cb?error=access_denied&state=s1"} {
			resp, err := http.Get(base + q)
			if err == nil {
				resp.Body.Close()
			}
		}
	}()
	_, err = WaitForCallback(context.Background(), ln, "/cb", "s1", 5*time.Second)
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Errorf("err = %v, want the provider error surfaced", err)
	}
}

func TestIsTerminalFalseOverSSH(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "1.2.3.4 1 5.6.7.8 22")
	if isTerminal() {
		t.Error("an SSH session must not count as a place to open a browser")
	}
}
