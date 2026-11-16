package mybrightday

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	ficClientID = "5VIzhuWNKxFc9etVvp5fonr2tlbBEZae"
	ficAudience = "https://ShareservicesAPI"
	userAgent   = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.7727.138 Safari/537.36"

	refreshTokenFile = "refresh_token"
)

// Endpoint bases are variables so tests can point the flow at a fake server.
var (
	ficAuth0BaseURL       = "https://bhloginsso.brighthorizons.com"
	mbdGatewayBaseURL     = "https://mbdwgateway.brighthorizons.com"
	mbdSessionRedirectURL = "https://mybrightday.brighthorizons.com/auth/jwt/redirect"
)

// ErrRefreshTokenRejected is returned when Auth0 refuses the refresh token
// outright (expired, revoked, or already rotated past its reuse window), as
// opposed to a transient failure. Callers should discard the token and ask the
// user to log in again.
var ErrRefreshTokenRejected = errors.New("refresh token rejected")

// tokenResponse is the subset of Auth0's /oauth/token body that the flow uses.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func generateRandomString(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func newAuthClient() (*http.Client, *cookiejar.Jar) {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, Timeout: 30 * time.Second}, jar
}

// Access is the result of a successful refresh grant.
type Access struct {
	// Token is the short-lived Family Info Center access token.
	Token string
	// IDToken is the OIDC ID token issued alongside it; the MyBrightDay
	// gateway wants it when registering the login.
	IDToken string
	// RefreshToken is the rotated refresh token to persist. It equals the
	// token that was presented only when Auth0 chose not to rotate.
	RefreshToken string
}

// RefreshAccess presents a refresh token to Auth0 and returns a fresh access
// token plus the rotated refresh token.
//
// Auth0 rotates refresh tokens: every grant returns a new one and the old one
// stops working shortly afterwards. Callers must persist Access.RefreshToken
// before doing anything else that could fail, which is why this is a separate
// step from SessionFromAccess.
//
// The HTTP client is intentionally not wrapped with httpx.RetryTransport: the
// grant is single-use once it succeeds, and an automatic retry of a request
// that actually went through would burn the freshly rotated token.
func RefreshAccess(ctx context.Context, refreshToken string) (Access, error) {
	client, _ := newAuthClient()

	tok, err := tokenGrant(ctx, client, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {ficClientID},
		"refresh_token": {refreshToken},
	})
	if err != nil {
		return Access{}, fmt.Errorf("refreshing MyBrightDay access: %w", err)
	}

	rotated := tok.RefreshToken
	if rotated == "" {
		rotated = refreshToken
	}
	return Access{Token: tok.AccessToken, IDToken: tok.IDToken, RefreshToken: rotated}, nil
}

// SessionFromAccess turns a Family Info Center login into a MyBrightDay
// session cookie, the same way the Family Info Center web app does after a
// browser login: register the login with the MyBrightDay gateway, fetch an
// MBD token, redeem it for a session.
func SessionFromAccess(ctx context.Context, access Access) (string, error) {
	client, jar := newAuthClient()
	if err := registerLogin(ctx, client, access); err != nil {
		return "", err
	}
	return sessionFromFICJWT(ctx, client, jar, access.Token)
}

