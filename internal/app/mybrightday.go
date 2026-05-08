package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	mediaDownloadURL  = "https://mybrightday.brighthorizons.com/remote/v1/file_attachment"
	captureTimeLayout = "2006-01-02T15:04:05"
)

// MyBrightDayClient makes authenticated requests to the MyBrightDay API.
type MyBrightDayClient struct {
	httpClient *http.Client
	baseURL    string
	cookie     string
}

// MediaItem is a single photo attachment returned by the media feed.
type MediaItem struct {
	AttachmentID string
	CaptureTime  time.Time // UTC
}

// guardianProfile is the JSON shape returned by GET /user/profile.
type guardianProfile struct {
	ID string `json:"id"`
}

// dependent is the JSON shape of a single entry from GET /dependents/guardian/{id}.
type dependent struct {
	ID     string `json:"id"`
	Center string `json:"center"`
}

// Center represents information about a daycare center.
type Center struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Timezone string `json:"timezone"`
	Address  struct {
		AddressLine1 string `json:"address_line_1"`
		City         string `json:"city"`
		State        string `json:"state"`
		PostalCode   string `json:"postal_code"`
	} `json:"address"`
}

var geocodeCache = make(map[string]struct{ lat, lon float64 })

// GeocodeResult is the JSON shape returned by Nominatim.
type GeocodeResult struct {
	Lat string `json:"lat"`
	Lon string `json:"lon"`
}

// mediaEntry is the JSON shape of a single item in the POST /dependent/memories/media response.
type mediaEntry struct {
	AttachmentID string `json:"attachment_id"`
	CaptureTime  string `json:"capture_time"`
	EntryType    string `json:"entry_type"`
}

// NewMyBrightDayClient creates an authenticated MyBrightDay API client.
func NewMyBrightDayClient(baseURL, cookie string) *MyBrightDayClient {
	if cookie != "" && !strings.HasPrefix(cookie, "session=") {
		cookie = "session=" + cookie
	}
	return &MyBrightDayClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    baseURL,
		cookie:     cookie,
	}
}

// GetCenterInfo fetches center information from the MyBrightDay API.
func (c *MyBrightDayClient) GetCenterInfo(ctx context.Context, centerID string) (*Center, error) {
	url := fmt.Sprintf("%s/api/v2/center/%s", c.baseURL, centerID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating center request: %w", err)
	}
	req.Header.Set("Cookie", c.cookie)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching center: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading center response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("center request failed with status %d: %s", resp.StatusCode, string(data))
	}

	var center Center
	if err := json.Unmarshal(data, &center); err != nil {
		return nil, fmt.Errorf("parsing center: %w", err)
	}

	return &center, nil
}

// GeocodeCenterAddress converts a center's address to latitude and longitude using Nominatim.
func (c *MyBrightDayClient) GeocodeCenterAddress(ctx context.Context, center *Center) (float64, float64, error) {
	addr := center.Address

	// Attempt 1: Using the street address
	queryAddr := fmt.Sprintf("%s, %s, %s %s, USA",
		addr.AddressLine1, addr.City, addr.State, addr.PostalCode)
	lat, lon, err := c.geocode(ctx, queryAddr)
	if err == nil {
		return lat, lon, nil
	}

	// Attempt 2: Fallback to using the center name
	queryName := fmt.Sprintf("%s, %s, %s %s, USA",
		center.Name, addr.City, addr.State, addr.PostalCode)
	slog.Debug("Geocoding address failed, falling back to center name",
		"address", queryAddr,
		"fallback", queryName,
		"error", err)
	return c.geocode(ctx, queryName)
}

func (c *MyBrightDayClient) geocode(ctx context.Context, query string) (float64, float64, error) {
	if res, ok := geocodeCache[query]; ok {
		return res.lat, res.lon, nil
	}

	v := url.Values{}
	v.Set("format", "json")
	v.Set("limit", "1")
	v.Set("q", query)

	searchURL := "https://nominatim.openstreetmap.org/search?" + v.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("creating geocode request: %w", err)
	}
	req.Header.Set("User-Agent", "daycare-photos/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("geocoding address: %w", err)
	}
	defer resp.Body.Close()

	var results []GeocodeResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return 0, 0, fmt.Errorf("parsing geocode response: %w", err)
	}

	if len(results) == 0 {
		return 0, 0, fmt.Errorf("no geocode results for: %s", query)
	}

	var lat, lon float64
	fmt.Sscanf(results[0].Lat, "%f", &lat)
	fmt.Sscanf(results[0].Lon, "%f", &lon)

	geocodeCache[query] = struct{ lat, lon float64 }{lat, lon}
	return lat, lon, nil
}

