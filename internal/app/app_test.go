package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mp4 "github.com/abema/go-mp4"

	"github.com/dipeshc/mybrightday-backup/internal/mybrightday"
	"github.com/dipeshc/mybrightday-backup/internal/storage/googlephotos"
)

func TestParseDateString(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	inTwoDays := time.Now().AddDate(0, 0, 2).Format("2006-01-02")

	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", today, false},
		{"0", today, false},
		{"-1", yesterday, false},
		{"+2", inTwoDays, false},
		{"2024-01-15", "2024-01-15", false},
		{"15/01/2024", "", true},
		{"2024-13-45", "", true},
	}
	for _, tt := range tests {
		got, err := parseDateString(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseDateString(%q) err = %v, wantErr %t", tt.in, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("parseDateString(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseDates(t *testing.T) {
	t.Run("single date", func(t *testing.T) {
		start, end, startTime, endTime, err := parseDates("2024-01-15")
		if err != nil {
			t.Fatalf("parseDates: %v", err)
		}
		if start != "2024-01-15" || end != "2024-01-15" {
			t.Errorf("range = %s..%s", start, end)
		}
		if !startTime.Equal(endTime) {
			t.Errorf("times differ: %v vs %v", startTime, endTime)
		}
	})

	t.Run("range", func(t *testing.T) {
		start, end, startTime, endTime, err := parseDates("2024-01-10:2024-01-20")
		if err != nil {
			t.Fatalf("parseDates: %v", err)
		}
		if start != "2024-01-10" || end != "2024-01-20" {
			t.Errorf("range = %s..%s", start, end)
		}
		if !endTime.After(startTime) {
			t.Errorf("endTime %v not after startTime %v", endTime, startTime)
		}
	})

	t.Run("invalid start", func(t *testing.T) {
		if _, _, _, _, err := parseDates("garbage:2024-01-20"); err == nil {
			t.Error("expected error for invalid start date")
		}
	})

	t.Run("invalid end", func(t *testing.T) {
		if _, _, _, _, err := parseDates("2024-01-10:garbage"); err == nil {
			t.Error("expected error for invalid end date")
		}
	})
}

// stubSession replaces the session validation seam. Tests using it must
// not run in parallel.
func stubSession(t *testing.T, err error) {
	t.Helper()
	original := checkSession
	checkSession = func(ctx context.Context, c *mybrightday.Client) error { return err }
	t.Cleanup(func() { checkSession = original })
}

// stubTerminal controls whether the run believes a human is at the keyboard.
func stubTerminal(t *testing.T, interactive bool) {
	t.Helper()
	original := isTerminal
	isTerminal = func() bool { return interactive }
	t.Cleanup(func() { isTerminal = original })
}

// loginCalls records what the login seams were asked to do.
type loginCalls struct {
	mbdBegins, googleBegins           int
	mbdFinished, googleFinished       []string
	mbdInteractive, googleInteractive int
	sessionDeleted                    int
	googleDeleted                     int
}

// stubLogins replaces every login seam with recorders that behave like a
// successful login (writing the credential files the real ones would).
func stubLogins(t *testing.T) *loginCalls {
	t.Helper()
	calls := &loginCalls{}
	origs := []any{mbdBeginLogin, mbdFinishLogin, mbdInteractiveLogin, googleBeginLogin, googleFinishLogin, googleInteractiveLogin, deleteSession}
	mbdBeginLogin = func() (string, error) {
		calls.mbdBegins++
		return "https://auth0.example/authorize?state=mbd", nil
	}
	mbdFinishLogin = func(ctx context.Context, raw string) error {
		calls.mbdFinished = append(calls.mbdFinished, raw)
		return mybrightday.SaveSession("session-from-callback")
	}
	mbdInteractiveLogin = func(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
		calls.mbdInteractive++
		return mybrightday.SaveSession("session-from-browser")
	}
	googleBeginLogin = func(cfg googlephotos.Config) (string, error) {
		calls.googleBegins++
		return "https://google.example/auth?state=g", nil
	}
	googleFinishLogin = func(ctx context.Context, cfg googlephotos.Config, raw string) error {
		calls.googleFinished = append(calls.googleFinished, raw)
		return googlephotos.SaveRefreshToken("google-from-callback")
	}
	googleInteractiveLogin = func(ctx context.Context, cfg googlephotos.Config) error {
		calls.googleInteractive++
		return googlephotos.SaveRefreshToken("google-from-browser")
	}
	deleteSession = func() error {
		calls.sessionDeleted++
		return mybrightday.DeleteSession()
	}
	origDeleteGoogle := deleteGoogleToken
	deleteGoogleToken = func() error {
		calls.googleDeleted++
		return googlephotos.DeleteRefreshToken()
	}
	t.Cleanup(func() { deleteGoogleToken = origDeleteGoogle })
	t.Cleanup(func() {
		mbdBeginLogin = origs[0].(func() (string, error))
		mbdFinishLogin = origs[1].(func(context.Context, string) error)
		mbdInteractiveLogin = origs[2].(func(context.Context, io.Reader, io.Writer) error)
		googleBeginLogin = origs[3].(func(googlephotos.Config) (string, error))
		googleFinishLogin = origs[4].(func(context.Context, googlephotos.Config, string) error)
		googleInteractiveLogin = origs[5].(func(context.Context, googlephotos.Config) error)
		deleteSession = origs[6].(func() error)
	})
	return calls
}

// stubAuth is the common happy-path setup: a valid stored session.
func stubAuth(t *testing.T, cookie string, err error) {
	t.Helper()
	stubSession(t, err)
	stubTerminal(t, false)
	t.Setenv("CONFIG_FILES_DIR", t.TempDir())
}

func tinyJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatalf("encoding JPEG: %v", err)
	}
	return buf.Bytes()
}

// tinyMP4 builds a minimal valid MP4 (ftyp + moov/mvhd), just enough for
// processor.AddMP4Metadata to rewrite it.
func tinyMP4(t *testing.T) []byte {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.mp4")
	if err != nil {
		t.Fatalf("creating temp mp4: %v", err)
	}
	defer f.Close()

	w := mp4.NewWriter(f)
	start := func(bt mp4.BoxType) {
		if _, err := w.StartBox(&mp4.BoxInfo{Type: bt}); err != nil {
			t.Fatalf("starting %s box: %v", bt, err)
		}
	}
	end := func() {
		if _, err := w.EndBox(); err != nil {
			t.Fatalf("ending box: %v", err)
		}
	}
	marshal := func(payload mp4.IImmutableBox) {
		if _, err := mp4.Marshal(w, payload, mp4.Context{}); err != nil {
			t.Fatalf("marshaling box: %v", err)
		}
	}

	start(mp4.BoxTypeFtyp())
	marshal(&mp4.Ftyp{
		MajorBrand:       [4]byte{'i', 's', 'o', 'm'},
		MinorVersion:     512,
		CompatibleBrands: []mp4.CompatibleBrandElem{{CompatibleBrand: [4]byte{'i', 's', 'o', 'm'}}},
	})
	end()
	start(mp4.BoxTypeMoov())
	start(mp4.BoxTypeMvhd())
	marshal(&mp4.Mvhd{Timescale: 1000, Rate: 0x00010000, Volume: 0x0100, NextTrackID: 2})
	end()
	end()

	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("reading temp mp4: %v", err)
	}
	return data
}