// registerLogin posts the ID token to the gateway's oktaservice endpoint.
// The gateway refuses to mint an MBD token for a login it has not seen
// through this call (observed as a bare 401 from mbdtoken), and the record
// it keeps expires, so this runs on every authentication rather than only
// once.
func registerLogin(ctx context.Context, client *http.Client, access Access) error {
	body, err := json.Marshal(map[string]string{"idToken": access.IDToken})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mbdGatewayBaseURL+"/api/account/oktaservice", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+access.Token)
	req.Header.Set("Content-Type", "application/json")
	setGatewayHeaders(req)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("registering login with MBD gateway: %w", withoutURL(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("registering login with MBD gateway failed: status %d", resp.StatusCode)
	}

	var result struct {
		IsAuthorized bool `json:"isAuthorized"`
		ErrorCode    any  `json:"errorCode"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decoding MBD gateway login response: %w", err)
	}
	if !result.IsAuthorized {
		return fmt.Errorf("MBD gateway did not authorize the login (error code %v)", result.ErrorCode)
	}
	return nil
}

// withoutURL strips the request URL from a transport error. url.Error's
// message embeds the full URL, and some of ours carry credentials in the
// query string.
func withoutURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return ue.Err
	}
	return err
}

func setGatewayHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Origin", "https://familyinfocenter.brighthorizons.com")
	req.Header.Set("Referer", "https://familyinfocenter.brighthorizons.com/")
	req.Header.Set("User-Agent", userAgent)
}

// tokenGrant posts form to Auth0's token endpoint and decodes the response.
// A 4xx carrying Auth0's invalid_grant error maps to ErrRefreshTokenRejected;
// anything else non-200 is reported as a plain error. Only Auth0's
// error_description is included in error text, never the request or any token.
func tokenGrant(ctx context.Context, client *http.Client, form url.Values) (*tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ficAuth0BaseURL+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", withoutURL(err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading token response: %w", err)
	}

	var tok tokenResponse
	// Ignore decode errors here: a non-JSON body is reported via the status
	// check below, and a 200 with garbage is caught by the empty-token check.
	_ = json.Unmarshal(body, &tok)

	// Only invalid_grant means the token itself is dead. Other 4xx responses
	// (429 rate limiting, 403 access_denied from tenant-level blocks, 400
	// invalid_request) leave the token valid at Auth0, so they must not be
	// classified as a rejection, or the caller would delete a good token.
	if resp.StatusCode >= 400 && resp.StatusCode < 500 && tok.Error == "invalid_grant" {
		desc := tok.ErrorDesc
		if desc == "" {
			desc = http.StatusText(resp.StatusCode)
		}
		return nil, fmt.Errorf("%w: %s", ErrRefreshTokenRejected, desc)
	}
	if resp.StatusCode != http.StatusOK {
		if tok.Error != "" {
			return nil, fmt.Errorf("token endpoint returned status %d (%s)", resp.StatusCode, tok.Error)
		}
		return nil, fmt.Errorf("token endpoint returned status %d", resp.StatusCode)
	}
	if tok.AccessToken == "" {
		return nil, errors.New("token endpoint returned no access token")
	}
	return &tok, nil
}

// sessionFromFICJWT runs the two MyBrightDay-side stages: exchange the Family
// Info Center access token for an MBD token, then redeem that for a session
// cookie.
func sessionFromFICJWT(ctx context.Context, client *http.Client, jar *cookiejar.Jar, ficJWT string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mbdGatewayBaseURL+"/api/account/mbdtoken", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+ficJWT)
	setGatewayHeaders(req)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("exchanging MBD token: %w", withoutURL(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("MBD token exchange failed: status %d", resp.StatusCode)
	}

	var mbd struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&mbd); err != nil {
		return "", fmt.Errorf("decoding MBD token response: %w", err)
	}
	if mbd.Token == "" {
		return "", errors.New("MBD token exchange returned no token")
	}

	req, err = http.NewRequestWithContext(ctx, http.MethodGet, mbdSessionRedirectURL, nil)
	if err != nil {
		return "", err
	}
	q := req.URL.Query()
	q.Add("jwt", mbd.Token)
	req.URL.RawQuery = q.Encode()
	req.Header.Set("User-Agent", userAgent)

	resp, err = client.Do(req)
	if err != nil {
		// The request URL carries the MBD JWT in its query string, and a
		// url.Error's text includes the URL. Never let that reach a log.
		return "", fmt.Errorf("initializing MBD session: %w", withoutURL(err))
	}
	defer resp.Body.Close()

	for _, c := range jar.Cookies(resp.Request.URL) {
		if c.Name == "session" {
			return c.Value, nil
		}
	}
	return "", fmt.Errorf("session cookie not found after initialization (checked %s)", resp.Request.URL.Host)
}

// configDir returns the secrets directory used by the config resolution
// system, so files written here are picked up on the next run without any
// extra wiring.
func configDir() string {
	if dir := os.Getenv("CONFIG_FILES_DIR"); dir != "" {
		return dir
	}
	return "config"
}

func mybrightdayDir() string {
	return filepath.Join(configDir(), "mybrightday")
}

// RefreshTokenPath is where SaveRefreshToken writes and where config
// resolution reads `mybrightday.refresh_token` from.
func RefreshTokenPath() string {
	return filepath.Join(mybrightdayDir(), refreshTokenFile)
}

// SaveRefreshToken writes token to RefreshTokenPath with owner-only
// permissions. The write is atomic (temp file + rename) so a crash cannot
// leave a truncated token behind.
func SaveRefreshToken(token string) error {
	return writeSecretFile(RefreshTokenPath(), []byte(token))
}

// DeleteRefreshToken removes the stored token, including any temp copy left
// by an interrupted write. A missing file is not an error.
func DeleteRefreshToken() error {
	removeStaleTempFiles(RefreshTokenPath())
	if err := os.Remove(RefreshTokenPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// removeStaleTempFiles deletes leftovers of writeSecretFile for path: a crash
// between CreateTemp and Rename leaves a 0600 copy of the secret behind.
func removeStaleTempFiles(path string) {
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".*"))
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

func writeSecretFile(path string, data []byte) error {
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
