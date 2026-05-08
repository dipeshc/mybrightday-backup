package app

import (
	"fmt"
	"os"
	"reflect"

	"github.com/dipesh/mybrightday-backup/pkg/config"
	"gopkg.in/yaml.v3"
)

// LoggingConfig holds common configuration properties for all commands.
type LoggingConfig struct {
	// Format sets the log output format (text-simple, text-full, or json).
	Format string `yaml:"format" desc:"Log output format (text-simple, text-full, or json)"`
	// Level sets the log level (DEBUG, INFO, WARN, ERROR).
	Level string `yaml:"level" desc:"Log level (DEBUG, INFO, WARN, ERROR)"`
}

// MyBrightDayConfig holds connection settings for the MyBrightDay API.
type MyBrightDayConfig struct {
	// Email is the user's email for authentication.
	Email string `yaml:"email" desc:"MyBrightDay email"`
	// Password is the user's password for authentication.
	Password string `yaml:"password" desc:"MyBrightDay password"`
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
	Enabled *bool `yaml:"enabled" desc:"Enable saving photos locally"`
	// Directory is the root directory where photos will be saved.
	Directory string `yaml:"directory" desc:"Directory where photos will be saved"`
}

// LocationOverrideConfig holds manual coordinates for EXIF metadata.
type LocationOverrideConfig struct {
	Latitude  float64 `yaml:"latitude" desc:"Manual GPS latitude override"`
	Longitude float64 `yaml:"longitude" desc:"Manual GPS longitude override"`
}

// DownloadConfig is the configuration structure for the 'download' command.
type DownloadConfig struct {
	Logging          LoggingConfig           `yaml:"logging"`
	Date             string                  `yaml:"date" desc:"Date or range to fetch photos for (YYYY-MM-DD, -1, +1, -1:+1)"`
	DryRun           bool                    `yaml:"dry_run" desc:"Find and process images without saving or uploading"`
	MyBrightDay      MyBrightDayConfig       `yaml:"mybrightday"`
	GooglePhotos     GooglePhotosConfig      `yaml:"google_photos"`
	Local            LocalConfig             `yaml:"local"`
	LocationOverride *LocationOverrideConfig `yaml:"location_override,omitempty"`
}

// GooglePhotosInitConfig is the configuration structure for the 'google-photos-init' command.
type GooglePhotosInitConfig struct {
	Logging      LoggingConfig      `yaml:"logging"`
	GooglePhotos GooglePhotosConfig `yaml:"google_photos"`
}

// VersionConfig is the configuration structure for the 'version' command.
type VersionConfig struct {
	Logging LoggingConfig `yaml:"logging"`
}

// Resolve applies hierarchical resolution to all configuration fields based on struct tags.
func (c *DownloadConfig) Resolve(flags map[string]string) {
	config.ResolveStruct(reflect.ValueOf(c).Elem(), "", "", flags)

	if c.LocationOverride != nil && c.LocationOverride.Latitude == 0 && c.LocationOverride.Longitude == 0 {
		c.LocationOverride = nil
	}

	c.ApplyDefaults()
}

// Resolve applies hierarchical resolution to all configuration fields based on struct tags.
func (c *GooglePhotosInitConfig) Resolve(flags map[string]string) {
	config.ResolveStruct(reflect.ValueOf(c).Elem(), "", "", flags)
	c.ApplyDefaults()
}

// Resolve applies hierarchical resolution to all configuration fields based on struct tags.
func (c *VersionConfig) Resolve(flags map[string]string) {
	config.ResolveStruct(reflect.ValueOf(c).Elem(), "", "", flags)
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

// NewDefaultDownloadConfig creates a DownloadConfig instance with default values populated.
func NewDefaultDownloadConfig() *DownloadConfig {
	cfg := &DownloadConfig{}
	cfg.ApplyDefaults()
	return cfg
}

// NewDefaultGooglePhotosInitConfig creates a GooglePhotosInitConfig instance with default values populated.
func NewDefaultGooglePhotosInitConfig() *GooglePhotosInitConfig {
	cfg := &GooglePhotosInitConfig{}
	cfg.ApplyDefaults()
	return cfg
}

// ApplyDefaults sets default values for optional configuration fields in DownloadConfig.
func (c *DownloadConfig) ApplyDefaults() {
	if c.Logging.Format == "" {
		c.Logging.Format = "text-simple"
	}
	if c.Logging.Level == "" {
		c.Logging.Level = "INFO"
	}
	if c.GooglePhotos.AlbumName == "" {
		c.GooglePhotos.AlbumName = "Daycare Photos"
	}
	if c.Local.Directory == "" {
		c.Local.Directory = "./photos"
	}
	// Default Local.Enabled to true if not explicitly set.
	if c.Local.Enabled == nil {
		enabled := true
		c.Local.Enabled = &enabled
	}
	if c.MyBrightDay.BaseURL == "" {
		c.MyBrightDay.BaseURL = "https://mybrightday.brighthorizons.com"
	}
}

// ApplyDefaults sets default values for optional configuration fields in GooglePhotosInitConfig.
func (c *GooglePhotosInitConfig) ApplyDefaults() {
	if c.Logging.Format == "" {
		c.Logging.Format = "text-simple"
	}
	if c.Logging.Level == "" {
		c.Logging.Level = "INFO"
	}
}

// ApplyDefaults sets default values for optional configuration fields in VersionConfig.
func (c *VersionConfig) ApplyDefaults() {
	if c.Logging.Format == "" {
		c.Logging.Format = "text-simple"
	}
	if c.Logging.Level == "" {
		c.Logging.Level = "INFO"
	}
}
