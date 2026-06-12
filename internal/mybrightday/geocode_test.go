package mybrightday

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// pointNominatimAt redirects geocoding at a fake server and clears the
// package-level cache afterwards. Tests using it must not run in parallel.
func pointNominatimAt(t *testing.T, url string) {
	t.Helper()
	original := nominatimSearchURL
	nominatimSearchURL = url
	t.Cleanup(func() {
		nominatimSearchURL = original
		geocodeCacheMu.Lock()
		geocodeCache = make(map[string]struct{ lat, lon float64 })
		geocodeCacheMu.Unlock()
	})
}

func testCenter(name string) *Center {
	c := &Center{ID: "c1", Name: name, Timezone: "America/Los_Angeles"}
	c.Address.AddressLine1 = "1 Main St"
	c.Address.City = "Springfield"
	c.Address.State = "CA"
	c.Address.PostalCode = "90001"
	return c
}

func TestGeocodeSuccessAndCache(t *testing.T) {
	requests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if got := r.URL.Query().Get("format"); got != "json" {
			t.Errorf("format = %q, want json", got)
		}
		json.NewEncoder(w).Encode([]GeocodeResult{{Lat: "34.05", Lon: "-118.24"}})
	}))
	defer ts.Close()
	pointNominatimAt(t, ts.URL)

	c := NewClient("http://unused.test", "tok")
	lat, lon, err := c.geocode(context.Background(), "unique-query-cache-test")
	if err != nil {
		t.Fatalf("geocode: %v", err)
	}
	if lat != 34.05 || lon != -118.24 {
		t.Errorf("coords = %f,%f, want 34.05,-118.24", lat, lon)
	}

	// Second call must come from the cache.
	lat2, lon2, err := c.geocode(context.Background(), "unique-query-cache-test")
	if err != nil {
		t.Fatalf("cached geocode: %v", err)
	}
	if lat2 != lat || lon2 != lon {
		t.Errorf("cached coords = %f,%f, want %f,%f", lat2, lon2, lat, lon)
	}
	if requests != 1 {
		t.Errorf("requests = %d, want 1 (second call cached)", requests)
	}
}

func TestGeocodeErrors(t *testing.T) {
	t.Run("no results", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("[]"))
		}))
		defer ts.Close()
		pointNominatimAt(t, ts.URL)

		_, _, err := NewClient("http://unused.test", "tok").geocode(context.Background(), "unique-query-empty")
		if err == nil || !strings.Contains(err.Error(), "no geocode results") {
			t.Errorf("err = %v, want no geocode results", err)
		}
	})

	t.Run("bad json", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("{not an array"))
		}))
		defer ts.Close()
		pointNominatimAt(t, ts.URL)

		_, _, err := NewClient("http://unused.test", "tok").geocode(context.Background(), "unique-query-badjson")
		if err == nil || !strings.Contains(err.Error(), "parsing geocode response") {
			t.Errorf("err = %v, want parsing geocode response", err)
		}
	})
}

func TestGeocodeCenterAddressFirstAttempt(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if !strings.HasPrefix(q, "1 Main St") {
			t.Errorf("q = %q, want street-address query", q)
		}
		json.NewEncoder(w).Encode([]GeocodeResult{{Lat: "34.0", Lon: "-118.0"}})
	}))
	defer ts.Close()
	pointNominatimAt(t, ts.URL)

	lat, lon, err := NewClient("http://unused.test", "tok").
		GeocodeCenterAddress(context.Background(), testCenter("Center A"))
	if err != nil {
		t.Fatalf("GeocodeCenterAddress: %v", err)
	}
	if lat != 34.0 || lon != -118.0 {
		t.Errorf("coords = %f,%f", lat, lon)
	}
}

func TestGeocodeCenterAddressFallsBackToName(t *testing.T) {
	var queries []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		queries = append(queries, q)
		if strings.HasPrefix(q, "1 Main St") {
			w.Write([]byte("[]"))
			return
		}
		json.NewEncoder(w).Encode([]GeocodeResult{{Lat: "35.0", Lon: "-119.0"}})
	}))
	defer ts.Close()
	pointNominatimAt(t, ts.URL)

	lat, lon, err := NewClient("http://unused.test", "tok").
		GeocodeCenterAddress(context.Background(), testCenter("Center B Fallback"))
	if err != nil {
		t.Fatalf("GeocodeCenterAddress: %v", err)
	}
	if lat != 35.0 || lon != -119.0 {
		t.Errorf("coords = %f,%f", lat, lon)
	}
	if len(queries) != 2 || !strings.HasPrefix(queries[1], "Center B Fallback") {
		t.Errorf("queries = %v, want address then center-name fallback", queries)
	}
}

func TestGeocodeCenterAddressBothFail(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("[]"))
	}))
	defer ts.Close()
	pointNominatimAt(t, ts.URL)

	_, _, err := NewClient("http://unused.test", "tok").
		GeocodeCenterAddress(context.Background(), testCenter(fmt.Sprintf("Center C %p", t)))
	if err == nil || !strings.Contains(err.Error(), "no geocode results") {
		t.Errorf("err = %v, want no geocode results", err)
	}
}
