package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/dipesh/mybrightday-backup/internal/credential"
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

// AuthenticateMyBrightDay performs the multi-stage authentication flow to obtain a session cookie.
func AuthenticateMyBrightDay(ctx context.Context, email, password string) (string, error) {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Don't follow redirects to the callback URI, we want to capture the code
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

	// Stage 1: Start OIDC flow
	slog.Debug("Stage 1: Starting Auth0 authorization flow")
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
		// If it didn't redirect, maybe we are already there?
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

	// Stage 2: Submit Identifier (Email)
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

	// Stage 3: Submit Password
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

	// Should redirect back to ficRedirectURI with code
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

	// Stage 4: Exchange Code for FIC JWT
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

	// Stage 5: Exchange for MBD Token
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

	// Stage 6: Initialize MBD Session
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

	// Switch back to default redirect handling for this one
	client.CheckRedirect = nil
	resp, err = client.Do(req)
	if err != nil {
		return "", fmt.Errorf("initializing MBD session: %w", err)
	}
	defer resp.Body.Close()

	// Extract the session cookie
	sessionCookie := ""
	destURL := resp.Request.URL
	slog.Debug("Final MBD session URL", "url", destURL.String())

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

// buildOAuthConfig returns an OAuth2 config using the client_secret JSON string if provided,
// otherwise falls back to the obfuscated credentials embedded in the binary.
func buildOAuthConfig(cfg *RunConfig) (*oauth2.Config, error) {
	if cfg.GooglePhotos.ClientSecret != "" {
		oauthCfg, err := google.ConfigFromJSON([]byte(cfg.GooglePhotos.ClientSecret), oauthScopes...)
		if err != nil {
			return nil, fmt.Errorf("parsing client secret JSON: %w", err)
		}
		return oauthCfg, nil
	}
	return &oauth2.Config{
		ClientID:     credential.ClientID,
		ClientSecret: credential.ClientSecret(),
		Endpoint:     google.Endpoint,
		Scopes:       oauthScopes,
	}, nil
}

// oauthScopes defines the Google API scopes needed by this tool.
var oauthScopes = []string{
	"https://www.googleapis.com/auth/photoslibrary.appendonly",
	"https://www.googleapis.com/auth/photoslibrary.readonly.appcreateddata",
}

// getOAuthClient creates an authenticated HTTP client using OAuth2.
// It requires a valid token to exist, otherwise it returns an error.
func getOAuthClient(ctx context.Context, cfg *RunConfig) (*http.Client, error) {
	oauthCfg, err := buildOAuthConfig(cfg)
	if err != nil {
		return nil, err
	}

	if cfg.GooglePhotos.TokenSecret == "" {
		binaryName := filepath.Base(os.Args[0])
		return nil, fmt.Errorf("token_secret missing. Please run '%s init --google-photos' to authenticate", binaryName)
	}

	tok := &oauth2.Token{}
	if err := json.Unmarshal([]byte(cfg.GooglePhotos.TokenSecret), tok); err != nil {
		return nil, fmt.Errorf("decoding token_secret JSON: %w", err)
	}

	return oauthCfg.Client(ctx, tok), nil
}

// PerformInitAuth performs the interactive authorization flow and returns the obtained token.
func PerformInitAuth(ctx context.Context, cfg *RunConfig) (*oauth2.Token, error) {
	oauthCfg, err := buildOAuthConfig(cfg)
	if err != nil {
		return nil, err
	}

	tok, err := getTokenFromWeb(ctx, oauthCfg)
	if err != nil {
		return nil, fmt.Errorf("obtaining token: %w", err)
	}

	return tok, nil
}

// getTokenFromWeb performs the OAuth2 authorization code flow by starting a temporary
// local server and automatically opening the user's browser.
func getTokenFromWeb(ctx context.Context, config *oauth2.Config) (*oauth2.Token, error) {
	// Find an available local port.
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, fmt.Errorf("starting local listener: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	config.RedirectURL = fmt.Sprintf("http://localhost:%d", port)

	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				w.WriteHeader(http.StatusNotFound)
				return
			}

			code := r.URL.Query().Get("code")
			if code == "" {
				errStr := r.URL.Query().Get("error")
				select {
				case errChan <- fmt.Errorf("google returned error: %s", errStr):
				default:
				}
				fmt.Fprint(w, "Authentication failed. You can close this window.")
				return
			}

			select {
			case codeChan <- code:
			default:
			}
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "Authentication successful! You can close this window now.")
		}),
	}

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			select {
			case errChan <- fmt.Errorf("http server error: %w", err):
			default:
			}
		}
	}()
	defer func() {
		// Use a very short timeout for shutdown to avoid visible delay.
		// For this local one-shot server, we don't need a long graceful period.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	slog.Info("Opening browser for authorization...")
	if err := openBrowser(authURL); err != nil {
		slog.Error("Failed to open browser", "error", err)
		fmt.Printf("Please open this URL manually:\n\n%s\n\n", authURL)
	}

	select {
	case code := <-codeChan:
		tok, err := config.Exchange(ctx, code)
		if err == nil {
			slog.Info("Successfully authenticated and retrieved token!")
		}
		return tok, err
	case err := <-errChan:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("timeout waiting for authorization")
	}
}

// openBrowser opens the specified URL in the user's default browser.
func openBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default: // Linux and others
		cmd = "xdg-open"
		args = []string{url}
	}

	return exec.Command(cmd, args...).Start()
}
