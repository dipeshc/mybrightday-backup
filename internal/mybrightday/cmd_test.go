package mybrightday

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestFriendlyLoginError(t *testing.T) {
	tests := []struct {
		in   error
		want string
	}{
		{nil, ""},
		{ErrNoPendingLogin, "no login is in progress"},
		{ErrCallbackNoCode, "doesn't contain a login code"},
		{ErrCallbackStateMismatch, "doesn't match the login link"},
		{ErrLoginLinkExpired, "expired or was already used"},
		{errors.New("dial tcp: boom"), "MyBrightDay login failed: dial tcp: boom"},
	}
	for _, tt := range tests {
		got := friendlyLoginError(tt.in)
		if tt.want == "" {
			if got != nil {
				t.Errorf("friendlyLoginError(nil) = %v, want nil", got)
			}
			continue
		}
		if got == nil || !strings.Contains(got.Error(), tt.want) {
			t.Errorf("friendlyLoginError(%v) = %v, want containing %q", tt.in, got, tt.want)
		}
	}
}

func TestLoginCommandRejectsBothFlags(t *testing.T) {
	cmd := LoginCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--print-url", "--callback-url", "http://localhost:4200/okta/callback?code=x"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Errorf("err = %v, want flag-conflict error", err)
	}
}

func TestLoginCommandPrintURL(t *testing.T) {
	t.Setenv("CONFIG_FILES_DIR", t.TempDir())
	cmd := LoginCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--print-url"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(out.String()), ficAuth0BaseURL+"/authorize?") {
		t.Errorf("printed %q, want an authorize URL", out.String())
	}
}

func TestLoginCommandCallbackWithoutPendingLogin(t *testing.T) {
	t.Setenv("CONFIG_FILES_DIR", t.TempDir())
	cmd := LoginCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--callback-url", "http://localhost:4200/okta/callback?code=x&state=y"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "no login is in progress") {
		t.Errorf("err = %v, want friendly no-pending-login message", err)
	}
}
