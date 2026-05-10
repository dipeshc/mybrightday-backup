package googlephotos

import "github.com/dipesh/mybrightday-backup/internal/storage"

// Config holds settings for Google Photos storage.
type Config struct {
	storage.BaseConfig `yaml:",inline"`
	// RefreshToken is the long-lived OAuth2 refresh token obtained via
	// `mbdb google-photos init`. The access token is fetched at runtime from
	// this refresh token and is never persisted to disk.
	RefreshToken string `yaml:"refresh_token" desc:"Google Photos OAuth refresh token (set via 'mbdb google-photos init')"`
	// ClientSecret is the JSON-encoded Google OAuth2 client secret (optional; uses bundled credentials if omitted).
	ClientSecret string `yaml:"client_secret" desc:"Google Photos Client Secret (JSON string)"`
	// AlbumName is the name of the Google Photos album to upload photos to.
	AlbumName string `yaml:"album_name" desc:"Google Photos album name"`
}
