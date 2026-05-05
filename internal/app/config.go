package app

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ExampleConfigYAML is a template for the config.yaml file.
const ExampleConfigYAML = `email:
  subject_pattern: "Daily Report"
  sender: "dailyreports@daycare.com"
photo:
  timezone_offset: "00:00"
  latitude: 0.0000
  longitude: 0.0000
`

// PhotoConfig holds settings for EXIF metadata injection.
type PhotoConfig struct {
	TimezoneOffset  string  `yaml:"timezone_offset"`
	Latitude        float64 `yaml:"latitude"`
	Longitude       float64 `yaml:"longitude"`
	StartHour       int     `yaml:"start_hour"`
	IntervalMinutes int     `yaml:"interval_minutes"`
}

// EmailConfig holds Gmail search parameters.
type EmailConfig struct {
	SubjectPattern string `yaml:"subject_pattern"`
	Sender         string `yaml:"sender"`
}

// AuthConfig holds OAuth2 file paths.
type AuthConfig struct {
	TokenFile        string `yaml:"token_file"`
	ClientSecretFile string `yaml:"client_secret_file"`
}

// Config is the top-level configuration structure.
type Config struct {
	Photo     PhotoConfig `yaml:"photo"`
	Email     EmailConfig `yaml:"email"`
	Auth      AuthConfig  `yaml:"auth"`
	AlbumName string      `yaml:"album_name"`
}

// LoadConfig reads and parses a YAML configuration file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	cfg.applyDefaults()

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// applyDefaults sets default values for optional configuration fields.
func (c *Config) applyDefaults() {
	if c.AlbumName == "" {
		c.AlbumName = "Daycare Photos"
	}
	if c.Auth.TokenFile == "" {
		c.Auth.TokenFile = "token.json"
	}
	if c.Photo.StartHour == 0 {
		c.Photo.StartHour = 10
	}
	if c.Photo.IntervalMinutes == 0 {
		c.Photo.IntervalMinutes = 5
	}
}

// validate checks that required configuration fields are set.
func (c *Config) validate() error {
	if c.Email.SubjectPattern == "" {
		return errors.New("email.subject_pattern is required")
	}
	if c.Email.Sender == "" {
		return errors.New("email.sender is required")
	}

	return nil
}
