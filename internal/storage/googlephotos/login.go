package googlephotos

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"golang.org/x/oauth2"

	"github.com/dipeshc/mybrightday-backup/internal/oauthlogin"
)

// Google's desktop-app OAuth clients accept any localhost redirect, so a
// fixed port is used for the two-step (link, then pasted address) login that
// machines without a browser rely on. The interactive flow in auth.go keeps
// its random port.
const (
	configSection    = "google_photos"
	refreshTokenFile = "refresh_token"
	loginRedirectURI = "http://localhost:4201/google/callback"
)

// LoginURL builds the Google consent link for the pending login.
func LoginURL(cfg Config, p oauthlogin.Pending) (string, error) {
	oauthCfg, err := buildOAuthConfig(cfg)
	if err != nil {
		return "", err
	}
	oauthCfg.RedirectURL = loginRedirectURI
	return oauthCfg.AuthCodeURL(p.State,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
		oauth2.S256ChallengeOption(p.Verifier),
	), nil
}

// IsCallback reports whether raw is a Google login callback address.
func IsCallback(raw string) bool {
	return oauthlogin.CallbackMatches(raw, loginRedirectURI)
}

// BeginLogin records a pending login and returns the link to open.
func BeginLogin(cfg Config) (string, error) {
	p := oauthlogin.NewPending(configSection)
	u, err := LoginURL(cfg, p)
	if err != nil {
		return "", err
	}
	if err := oauthlogin.SavePending(configSection, p); err != nil {
		return "", fmt.Errorf("saving pending login: %w", err)
	}
	return u, nil
}

// FinishLogin completes a login started by BeginLogin using the address the
// browser landed on, and saves the refresh token.
func FinishLogin(ctx context.Context, cfg Config, callbackURL string) error {
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
	oauthCfg, err := buildOAuthConfig(cfg)
	if err != nil {
		return err
	}
	oauthCfg.RedirectURL = loginRedirectURI
	tok, err := oauthCfg.Exchange(ctx, code, oauth2.VerifierOption(p.Verifier))
	if err != nil {
		var rerr *oauth2.RetrieveError
		if errors.As(err, &rerr) && rerr.ErrorCode == "invalid_grant" {
			oauthlogin.DeletePending(configSection)
			return oauthlogin.ErrLoginLinkExpired
		}
		return fmt.Errorf("exchanging Google login code: %w", withoutURL(err))
	}
	if tok.RefreshToken == "" {
		return errors.New("Google did not return a refresh token")
	}
	if err := SaveRefreshToken(tok.RefreshToken); err != nil {
		return err
	}
	oauthlogin.DeletePending(configSection)
	return nil
}

// InteractiveLogin runs the browser flow at a terminal and saves the token.
func InteractiveLogin(ctx context.Context, cfg Config) error {
	tok, err := performInitAuthFn(ctx, cfg)
	if err != nil {
		return fmt.Errorf("google photos authentication: %w", err)
	}
	return SaveRefreshToken(tok.RefreshToken)
}

// ErrTokenRejected is returned (wrapped) when Google refuses the stored
// refresh token: revoked by the user, expired, or the project's consent
// screen moved back to testing. Callers should discard the token and ask for
// a login again.
var ErrTokenRejected = errors.New("google refresh token rejected")

// IsTokenRejected reports whether err stems from Google refusing the refresh
// token (an invalid_grant from the token endpoint), however deeply the oauth2
// transport wrapped it.
func IsTokenRejected(err error) bool {
	if errors.Is(err, ErrTokenRejected) {
		return true
	}
	var rerr *oauth2.RetrieveError
	if !errors.As(err, &rerr) || rerr.ErrorCode != "invalid_grant" {
		return false
	}
	// Only a 4xx from the token endpoint is a verdict on the token itself.
	return rerr.Response == nil || (rerr.Response.StatusCode >= 400 && rerr.Response.StatusCode < 500)
}

// DeleteRefreshToken removes the stored token. A missing file is not an error.
func DeleteRefreshToken() error {
	return oauthlogin.RemoveSecretFile(RefreshTokenPath())
}

// RefreshTokenPath is where the token is saved and where config resolution
// reads `google_photos.refresh_token` from.
func RefreshTokenPath() string {
	return oauthlogin.SectionPath(configSection, refreshTokenFile)
}

// SaveRefreshToken writes the token with owner-only permissions.
func SaveRefreshToken(token string) error {
	if err := oauthlogin.WriteSecretFile(RefreshTokenPath(), []byte(token)); err != nil {
		return fmt.Errorf("saving google refresh token: %w", err)
	}
	return nil
}

// LoadRefreshToken returns the stored token, or "".
func LoadRefreshToken() string {
	return oauthlogin.ReadSecretFile(RefreshTokenPath())
}

// withoutURL strips the request URL from a transport error so a token
// endpoint's query can never reach a log.
func withoutURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return ue.Err
	}
	return err
}
