package app

import (
	"testing"
)

func TestNewDefaultConfig(t *testing.T) {
	cfg := NewDefaultConfig()

	if cfg.MyBrightDay.BaseURL != "https://mybrightday.brighthorizons.com" {
		t.Errorf("BaseURL = %q", cfg.MyBrightDay.BaseURL)
	}
	if cfg.Logging.Format != "text-simple" || cfg.Logging.Level != "INFO" {
		t.Errorf("Logging = %+v", cfg.Logging)
	}
	if cfg.GooglePhotos.Enabled {
		t.Error("GooglePhotos enabled by default, want disabled")
	}
	if cfg.GooglePhotos.AlbumName != "Daycare Photos" {
		t.Errorf("AlbumName = %q", cfg.GooglePhotos.AlbumName)
	}
	if !cfg.Local.Enabled || cfg.Local.Directory != "./photos" {
		t.Errorf("Local = %+v", cfg.Local)
	}
	if cfg.LocationOverride != nil {
		t.Error("LocationOverride set by default, want nil")
	}
}

func TestResolveClearsEmptyLocationOverride(t *testing.T) {
	t.Setenv("CONFIG_FILES_DIR", t.TempDir())

	cfg := NewDefaultConfig()
	cfg.Resolve(nil)
	if cfg.LocationOverride != nil {
		t.Errorf("LocationOverride = %+v, want nil when never populated", cfg.LocationOverride)
	}
}

func TestResolveKeepsPopulatedLocationOverride(t *testing.T) {
	t.Setenv("CONFIG_FILES_DIR", t.TempDir())

	cfg := NewDefaultConfig()
	cfg.LocationOverride = &LocationOverrideConfig{Latitude: 37.42, Longitude: -122.08}
	cfg.Resolve(nil)
	if cfg.LocationOverride == nil || cfg.LocationOverride.Latitude != 37.42 {
		t.Errorf("LocationOverride = %+v, want preserved", cfg.LocationOverride)
	}
}

func TestResolveAppliesEnvOverrides(t *testing.T) {
	t.Setenv("CONFIG_FILES_DIR", t.TempDir())
	t.Setenv("MYBRIGHTDAY_EMAIL", "env@example.com")
	t.Setenv("LOCATION_OVERRIDE_LATITUDE", "40.5")
	t.Setenv("LOCATION_OVERRIDE_LONGITUDE", "-74.25")

	cfg := NewDefaultConfig()
	cfg.Resolve(nil)
	if cfg.MyBrightDay.Email != "env@example.com" {
		t.Errorf("Email = %q, want env value", cfg.MyBrightDay.Email)
	}
	if cfg.LocationOverride == nil || cfg.LocationOverride.Latitude != 40.5 || cfg.LocationOverride.Longitude != -74.25 {
		t.Errorf("LocationOverride = %+v, want populated from env", cfg.LocationOverride)
	}
}
