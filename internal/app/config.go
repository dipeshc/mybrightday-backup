package app

import (
	"fmt"
	"os"
	"reflect"

	"github.com/dipesh/daycare-photos/pkg/config"
	"gopkg.in/yaml.v3"
)

// LoggingConfig holds common configuration properties for all commands.
type LoggingConfig struct {
	// Format sets the log output format (text or json).
	Format string `yaml:"format" desc:"Log output format (text or json)"`
	// Verbose enables detailed logging.
	Verbose bool `yaml:"verbose" desc:"Enable verbose logging"`
}

// MyBrightDayConfig holds connection settings for the MyBrightDay API.
type MyBrightDayConfig struct {
	// SessionCookieSecret is the authenticated session cookie.
	SessionCookieSecret string `yaml:"session_cookie_secret" desc:"MyBrightDay session cookie"`
	// BaseURL is the API base URL (default: https://mybrightday.brighthorizons.com).
	BaseURL string `yaml:"base_url" desc:"MyBrightDay API base URL"`
}

// GooglePhotosConfig holds settings for Google Photos integration.
type GooglePhotosConfig struct {
	// Enabled enables uploading to Google Photos.
	Enabled bool `yaml:"enabled" desc:"Enable Google Photos upload"`
	// TokenSecret is the JSON-encoded OAuth2 token.
	TokenSecret string `yaml:"token_secret" desc:"Google Photos OAuth token (JSON string)"`
	// ClientSecret is the JSON-encoded Google OAuth2 client secret.
	ClientSecret string `yaml:"client_secret" desc:"Google Photos Client Secret (JSON string)"`
	// AlbumName is the name of the album to upload photos to.
	AlbumName string `yaml:"album_name" desc:"Google Photos album name"`
}

// LocalConfig holds settings for local photo storage.
type LocalConfig struct {
	// Enabled enables saving photos locally.
	Enabled bool `yaml:"enabled" desc:"Enable saving photos locally"`
	// Directory is the root directory where photos will be saved.
	Directory string `yaml:"directory" desc:"Directory where photos will be saved"`
}

// LocationOverrideConfig holds manual coordinates for EXIF metadata.
type LocationOverrideConfig struct {
	Latitude  float64 `yaml:"latitude" desc:"Manual GPS latitude override"`
	Longitude float64 `yaml:"longitude" desc:"Manual GPS longitude override"`
}

// RunConfig is the configuration structure for the 'run' command.
type RunConfig struct {
	Logging          LoggingConfig           `yaml:"logging"`
	Date             string                  `yaml:"date" desc:"Date or range to fetch photos for (YYYY-MM-DD, -1, +1, -1:+1)"`
	DryRun           bool                    `yaml:"dry_run" desc:"Find and process images without saving or uploading"`
	MyBrightDay      MyBrightDayConfig       `yaml:"mybrightday"`
	GooglePhotos     GooglePhotosConfig      `yaml:"google_photos"`
	Local            LocalConfig             `yaml:"local"`
	LocationOverride *LocationOverrideConfig `yaml:"location_override,omitempty"`
}

// InitConfig is the configuration structure for the 'init' command.
type InitConfig struct {
	Logging      LoggingConfig      `yaml:"logging"`
	MyBrightDay  MyBrightDayConfig  `yaml:"mybrightday"`
	GooglePhotos GooglePhotosConfig `yaml:"google_photos"`
}

// VersionConfig is the configuration structure for the 'version' command.
type VersionConfig struct {
	Logging LoggingConfig `yaml:"logging"`
}

// Resolve applies hierarchical resolution to all configuration fields based on struct tags.
func (c *RunConfig) Resolve(flags map[string]string) {
	config.ResolveStruct(reflect.ValueOf(c).Elem(), "", flags)

	if c.LocationOverride != nil && c.LocationOverride.Latitude == 0 && c.LocationOverride.Longitude == 0 {
		c.LocationOverride = nil
	}

	c.ApplyDefaults()
}

// Resolve applies hierarchical resolution to all configuration fields based on struct tags.
func (c *InitConfig) Resolve(flags map[string]string) {
	config.ResolveStruct(reflect.ValueOf(c).Elem(), "", flags)
	c.ApplyDefaults()
}

// Resolve applies hierarchical resolution to all configuration fields based on struct tags.
func (c *VersionConfig) Resolve(flags map[string]string) {
	config.ResolveStruct(reflect.ValueOf(c).Elem(), "", flags)
}

// LoadConfig reads and parses a YAML configuration file into the target struct.
// If the file does not exist, it does nothing (allowing defaults and other sources to take over).
func LoadConfig(path string, target interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading config file: %w", err)
	}

	if err := yaml.Unmarshal(data, target); err != nil {
		return fmt.Errorf("parsing config file: %w", err)
	}

	return nil
}

// NewDefaultRunConfig creates a RunConfig instance with default values populated.
func NewDefaultRunConfig() *RunConfig {
	cfg := &RunConfig{}
	cfg.ApplyDefaults()
	return cfg
}

// ApplyDefaults sets default values for optional configuration fields in RunConfig.
func (c *RunConfig) ApplyDefaults() {
	if c.Logging.Format == "" {
		c.Logging.Format = "text"
	}
	if c.GooglePhotos.AlbumName == "" {
		c.GooglePhotos.AlbumName = "Daycare Photos"
	}
	if c.Local.Directory == "" {
		c.Local.Directory = "./photos/"
	}
	// Default Local.Enabled to true if not already set.
	// Since we are applying defaults AFTER resolving, we should be careful.
	// However, the current hierarchical logic sets booleans strictly.
	// Let's assume if it is false and it wasn't in config or flags, we set it true.
	// This is hard to detect without more complexity.
	// For now, let's keep the user's requirement.
	if !c.Local.Enabled && c.Local.Directory == "./photos/" {
		c.Local.Enabled = true
	}
	if c.MyBrightDay.BaseURL == "" {
		c.MyBrightDay.BaseURL = "https://mybrightday.brighthorizons.com"
	}
}

// ApplyDefaults sets default values for optional configuration fields in InitConfig.
func (c *InitConfig) ApplyDefaults() {
	if c.Logging.Format == "" {
		c.Logging.Format = "text"
	}
	if c.MyBrightDay.BaseURL == "" {
		c.MyBrightDay.BaseURL = "https://mybrightday.brighthorizons.com"
	}
}

// ApplyDefaults sets default values for optional configuration fields in VersionConfig.
func (c *VersionConfig) ApplyDefaults() {
	if c.Logging.Format == "" {
		c.Logging.Format = "text"
	}
}
