package mybrightday

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Error-path fakes must respond with non-retryable statuses (404/400): the
// client's RetryTransport uses the default 1s/5-attempt policy, so a 5xx
// would stall the test for ~15 seconds.

func TestNewClientCookieNormalisation(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"abc", "session=abc"},
		{"session=abc", "session=abc"},
		{"", ""},
	}
	for _, tt := range tests {
		c := NewClient("http://example.test", tt.in)
		if c.cookie != tt.want {
			t.Errorf("cookie %q normalised to %q, want %q", tt.in, c.cookie, tt.want)
		}
	}
}

func TestGetCenterInfo(t *testing.T) {
	center := Center{ID: "c1", Name: "Sunny Daycare", Timezone: "America/Los_Angeles"}
	center.Address.AddressLine1 = "1 Main St"
	center.Address.City = "Springfield"
	center.Address.State = "CA"
	center.Address.PostalCode = "90001"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/center/c1" {
			t.Errorf("path = %q, want /api/v2/center/c1", r.URL.Path)
		}
		if got := r.Header.Get("Cookie"); got != "session=tok" {
			t.Errorf("cookie = %q, want session=tok", got)
		}
		json.NewEncoder(w).Encode(center)
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "tok")
	got, err := c.GetCenterInfo(context.Background(), "c1")
	if err != nil {
		t.Fatalf("GetCenterInfo: %v", err)
	}
	if got.Name != "Sunny Daycare" || got.Timezone != "America/Los_Angeles" || got.Address.City != "Springfield" {
		t.Errorf("center = %+v", got)
	}
}

func TestGetCenterInfoErrors(t *testing.T) {
	t.Run("non-200", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "nope", http.StatusNotFound)
		}))
		defer ts.Close()

		_, err := NewClient(ts.URL, "tok").GetCenterInfo(context.Background(), "c1")
		if err == nil || !strings.Contains(err.Error(), "status 404") {
			t.Errorf("err = %v, want status 404 error", err)
		}
	})

	t.Run("bad json", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("{not json"))
		}))
		defer ts.Close()

		_, err := NewClient(ts.URL, "tok").GetCenterInfo(context.Background(), "c1")
		if err == nil || !strings.Contains(err.Error(), "parsing center") {
			t.Errorf("err = %v, want parsing center error", err)
		}
	})
}

func TestGetDependentIDs(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/user/profile":
			json.NewEncoder(w).Encode(guardianProfile{ID: "g1"})
		case "/api/v2/dependents/guardian/g1":
			json.NewEncoder(w).Encode([]dependent{
				{ID: "d1", Center: "c1"},
				{ID: "d2", Center: "c2"},
			})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	ids, centers, err := NewClient(ts.URL, "tok").GetDependentIDs(context.Background())
	if err != nil {
		t.Fatalf("GetDependentIDs: %v", err)
	}
	if len(ids) != 2 || ids[0] != "d1" || ids[1] != "d2" {
		t.Errorf("ids = %v, want [d1 d2]", ids)
	}
	if centers["d1"] != "c1" || centers["d2"] != "c2" {
		t.Errorf("centers = %v", centers)
	}
}

func TestGetDependentIDsErrors(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		errContains string
	}{
		{
			name: "profile fails",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "denied", http.StatusUnauthorized)
			},
			errContains: "fetching profile",
		},
		{
			name: "profile bad json",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("{oops"))
			},
			errContains: "parsing profile",
		},
		{
			name: "dependents fails",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v2/user/profile" {
					json.NewEncoder(w).Encode(guardianProfile{ID: "g1"})
					return
				}
				http.Error(w, "nope", http.StatusNotFound)
			},
			errContains: "fetching dependents",
		},
		{
			name: "dependents bad json",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v2/user/profile" {
					json.NewEncoder(w).Encode(guardianProfile{ID: "g1"})
					return
				}
				w.Write([]byte("[broken"))
			},
			errContains: "parsing dependents",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(tt.handler)
			defer ts.Close()

			_, _, err := NewClient(ts.URL, "tok").GetDependentIDs(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("err = %v, want containing %q", err, tt.errContains)
			}
		})
	}
}

