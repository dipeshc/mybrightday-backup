package mybrightday

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

const (
	ficClientID    = "5VIzhuWNKxFc9etVvp5fonr2tlbBEZae"
	ficAuth0Domain = "bhloginsso.brighthorizons.com"
	ficRedirectURI = "https://familyinfocenter.brighthorizons.com/okta/callback"
	ficAudience    = "https://ShareservicesAPI"
	userAgent      = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.7727.138 Safari/537.36"
)

func generateRandomString(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Authenticate performs the multi-stage authentication flow to obtain a MyBrightDay session cookie.
//
// The HTTP client here is intentionally *not* wrapped with httpx.RetryTransport:
// each stage carries a single-use Auth0 state/code parameter, so a network blip
// followed by an automatic retry would re-submit a burned token and fail with a
// 4xx. The flow runs once at startup; on transient failure the whole run aborts.
func Authenticate(ctx context.Context, email, password string) (string, error) {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Don't follow redirects to the callback URI, we want to capture the code.
			if strings.HasPrefix(req.URL.String(), ficRedirectURI) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	// PKCE
	codeVerifier := generateRandomString(32)
	hash := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(hash[:])
	stateOIDC := generateRandomString(16)
	nonce := generateRandomString(16)

	// Stage 1: Start OIDC flow.
	slog.Debug("Stage 1: Starting Auth0 authorization flow", "email", email)
	authorizeURL := fmt.Sprintf("https://%s/authorize?client_id=%s&scope=openid+offline_access+profile+email&audience=%s&redirect_uri=%s&response_type=code&response_mode=query&state=%s&nonce=%s&code_challenge=%s&code_challenge_method=S256",
		ficAuth0Domain, ficClientID, url.QueryEscape(ficAudience), url.QueryEscape(ficRedirectURI), stateOIDC, nonce, codeChallenge)

	req, err := http.NewRequestWithContext(ctx, "GET", authorizeURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("starting authorize: %w", err)
	}
	resp.Body.Close()

	// Should have redirected to /u/login/identifier?state=...
	finalURL := resp.Header.Get("Location")
	if finalURL == "" {
		finalURL = resp.Request.URL.String()
	}
	if !strings.Contains(finalURL, "/u/login/identifier") {
		return "", fmt.Errorf("unexpected authorize redirect URL: %s", finalURL)
	}

	u, _ := url.Parse(finalURL)
	auth0State := u.Query().Get("state")
	if auth0State == "" {
		return "", fmt.Errorf("could not find Auth0 state in URL: %s", finalURL)
	}

	// Stage 2: Submit Identifier (Email).
	slog.Debug("Stage 2: Submitting email identifier")
	identifierURL := fmt.Sprintf("https://%s/u/login/identifier?state=%s", ficAuth0Domain, auth0State)

	val := url.Values{}
	val.Add("username", email)
	val.Add("action", "default")
	val.Add("state", auth0State)

	req, err = http.NewRequestWithContext(ctx, "POST", identifierURL, strings.NewReader(val.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)

	resp, err = client.Do(req)
	if err != nil {
		return "", fmt.Errorf("submitting identifier: %w", err)
	}
	resp.Body.Close()

	// Stage 3: Submit Password.
	slog.Debug("Stage 3: Submitting password")
	passwordURL := fmt.Sprintf("https://%s/u/login/password?state=%s", ficAuth0Domain, auth0State)

	val = url.Values{}
	val.Add("username", email)
	val.Add("password", password)
	val.Add("action", "default")
	val.Add("state", auth0State)

	req, err = http.NewRequestWithContext(ctx, "POST", passwordURL, strings.NewReader(val.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)

	resp, err = client.Do(req)
	if err != nil {
		return "", fmt.Errorf("submitting password: %w", err)
	}
	resp.Body.Close()

	// Should redirect back to ficRedirectURI with code.
	callbackURL := resp.Header.Get("Location")
	if callbackURL == "" {
		return "", fmt.Errorf("missing callback redirect after login")
	}

	u, err = url.Parse(callbackURL)
	if err != nil {
		return "", fmt.Errorf("parsing callback URL: %w", err)
	}
	code := u.Query().Get("code")
	if code == "" {
		return "", fmt.Errorf("no authorization code found in callback URL: %s", callbackURL)
	}

	// Stage 4: Exchange Code for FIC JWT.
	slog.Debug("Stage 4: Exchanging code for FIC JWT")
	tokenURL := fmt.Sprintf("https://%s/oauth/token", ficAuth0Domain)
	val = url.Values{}
	val.Add("grant_type", "authorization_code")
	val.Add("client_id", ficClientID)
	val.Add("code", code)
	val.Add("code_verifier", codeVerifier)
	val.Add("redirect_uri", ficRedirectURI)

	req, err = http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(val.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)

	resp, err = client.Do(req)
	if err != nil {
		return "", fmt.Errorf("exchanging code: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token exchange failed: %d: %s", resp.StatusCode, string(data))
	}

	bodyData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading token response body: %w", err)
	}

	var oauthResp struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
	}
	if err := json.Unmarshal(bodyData, &oauthResp); err != nil {
		return "", fmt.Errorf("decoding token response: %w", err)
	}

	ficJWT := oauthResp.AccessToken
	if ficJWT == "" {
		return "", fmt.Errorf("no valid token found in response")
	}

	// Stage 5: Exchange for MBD Token.
	slog.Debug("Stage 5: Exchanging FIC JWT for MBD Token")
	req, err = http.NewRequestWithContext(ctx, "GET", "https://mbdwgateway.brighthorizons.com/api/account/mbdtoken", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+ficJWT)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Origin", "https://familyinfocenter.brighthorizons.com")
	req.Header.Set("Referer", "https://familyinfocenter.brighthorizons.com/")
	req.Header.Set("User-Agent", userAgent)

	resp, err = client.Do(req)
	if err != nil {
		return "", fmt.Errorf("exchanging MBD token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("MBD token exchange failed: %d: %s", resp.StatusCode, string(data))
	}

	var tokenResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decoding MBD token response: %w", err)
	}

	// Stage 6: Initialize MBD Session.
	slog.Debug("Stage 6: Establishing MyBrightDay session")
	redirectURL := "https://mybrightday.brighthorizons.com/auth/jwt/redirect"
	req, err = http.NewRequestWithContext(ctx, "GET", redirectURL, nil)
	if err != nil {
		return "", err
	}
	q := req.URL.Query()
	q.Add("jwt", tokenResp.Token)
	req.URL.RawQuery = q.Encode()
	req.Header.Set("User-Agent", userAgent)

	// Switch back to default redirect handling for this one.
	client.CheckRedirect = nil
	resp, err = client.Do(req)
	if err != nil {
		return "", fmt.Errorf("initializing MBD session: %w", err)
	}
	defer resp.Body.Close()

	// Extract the session cookie.
	sessionCookie := ""
	destURL := resp.Request.URL

	for _, c := range jar.Cookies(destURL) {
		if c.Name == "session" {
			sessionCookie = c.Value
			break
		}
	}

	if sessionCookie == "" {
		return "", fmt.Errorf("session cookie not found after initialization (checked %s)", destURL.String())
	}

	return sessionCookie, nil
}