// mbdServerOptions tweaks individual endpoints of the fake MyBrightDay API.
type mbdServerOptions struct {
	dependents       []map[string]string
	dependentsStatus int
	centerStatus     int
	timezone         string
	mediaStatus      int
	media            []map[string]string
	attachmentStatus int
}

func startMBDServer(t *testing.T, opts mbdServerOptions) *httptest.Server {
	t.Helper()
	jpegData := tinyJPEG(t)
	mp4Data := tinyMP4(t)

	if opts.timezone == "" {
		opts.timezone = "America/Los_Angeles"
	}
	if opts.dependents == nil {
		opts.dependents = []map[string]string{{"id": "d1", "center": "c1"}}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/user/profile", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"id": "g1"})
	})
	mux.HandleFunc("/api/v2/dependents/guardian/g1", func(w http.ResponseWriter, r *http.Request) {
		if opts.dependentsStatus != 0 {
			http.Error(w, "nope", opts.dependentsStatus)
			return
		}
		json.NewEncoder(w).Encode(opts.dependents)
	})
	mux.HandleFunc("/api/v2/center/c1", func(w http.ResponseWriter, r *http.Request) {
		if opts.centerStatus != 0 {
			http.Error(w, "nope", opts.centerStatus)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id": "c1", "name": "Sunny Daycare", "timezone": opts.timezone,
			"address": map[string]string{
				"address_line_1": "1 Main St", "city": "Springfield",
				"state": "CA", "postal_code": "90001",
			},
		})
	})
	mux.HandleFunc("/api/v2/dependent/memories/media", func(w http.ResponseWriter, r *http.Request) {
		if opts.mediaStatus != 0 {
			http.Error(w, "nope", opts.mediaStatus)
			return
		}
		json.NewEncoder(w).Encode(opts.media)
	})
	mux.HandleFunc("/remote/v1/file_attachment", func(w http.ResponseWriter, r *http.Request) {
		if opts.attachmentStatus != 0 {
			http.Error(w, "gone", opts.attachmentStatus)
			return
		}
		key := r.URL.Query().Get("key")
		if strings.HasPrefix(key, "pdf") {
			w.Header().Set("Content-Type", "application/pdf")
			w.Write([]byte("%PDF-fake"))
			return
		}
		if strings.HasPrefix(key, "broken") {
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("not a real image"))
			return
		}
		if strings.HasPrefix(key, "vid") {
			w.Header().Set("Content-Type", "video/mp4")
			w.Write(mp4Data)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegData)
	})

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func testDownloadConfig(t *testing.T, baseURL string) *Config {
	t.Helper()
	cfg := NewDefaultConfig()
	cfg.MyBrightDay.Session = "stored-session"
	cfg.MyBrightDay.BaseURL = baseURL
	cfg.Date = "2024-01-15:2024-01-17"
	cfg.Local.Directory = t.TempDir()
	cfg.GooglePhotos.Enabled = false
	cfg.LocationOverride = &LocationOverrideConfig{Latitude: 37.42, Longitude: -122.08}
	return cfg
}

