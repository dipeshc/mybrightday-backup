package app

import (
	"reflect"

	appconfig "github.com/dipesh/mybrightday-backup/internal/config"
	"github.com/dipesh/mybrightday-backup/internal/logging"
	"github.com/dipesh/mybrightday-backup/internal/mybrightday"
	"github.com/dipesh/mybrightday-backup/internal/storage"
	"github.com/dipesh/mybrightday-backup/internal/storage/googlephotos"
	"github.com/dipesh/mybrightday-backup/internal/storage/local"
)

// LocationOverrideConfig holds manual coordinates used instead of geocoding.
type LocationOverrideConfig struct {
	Latitude  float64 `yaml:"latitude" desc:"Manual GPS latitude override"`
	Longitude float64 `yaml:"longitude" desc:"Manual GPS longitude override"`
}

// Config is the root configuration for the download command.
type Config struct {
	Logging          logging.Config          `yaml:"logging"`
	Date             string                  `yaml:"date" desc:"Date or range to fetch photos for (YYYY-MM-DD, -1, +1, -1:+1)"`
	DryRun           bool                    `yaml:"dry_run" desc:"Find and process images without saving or uploading"`
	MyBrightDay      mybrightday.Config      `yaml:"mybrightday"`
	GooglePhotos     googlephotos.Config     `yaml:"google_photos"`
	Local            local.Config            `yaml:"local"`
	LocationOverride *LocationOverrideConfig `yaml:"location_override,omitempty"`
}

// NewDefaultConfig returns a Config with sensible defaults applied.
// Defaults are set before YAML loading so that absent YAML keys retain these values.
func NewDefaultConfig() *Config {
	return &Config{
		Logging: logging.Config{
			Format: "text-simple",
			Level:  "INFO",
		},
		MyBrightDay: mybrightday.Config{
			BaseURL: "https://mybrightday.brighthorizons.com",
		},
		GooglePhotos: googlephotos.Config{
			BaseConfig: storage.BaseConfig{Enabled: false},
			AlbumName:  "Daycare Photos",
		},
		Local: local.Config{
			BaseConfig: storage.BaseConfig{Enabled: true},
			Directory:  "./photos",
		},
	}
}

// Resolve applies hierarchical resolution (flags > env > secrets dir > yaml) to all fields.
func (c *Config) Resolve(flags map[string]string) {
	appconfig.ResolveStruct(reflect.ValueOf(c).Elem(), "", "", flags)

	// Clear a location override that was never actually populated.
	if c.LocationOverride != nil && c.LocationOverride.Latitude == 0 && c.LocationOverride.Longitude == 0 {
		c.LocationOverride = nil
	}
}