func TestGetMediaForDateRange(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/dependent/memories/media" {
			t.Errorf("path = %q", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("start_date") != "2024-01-01" || q.Get("end_date") != "2024-01-31" {
			t.Errorf("dates = %s..%s", q.Get("start_date"), q.Get("end_date"))
		}
		var ids []string
		if err := json.NewDecoder(r.Body).Decode(&ids); err != nil || len(ids) != 2 {
			t.Errorf("posted ids = %v (err %v), want 2 ids", ids, err)
		}
		json.NewEncoder(w).Encode([]mediaEntry{
			{AttachmentID: "a1", CaptureTime: "2024-01-15T10:30:00"},
			{AttachmentID: "a1", CaptureTime: "2024-01-15T10:30:00"}, // duplicate
			{AttachmentID: "", CaptureTime: "2024-01-15T11:00:00"},   // empty id
			{AttachmentID: "a2", CaptureTime: "2024-01-16T09:00:00"},
		})
	}))
	defer ts.Close()

	items, err := NewClient(ts.URL, "tok").GetMediaForDateRange(
		context.Background(), []string{"d1", "d2"}, "2024-01-01", "2024-01-31")
	if err != nil {
		t.Fatalf("GetMediaForDateRange: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2 (dedup + empty skip)", len(items))
	}
	wantTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	if items[0].AttachmentID != "a1" || !items[0].CaptureTime.Equal(wantTime) {
		t.Errorf("item[0] = %+v, want a1 at %v UTC", items[0], wantTime)
	}
}

func TestGetMediaForDateRangeErrors(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		errContains string
	}{
		{
			name: "non-200",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "nope", http.StatusBadRequest)
			},
			errContains: "status 400",
		},
		{
			name: "bad json",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("[broken"))
			},
			errContains: "parsing memories",
		},
		{
			name: "bad capture time",
			handler: func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode([]mediaEntry{{AttachmentID: "a1", CaptureTime: "yesterday"}})
			},
			errContains: "parsing capture_time",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(tt.handler)
			defer ts.Close()

			_, err := NewClient(ts.URL, "tok").GetMediaForDateRange(
				context.Background(), []string{"d1"}, "2024-01-01", "2024-01-31")
			if err == nil || !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("err = %v, want containing %q", err, tt.errContains)
			}
		})
	}
}

func TestDownloadMedia(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/remote/v1/file_attachment" {
			t.Errorf("path = %q, want /remote/v1/file_attachment", r.URL.Path)
		}
		if r.URL.Query().Get("key") != "att1" {
			t.Errorf("key = %q, want att1", r.URL.Query().Get("key"))
		}
		w.Header().Set("Content-Type", "image/jpeg; charset=binary")
		w.Write([]byte("image-bytes"))
	}))
	defer ts.Close()

	data, mediaType, err := NewClient(ts.URL, "tok").DownloadMedia(context.Background(), "att1")
	if err != nil {
		t.Fatalf("DownloadMedia: %v", err)
	}
	if string(data) != "image-bytes" {
		t.Errorf("data = %q", data)
	}
	if mediaType != "image/jpeg" {
		t.Errorf("mediaType = %q, want image/jpeg (parameters stripped)", mediaType)
	}
}

func TestDownloadMediaContentTypeFallback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ";;;garbage")
		w.Write([]byte("x"))
	}))
	defer ts.Close()

	_, mediaType, err := NewClient(ts.URL, "tok").DownloadMedia(context.Background(), "att1")
	if err != nil {
		t.Fatalf("DownloadMedia: %v", err)
	}
	if mediaType != ";;;garbage" {
		t.Errorf("mediaType = %q, want raw header fallback", mediaType)
	}
}

func TestDownloadMediaError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer ts.Close()

	_, _, err := NewClient(ts.URL, "tok").DownloadMedia(context.Background(), "att1")
	if err == nil || !strings.Contains(err.Error(), "status 404") {
		t.Errorf("err = %v, want status 404 error", err)
	}
}

func TestFormatOffset(t *testing.T) {
	tests := []struct {
		seconds int
		want    string
	}{
		{0, "+00:00"},
		{-28800, "-08:00"},
		{19800, "+05:30"},
		{-16200, "-04:30"},
		{3600, "+01:00"},
	}
	for _, tt := range tests {
		if got := FormatOffset(tt.seconds); got != tt.want {
			t.Errorf("FormatOffset(%d) = %q, want %q", tt.seconds, got, tt.want)
		}
	}
}