func TestDownloadHappyPath(t *testing.T) {
	stubAuth(t, "session-cookie", nil)
	// 20:00 UTC is 12:00 PST: capture dates survive the timezone conversion.
	// Two dates exercise the daily-stats rollover; the PDF attachment must be
	// skipped without counting as an error.
	ts := startMBDServer(t, mbdServerOptions{
		media: []map[string]string{
			{"attachment_id": "att1", "capture_time": "2024-01-15T20:00:00"},
			{"attachment_id": "att2", "capture_time": "2024-01-16T20:00:00"},
			{"attachment_id": "vid1", "capture_time": "2024-01-16T20:30:00"},
			{"attachment_id": "pdf1", "capture_time": "2024-01-16T21:00:00"},
		},
	})

	cfg := testDownloadConfig(t, ts.URL)
	if err := Download(context.Background(), cfg, RunOptions{}); err != nil {
		t.Fatalf("Download: %v", err)
	}

	for _, want := range []string{
		filepath.Join("2024-01-15", "daycare_2024-01-15_att1.jpg"),
		filepath.Join("2024-01-16", "daycare_2024-01-16_att2.jpg"),
		filepath.Join("2024-01-16", "daycare_2024-01-16_vid1.mp4"),
	} {
		path := filepath.Join(cfg.Local.Directory, want)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected media at %s: %v", want, err)
		}
	}

	// Only the image and video attachments may exist; the PDF must not be saved.
	var count int
	filepath.WalkDir(cfg.Local.Directory, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			count++
		}
		return nil
	})
	if count != 3 {
		t.Errorf("saved files = %d, want 3", count)
	}
}

