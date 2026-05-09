package googlephotos

import "github.com/dipesh/mybrightday-backup/internal/storage"

// Config holds settings for Google Photos storage.
type Config struct {
	storage.BaseConfig `yaml:",inline"`
	// TokenSecret is the JSON-encoded OAuth2 token.
	TokenSecret string `yaml:"token_secret" desc:"Google Photos OAuth token (JSON string)"`
	// ClientSecret is the JSON-encoded Google OAuth2 client secret (optional; uses bundled credentials if omitted).
	ClientSecret string `yaml:"client_secret" desc:"Google Photos Client Secret (JSON string)"`
	// AlbumName is the name of the Google Photos album to upload photos to.
	AlbumName string `yaml:"album_name" desc:"Google Photos album name"`
}
