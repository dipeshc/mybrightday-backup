package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/dipeshc/mybrightday-backup/internal/mybrightday"
	"github.com/dipeshc/mybrightday-backup/internal/oauthlogin"
	"github.com/dipeshc/mybrightday-backup/internal/storage/googlephotos"
)

// ErrLoginRequired is returned when a run cannot proceed without a browser
// login and no human is at the terminal. The login links have already been
// written to stdout as "login-url <provider> <url>" lines by then, so a
// wrapper (the GitHub workflow) can show them.
var ErrLoginRequired = errors.New("login required")

// LoginURLPrefix starts each line that announces a login link on stdout.
const LoginURLPrefix = "login-url"

// sessionWarnAhead is how far before the MyBrightDay session's expiry the
// run starts warning.
const sessionWarnAhead = 30 * 24 * time.Hour

// Seams so tests can stub the terminal check and the login flows.
var (
	isTerminal             = oauthlogin.IsTerminal
	mbdBeginLogin          = mybrightday.BeginLogin
	mbdFinishLogin         = mybrightday.FinishLogin
	mbdInteractiveLogin    = mybrightday.InteractiveLogin
	googleBeginLogin       = googlephotos.BeginLogin
	googleFinishLogin      = googlephotos.FinishLogin
	googleInteractiveLogin = googlephotos.InteractiveLogin
	deleteSession          = mybrightday.DeleteSession
	deleteGoogleToken      = googlephotos.DeleteRefreshToken
)

// ensureCredentials makes sure every credential the run needs is present:
// a MyBrightDay session always, a Google Photos refresh token when that
// backend is enabled.
//
// Pasted callback addresses complete logins that were started by an earlier
// run. What is still missing afterwards is obtained interactively when a
// human is at the terminal; otherwise a login link is issued for each and
// ErrLoginRequired is returned.
func ensureCredentials(ctx context.Context, cfg *Config, callbackURLs []string, stdout io.Writer) error {
	for _, raw := range callbackURLs {
		switch {
		case mybrightday.IsCallback(raw):
			if err := mbdFinishLogin(ctx, raw); err != nil {
				return fmt.Errorf("completing MyBrightDay login: %w", friendlyLoginError(err))
			}
			cfg.MyBrightDay.Session = mybrightday.LoadSession()
			slog.Info("Logged in to MyBrightDay")
		case googlephotos.IsCallback(raw):
			if err := googleFinishLogin(ctx, cfg.GooglePhotos, raw); err != nil {
				return fmt.Errorf("completing Google Photos login: %w", friendlyLoginError(err))
			}
			cfg.GooglePhotos.RefreshToken = googlephotos.LoadRefreshToken()
			slog.Info("Logged in to Google Photos")
		default:
			return fmt.Errorf("the callback address you pasted isn't one this tool issued.\nExpected an address starting with http://localhost:4200/okta/callback (MyBrightDay) or http://localhost:4201/google/callback (Google Photos)")
		}
	}

	needMBD := cfg.MyBrightDay.Session == ""
	needGoogle := cfg.GooglePhotos.Enabled && cfg.GooglePhotos.RefreshToken == ""
	if !needMBD && !needGoogle {
		return nil
	}

	if isTerminal() {
		if needMBD {
			if err := mbdInteractiveLogin(ctx, os.Stdin, stdout); err != nil {
				return fmt.Errorf("MyBrightDay login: %w", friendlyLoginError(err))
			}
			cfg.MyBrightDay.Session = mybrightday.LoadSession()
		}
		if needGoogle {
			if err := googleInteractiveLogin(ctx, cfg.GooglePhotos); err != nil {
				return err
			}
			cfg.GooglePhotos.RefreshToken = googlephotos.LoadRefreshToken()
		}
		return nil
	}

	// No human here: issue links for a later run to complete.
	if needMBD {
		u, err := mbdBeginLogin()
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s mybrightday %s\n", LoginURLPrefix, u)
	}
	if needGoogle {
		u, err := googleBeginLogin(cfg.GooglePhotos)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s google_photos %s\n", LoginURLPrefix, u)
	}
	return fmt.Errorf("%w: open the login link(s) above in a browser, log in, then run again with --callback-url <the address you land on> for each", ErrLoginRequired)
}