func TestDownloadDryRun(t *testing.T) {
	stubAuth(t, "session-cookie", nil)
	ts := startMBDServer(t, mbdServerOptions{
		media: []map[string]string{
			{"attachment_id": "att1", "capture_time": "2024-01-15T20:00:00"},
		},
	})

	cfg := testDownloadConfig(t, ts.URL)
	cfg.DryRun = true
	if err := Download(context.Background(), cfg, RunOptions{}); err != nil {
		t.Fatalf("Download: %v", err)
	}

	entries, err := os.ReadDir(cfg.Local.Directory)
	if err != nil {
		t.Fatalf("reading dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("dry-run wrote %d entries, want 0", len(entries))
	}
}

func TestDownloadPerItemErrors(t *testing.T) {
	t.Run("attachment download fails", func(t *testing.T) {
		stubAuth(t, "session-cookie", nil)
		ts := startMBDServer(t, mbdServerOptions{
			// Non-retryable status: a 5xx would stall on retries.
			attachmentStatus: http.StatusNotFound,
			media: []map[string]string{
				{"attachment_id": "att1", "capture_time": "2024-01-15T20:00:00"},
			},
		})

		err := Download(context.Background(), testDownloadConfig(t, ts.URL), RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "per-item error") {
			t.Errorf("err = %v, want per-item error summary", err)
		}
	})

	t.Run("image conversion fails", func(t *testing.T) {
		stubAuth(t, "session-cookie", nil)
		ts := startMBDServer(t, mbdServerOptions{
			media: []map[string]string{
				{"attachment_id": "broken1", "capture_time": "2024-01-15T20:00:00"},
			},
		})

		err := Download(context.Background(), testDownloadConfig(t, ts.URL), RunOptions{})
		if err == nil || !strings.Contains(err.Error(), "per-item error") {
			t.Errorf("err = %v, want per-item error summary", err)
		}
	})
}

func TestDownloadErrors(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(t *testing.T) *Config
		errContains string
	}{
		{
			name: "not logged in and no terminal",
			setup: func(t *testing.T) *Config {
				stubTerminal(t, false)
				stubLogins(t)
				t.Setenv("CONFIG_FILES_DIR", t.TempDir())
				cfg := NewDefaultConfig()
				cfg.Date = "2024-01-15"
				return cfg
			},
			errContains: "login required",
		},
		{
			name: "invalid date",
			setup: func(t *testing.T) *Config {
				stubAuth(t, "session-cookie", nil)
				cfg := testDownloadConfig(t, "http://unused.test")
				cfg.Date = "not-a-date"
				return cfg
			},
			errContains: "invalid date format",
		},
		{
			name: "authentication failure",
			setup: func(t *testing.T) *Config {
				stubAuth(t, "", fmt.Errorf("session check returned status 502"))
				return testDownloadConfig(t, "http://unused.test")
			},
			errContains: "checking MyBrightDay session",
		},
		{
			name: "local storage init failure",
			setup: func(t *testing.T) *Config {
				stubAuth(t, "session-cookie", nil)
				cfg := testDownloadConfig(t, "http://unused.test")
				blocker := filepath.Join(t.TempDir(), "blocker")
				if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
				cfg.Local.Directory = filepath.Join(blocker, "photos")
				return cfg
			},
			errContains: "initialising local storage",
		},
		{
			name: "google photos enabled without a token and no terminal",
			setup: func(t *testing.T) *Config {
				stubAuth(t, "session-cookie", nil)
				stubLogins(t)
				cfg := testDownloadConfig(t, "http://unused.test")
				cfg.GooglePhotos.Enabled = true
				// No refresh token: a Google login link is issued instead of
				// attempting the backend.
				return cfg
			},
			errContains: "login required",
		},
		{
			name: "no dependents",
			setup: func(t *testing.T) *Config {
				stubAuth(t, "session-cookie", nil)
				ts := startMBDServer(t, mbdServerOptions{dependents: []map[string]string{}})
				return testDownloadConfig(t, ts.URL)
			},
			errContains: "no dependents found",
		},
		{
			name: "dependents endpoint fails",
			setup: func(t *testing.T) *Config {
				stubAuth(t, "session-cookie", nil)
				ts := startMBDServer(t, mbdServerOptions{dependentsStatus: http.StatusNotFound})
				return testDownloadConfig(t, ts.URL)
			},
			errContains: "getting dependent IDs",
		},
		{
			name: "center endpoint fails",
			setup: func(t *testing.T) *Config {
				stubAuth(t, "session-cookie", nil)
				ts := startMBDServer(t, mbdServerOptions{centerStatus: http.StatusNotFound})
				return testDownloadConfig(t, ts.URL)
			},
			errContains: "getting center info",
		},
		{
			name: "bad timezone",
			setup: func(t *testing.T) *Config {
				stubAuth(t, "session-cookie", nil)
				ts := startMBDServer(t, mbdServerOptions{timezone: "Not/AZone"})
				return testDownloadConfig(t, ts.URL)
			},
			errContains: "loading timezone",
		},
		{
			name: "media endpoint fails",
			setup: func(t *testing.T) *Config {
				stubAuth(t, "session-cookie", nil)
				ts := startMBDServer(t, mbdServerOptions{mediaStatus: http.StatusNotFound})
				return testDownloadConfig(t, ts.URL)
			},
			errContains: "getting media",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.setup(t)
			err := Download(context.Background(), cfg, RunOptions{})
			if err == nil || !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("err = %v, want containing %q", err, tt.errContains)
			}
		})
	}
}

