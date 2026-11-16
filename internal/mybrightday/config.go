package mybrightday

// Config holds connection settings for the MyBrightDay API.
type Config struct {
	// RefreshToken is the Auth0 refresh token obtained by `mbdb login`. It is
	// rotated on every run: the token in use is replaced by the one Auth0
	// returns, and the config file is rewritten to match.
	RefreshToken string `yaml:"refresh_token" desc:"MyBrightDay refresh token (set via 'mbdb login')"`
	// BaseURL is the API base URL.
	BaseURL string `yaml:"base_url" desc:"MyBrightDay API base URL"`
}