// sessionRejected handles the API refusing the stored session: discard it,
// and either explain (interactive) or issue a new login link right away so a
// workflow can show it in the same run.
func sessionRejected(cfg *Config, stdout io.Writer) error {
	if err := deleteSession(); err != nil {
		slog.Warn("Could not remove rejected MyBrightDay session", "path", mybrightday.SessionPath(), "error", err)
	}
	cfg.MyBrightDay.Session = ""
	if isTerminal() {
		return errors.New("MyBrightDay rejected the stored session. This happens when it expires (about a year after logging in) or after logging out of the website. It has been cleared; run mbdb again to log in")
	}
	u, err := mbdBeginLogin()
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s mybrightday %s\n", LoginURLPrefix, u)
	return fmt.Errorf("%w: MyBrightDay rejected the stored session (expired or logged out). It has been cleared; open the login link above, log in, then run again with --callback-url <the address you land on>", ErrLoginRequired)
}

// googleTokenRejected handles Google refusing the refresh token. When the
// token came from the stored file it is discarded and a new login is
// requested (a link right away when no human is present). When it came from
// the environment or a flag, the file is left alone, since deleting it would
// not change what the next run presents and a fresh login would be thrown
// away the same way; the error names the source instead.
func googleTokenRejected(ctx context.Context, cfg *Config, stdout io.Writer) error {
	if stored := googlephotos.LoadRefreshToken(); stored == "" || stored != cfg.GooglePhotos.RefreshToken {
		return errors.New("Google rejected the Google Photos login supplied via the GOOGLE_PHOTOS_REFRESH_TOKEN environment variable, the --googlephotos.refreshtoken flag, or config.yaml (revoked or expired). Remove it so a login made by this tool can be used, or replace it")
	}
	if err := deleteGoogleToken(); err != nil {
		slog.Warn("Could not remove rejected Google Photos token", "path", googlephotos.RefreshTokenPath(), "error", err)
	}
	cfg.GooglePhotos.RefreshToken = ""
	if isTerminal() {
		return errors.New("Google rejected the stored Google Photos login (revoked or expired). It has been cleared; run mbdb again to sign in to Google")
	}
	u, err := googleBeginLogin(cfg.GooglePhotos)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s google_photos %s\n", LoginURLPrefix, u)
	return fmt.Errorf("%w: Google rejected the stored Google Photos login (revoked or expired). It has been cleared; open the login link above, sign in, then run again with --callback-url <the address you land on>", ErrLoginRequired)
}

// warnIfSessionExpiringSoon logs a warning when the session cookie's embedded
// expiry is within sessionWarnAhead.
func warnIfSessionExpiringSoon(session string) {
	exp, ok := mybrightday.SessionExpiry(session)
	if !ok {
		return
	}
	if until := time.Until(exp); until < sessionWarnAhead {
		slog.Warn("MyBrightDay session expires soon; log in again before then to avoid a failed backup",
			"expires", exp.UTC().Format("2006-01-02"), "days_left", int(until.Hours()/24))
	}
}

// friendlyLoginError turns the login sentinel errors into messages that say
// what happened and what to do, without any internals.
func friendlyLoginError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, oauthlogin.ErrNoPendingLogin):
		return errors.New("no login is in progress on this machine. Run without --callback-url first to get a fresh login link")
	case errors.Is(err, oauthlogin.ErrCallbackNoCode):
		return errors.New("the address you pasted doesn't contain a login code. Expected the address you landed on after logging in; run without --callback-url to get a fresh login link")
	case errors.Is(err, oauthlogin.ErrCallbackStateMismatch):
		return errors.New("the address you pasted doesn't match the login link this machine issued. If you started the login more than once, use the most recent link; otherwise run without --callback-url to get a fresh one")
	case errors.Is(err, oauthlogin.ErrLoginLinkExpired):
		return errors.New("the login link has expired or was already used. Links last ten minutes; run without --callback-url to get a fresh one")
	default:
		return err
	}
}