func TestNoTerminalPrintsLoginLinksAndStops(t *testing.T) {
	stubTerminal(t, false)
	calls := stubLogins(t)
	t.Setenv("CONFIG_FILES_DIR", t.TempDir())
	cfg := NewDefaultConfig()
	cfg.Date = "2024-01-15"
	cfg.GooglePhotos.Enabled = true

	var out bytes.Buffer
	err := Download(context.Background(), cfg, RunOptions{Stdout: &out})
	if !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("err = %v, want ErrLoginRequired", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "login-url mybrightday https://") || !strings.HasPrefix(lines[1], "login-url google_photos https://") {
		t.Errorf("stdout = %q, want one login-url line per missing credential", out.String())
	}
	if calls.mbdBegins != 1 || calls.googleBegins != 1 || calls.mbdInteractive != 0 || calls.googleInteractive != 0 {
		t.Errorf("calls = %+v, want one begin per provider and no interactive login", *calls)
	}
}

func TestNoTerminalSkipsGoogleWhenDisabled(t *testing.T) {
	stubTerminal(t, false)
	calls := stubLogins(t)
	t.Setenv("CONFIG_FILES_DIR", t.TempDir())
	cfg := NewDefaultConfig()
	cfg.Date = "2024-01-15"

	var out bytes.Buffer
	_ = Download(context.Background(), cfg, RunOptions{Stdout: &out})
	if calls.googleBegins != 0 || strings.Contains(out.String(), "google") {
		t.Errorf("Google login offered although Google Photos is disabled: %q", out.String())
	}
}

