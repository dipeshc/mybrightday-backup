package mybrightday

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/dipeshc/mybrightday-backup/internal/oauthlogin"
)

const (
	ficClientID = "5VIzhuWNKxFc9etVvp5fonr2tlbBEZae"
	ficAudience = "https://ShareservicesAPI"
	userAgent   = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.7727.138 Safari/537.36"

	configSection = "mybrightday"
	sessionFile   = "session"
)

// Endpoint bases are variables so tests can point the flow at a fake server.
var (
	ficAuth0BaseURL       = "https://bhloginsso.brighthorizons.com"
	mbdGatewayBaseURL     = "https://mbdwgateway.brighthorizons.com"
	mbdSessionRedirectURL = "https://mybrightday.brighthorizons.com/auth/jwt/redirect"
)

// ErrSessionRejected is returned when the MyBrightDay API refuses the stored
// session (expired, or revoked by a logout). Callers should discard the
// session and ask the user to log in again.
var ErrSessionRejected = errors.New("session rejected")

// tokenResponse is the subset of Auth0's /oauth/token body that the flow uses.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	Error       string `json:"error"`
	ErrorDesc   string `json:"error_description"`
}

func newAuthClient() (*http.Client, *cookiejar.Jar) {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, Timeout: 30 * time.Second}, jar
}

// CompleteLogin turns an authorization code from the browser login into a
// MyBrightDay session cookie: exchange the code at Auth0, register the login
// with the MyBrightDay gateway (what the Family Info Center web app does
// after every browser login; without it the gateway refuses to mint a
// session), fetch an MBD token, and redeem it for the session.
//
// The refresh token Auth0 also issues is deliberately discarded: on this
// tenant it dies after a few idle minutes, while the session cookie is valid
// for about a year.
func CompleteLogin(ctx context.Context, code, verifier string) (string, error) {
	client, jar := newAuthClient()

	tok, err := tokenGrant(ctx, client, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {ficClientID},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {loginRedirectURI},
	})
	if err != nil {
		return "", err
	}
	if err := registerLogin(ctx, client, tok.AccessToken, tok.IDToken); err != nil {
		return "", err
	}
	return sessionFromFICJWT(ctx, client, jar, tok.AccessToken)
}

// tokenGrant posts form to Auth0's token endpoint and decodes the response.
// A 4xx carrying Auth0's invalid_grant error means the code is stale or was
// already used and maps to oauthlogin.ErrLoginLinkExpired; anything else
// non-200 is reported as a plain error. Only Auth0's error code is included
// in error text, never the request or any token.
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

	if resp.StatusCode >= 400 && resp.StatusCode < 500 && tok.Error == "invalid_grant" {
		return nil, oauthlogin.ErrLoginLinkExpired
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

// registerLogin posts the ID token to the gateway's oktaservice endpoint.
// The gateway refuses to mint an MBD token for a login it has not seen
// through this call (observed as a bare 401 from mbdtoken).
func registerLogin(ctx context.Context, client *http.Client, accessToken, idToken string) error {
	body, err := json.Marshal(map[string]string{"idToken": idToken})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mbdGatewayBaseURL+"/api/account/oktaservice", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
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

// SessionPath is where SaveSession writes and where config resolution reads
// `mybrightday.session` from.
func SessionPath() string {
	return oauthlogin.SectionPath(configSection, sessionFile)
}

// SaveSession writes the session cookie with owner-only permissions.
func SaveSession(session string) error {
	return oauthlogin.WriteSecretFile(SessionPath(), []byte(session))
}

// LoadSession returns the stored session cookie, or "".
func LoadSession() string {
	return oauthlogin.ReadSecretFile(SessionPath())
}

// DeleteSession removes the stored session. A missing file is not an error.
func DeleteSession() error {
	return oauthlogin.RemoveSecretFile(SessionPath())
}

// SessionExpiry reads the expiry embedded in the session cookie's payload.
// The cookie is a signed, dot-separated token whose first part is base64
// JSON with an "expires" Unix timestamp. The signature is not verified
// (the server does that); this is only used to warn ahead of expiry.
func SessionExpiry(session string) (time.Time, bool) {
	first := strings.SplitN(session, ".", 2)[0]
	data, err := base64.RawURLEncoding.DecodeString(first)
	if err != nil {
		data, err = base64.URLEncoding.DecodeString(first)
	}
	if err != nil {
		return time.Time{}, false
	}
	var payload struct {
		Expires float64 `json:"expires"`
	}
	if json.Unmarshal(data, &payload) != nil || payload.Expires == 0 {
		return time.Time{}, false
	}
	return time.Unix(int64(payload.Expires), 0), true
}