// FormatOffset converts seconds east of UTC to a string like "-07:00".
func FormatOffset(seconds int) string {
	sign := "+"
	if seconds < 0 {
		sign = "-"
		seconds = -seconds
	}
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	return fmt.Sprintf("%s%02d:%02d", sign, hours, minutes)
}

// GetDependentIDs returns the IDs of all dependents for the authenticated guardian and their associated center IDs.
func (c *MyBrightDayClient) GetDependentIDs(ctx context.Context) ([]string, map[string]string, error) {
	profile, err := c.getProfile(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("fetching profile: %w", err)
	}

	deps, err := c.getDependents(ctx, profile.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("fetching dependents: %w", err)
	}

	ids := make([]string, len(deps))
	centerIDs := make(map[string]string)
	for i, d := range deps {
		ids[i] = d.ID
		centerIDs[d.ID] = d.Center
	}
	return ids, centerIDs, nil
}

// GetMediaForDateRange returns all photo attachments for the given dependents on the given date range.
// dates must be in YYYY-MM-DD format. capture_time values are in UTC.
func (c *MyBrightDayClient) GetMediaForDateRange(ctx context.Context, dependentIDs []string, startDate, endDate string) ([]MediaItem, error) {
	body, err := json.Marshal(dependentIDs)
	if err != nil {
		return nil, fmt.Errorf("marshaling dependent IDs: %w", err)
	}

	url := fmt.Sprintf("%s/api/v2/dependent/memories/media?start_date=%s&end_date=%s",
		c.baseURL, startDate, endDate)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating memories request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", c.cookie)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching memories: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading memories response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("memories request failed with status %d: %s", resp.StatusCode, string(data))
	}

	var entries []mediaEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parsing memories response: %w", err)
	}

	seen := make(map[string]bool)
	var items []MediaItem

	for _, e := range entries {
		if e.AttachmentID == "" || seen[e.AttachmentID] {
			continue
		}
		seen[e.AttachmentID] = true

		ct, err := time.ParseInLocation(captureTimeLayout, e.CaptureTime, time.UTC)
		if err != nil {
			return nil, fmt.Errorf("parsing capture_time %q: %w", e.CaptureTime, err)
		}

		items = append(items, MediaItem{
			AttachmentID: e.AttachmentID,
			CaptureTime:  ct,
		})
	}

	return items, nil
}

// DownloadMedia downloads the image bytes for the given attachment ID.
func (c *MyBrightDayClient) DownloadMedia(ctx context.Context, attachmentID string) ([]byte, error) {
	url := fmt.Sprintf("%s?key=%s", mediaDownloadURL, attachmentID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating download request: %w", err)
	}
	req.Header.Set("Cookie", c.cookie)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading media: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("media download returned status %d for attachment %s", resp.StatusCode, attachmentID)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading media data: %w", err)
	}

	return data, nil
}

// getProfile fetches the authenticated user's guardian profile.
func (c *MyBrightDayClient) getProfile(ctx context.Context) (*guardianProfile, error) {
	url := c.baseURL + "/api/v2/user/profile"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating profile request: %w", err)
	}
	req.Header.Set("Cookie", c.cookie)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching profile: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading profile response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("profile request failed with status %d: %s", resp.StatusCode, string(data))
	}

	var profile guardianProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		return nil, fmt.Errorf("parsing profile: %w", err)
	}

	return &profile, nil
}

// getDependents returns all dependents for the given guardian ID.
func (c *MyBrightDayClient) getDependents(ctx context.Context, guardianID string) ([]dependent, error) {
	url := fmt.Sprintf("%s/api/v2/dependents/guardian/%s", c.baseURL, guardianID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating dependents request: %w", err)
	}
	req.Header.Set("Cookie", c.cookie)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching dependents: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading dependents response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dependents request failed with status %d: %s", resp.StatusCode, string(data))
	}

	var deps []dependent
	if err := json.Unmarshal(data, &deps); err != nil {
		return nil, fmt.Errorf("parsing dependents: %w", err)
	}

	return deps, nil
}