func TestCallbackURLsCompleteLoginsThenRun(t *testing.T) {
	stubTerminal(t, false)
	stubSession(t, nil)
	calls := stubLogins(t)
	t.Setenv("CONFIG_FILES_DIR", t.TempDir())
	ts := startMBDServer(t, mbdServerOptions{})
	cfg := NewDefaultConfig()
	cfg.MyBrightDay.BaseURL = ts.URL
	cfg.Date = "2024-01-15:2024-01-17"
	cfg.Local.Directory = t.TempDir()
	cfg.LocationOverride = &LocationOverrideConfig{Latitude: 37.42, Longitude: -122.08}

	err := Download(context.Background(), cfg, RunOptions{
		CallbackURLs: []string{"http://localhost:4200/okta/callback?code=a&state=b"},
		Stdout:       &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if len(calls.mbdFinished) != 1 || calls.mbdBegins != 0 {
		t.Errorf("calls = %+v, want the callback consumed and no new login issued", *calls)
	}
	if cfg.MyBrightDay.Session != "session-from-callback" {
		t.Errorf("session after callback = %q", cfg.MyBrightDay.Session)
	}
}

func TestUnknownCallbackURLIsRejected(t *testing.T) {
	stubTerminal(t, false)
	stubLogins(t)
	t.Setenv("CONFIG_FILES_DIR", t.TempDir())
	cfg := testDownloadConfig(t, "http://unused.test")
	err := Download(context.Background(), cfg, RunOptions{CallbackURLs: []string{"https://example.com/?code=x"}, Stdout: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "isn't one this tool issued") {
		t.Errorf("err = %v, want unknown-callback message", err)
	}
}

func TestTerminalRunsInteractiveLogins(t *testing.T) {
	stubTerminal(t, true)
	stubSession(t, nil)
	calls := stubLogins(t)
	t.Setenv("CONFIG_FILES_DIR", t.TempDir())
	ts := startMBDServer(t, mbdServerOptions{})
	cfg := NewDefaultConfig()
	cfg.MyBrightDay.BaseURL = ts.URL
	cfg.Date = "2024-01-15:2024-01-17"
	cfg.Local.Directory = t.TempDir()
	cfg.GooglePhotos.Enabled = false
	cfg.LocationOverride = &LocationOverrideConfig{Latitude: 37.42, Longitude: -122.08}

	if err := Download(context.Background(), cfg, RunOptions{Stdout: &bytes.Buffer{}}); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if calls.mbdInteractive != 1 || calls.mbdBegins != 0 {
		t.Errorf("calls = %+v, want one interactive login and no link issued", *calls)
	}
	if cfg.MyBrightDay.Session != "session-from-browser" {
		t.Errorf("session after login = %q", cfg.MyBrightDay.Session)
	}
}

func TestRejectedSessionIsClearedAndLinkIssued(t *testing.T) {
	stubTerminal(t, false)
	stubSession(t, mybrightday.ErrSessionRejected)
	calls := stubLogins(t)
	t.Setenv("CONFIG_FILES_DIR", t.TempDir())
	if err := mybrightday.SaveSession("stale"); err != nil {
		t.Fatal(err)
	}
	cfg := testDownloadConfig(t, "http://unused.test")
	cfg.MyBrightDay.Session = "stale"

	var out bytes.Buffer
	err := Download(context.Background(), cfg, RunOptions{Stdout: &out})
	if !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("err = %v, want ErrLoginRequired", err)
	}
	if !strings.Contains(err.Error(), "rejected the stored session") {
		t.Errorf("err = %v, want rejection explained", err)
	}
	if strings.Contains(err.Error(), "stale") || strings.Contains(out.String(), "stale") {
		t.Error("output leaks the session value")
	}
	if calls.sessionDeleted != 1 || mybrightday.LoadSession() != "" {
		t.Errorf("deleted = %d, stored = %q; want the dead session removed", calls.sessionDeleted, mybrightday.LoadSession())
	}
	if !strings.HasPrefix(out.String(), "login-url mybrightday ") {
		t.Errorf("stdout = %q, want a fresh login link in the same run", out.String())
	}
}

func TestRejectedSessionInteractiveExplains(t *testing.T) {
	stubTerminal(t, true)
	stubSession(t, mybrightday.ErrSessionRejected)
	calls := stubLogins(t)
	t.Setenv("CONFIG_FILES_DIR", t.TempDir())
	cfg := testDownloadConfig(t, "http://unused.test")

	err := Download(context.Background(), cfg, RunOptions{Stdout: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "run mbdb again to log in") {
		t.Errorf("err = %v, want guidance to run again", err)
	}
	if errors.Is(err, ErrLoginRequired) {
		t.Error("interactive rejection must not use the login-required exit path")
	}
	if calls.sessionDeleted != 1 {
		t.Errorf("deleted = %d, want 1", calls.sessionDeleted)
	}
}

func TestSessionExpiryWarning(t *testing.T) {
	var buf bytes.Buffer
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(orig) })

	soon := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"expires":%d}`, time.Now().Add(10*24*time.Hour).Unix()))) + ".x.y"
	warnIfSessionExpiringSoon(soon)
	if !strings.Contains(buf.String(), "expires soon") {
		t.Errorf("no warning for a session expiring in 10 days: %q", buf.String())
	}
	buf.Reset()
	far := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"expires":%d}`, time.Now().Add(300*24*time.Hour).Unix()))) + ".x.y"
	warnIfSessionExpiringSoon(far)
	if strings.Contains(buf.String(), "expires soon") {
		t.Errorf("unexpected warning for a session expiring in 300 days: %q", buf.String())
	}
}

func TestRejectedGoogleTokenIsClearedAndLinkIssued(t *testing.T) {
	stubTerminal(t, false)
	stubSession(t, nil)
	calls := stubLogins(t)
	t.Setenv("CONFIG_FILES_DIR", t.TempDir())
	if err := googlephotos.SaveRefreshToken("g0tok-stored-secret"); err != nil {
		t.Fatal(err)
	}
	orig := newGooglePhotos
	newGooglePhotos = func(ctx context.Context, cfg googlephotos.Config, dryRun bool) (*googlephotos.GooglePhotosStorage, error) {
		return nil, fmt.Errorf("initialising: %w", googlephotos.ErrTokenRejected)
	}
	t.Cleanup(func() { newGooglePhotos = orig })

	cfg := testDownloadConfig(t, "http://unused.test")
	cfg.GooglePhotos.Enabled = true
	cfg.GooglePhotos.RefreshToken = "g0tok-stored-secret"

	var out bytes.Buffer
	err := Download(context.Background(), cfg, RunOptions{Stdout: &out})
	if !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("err = %v, want ErrLoginRequired", err)
	}
	if calls.googleDeleted != 1 || googlephotos.LoadRefreshToken() != "" {
		t.Errorf("deleted = %d, stored = %q; want the dead token removed", calls.googleDeleted, googlephotos.LoadRefreshToken())
	}
	if !strings.HasPrefix(out.String(), "login-url google_photos ") {
		t.Errorf("stdout = %q, want a fresh Google login link", out.String())
	}
	if strings.Contains(err.Error()+out.String(), "g0tok-stored-secret") {
		t.Error("output leaks the token value")
	}
}

func TestRejectedGoogleTokenFromEnvLeavesStoredFileAlone(t *testing.T) {
	stubTerminal(t, false)
	stubSession(t, nil)
	calls := stubLogins(t)
	t.Setenv("CONFIG_FILES_DIR", t.TempDir())
	if err := googlephotos.SaveRefreshToken("g0tok-good-stored"); err != nil {
		t.Fatal(err)
	}
	orig := newGooglePhotos
	newGooglePhotos = func(ctx context.Context, cfg googlephotos.Config, dryRun bool) (*googlephotos.GooglePhotosStorage, error) {
		return nil, fmt.Errorf("initialising: %w", googlephotos.ErrTokenRejected)
	}
	t.Cleanup(func() { newGooglePhotos = orig })

	cfg := testDownloadConfig(t, "http://unused.test")
	cfg.GooglePhotos.Enabled = true
	// The run is using a different token than the stored one, i.e. it came
	// from the environment or a flag.
	cfg.GooglePhotos.RefreshToken = "g0tok-dead-from-env"

	var out bytes.Buffer
	err := Download(context.Background(), cfg, RunOptions{Stdout: &out})
	if err == nil || errors.Is(err, ErrLoginRequired) || !strings.Contains(err.Error(), "environment variable") {
		t.Errorf("err = %v, want a plain error naming the env/flag source", err)
	}
	if calls.googleDeleted != 0 || googlephotos.LoadRefreshToken() != "g0tok-good-stored" {
		t.Errorf("deleted = %d, stored = %q; the stored login must be left alone", calls.googleDeleted, googlephotos.LoadRefreshToken())
	}
	if calls.googleBegins != 0 || out.Len() != 0 {
		t.Errorf("a login link was issued (%q) although the stored login was not the problem", out.String())
	}
	if strings.Contains(err.Error(), "g0tok") {
		t.Error("error leaks a token value")
	}
}
