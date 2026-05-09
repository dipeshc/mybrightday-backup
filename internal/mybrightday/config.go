package mybrightday

// Config holds connection settings for the MyBrightDay API.
type Config struct {
	// Email is the user's MyBrightDay email address.
	Email string `yaml:"email" desc:"MyBrightDay email"`
	// Password is the user's MyBrightDay password.
	Password string `yaml:"password" desc:"MyBrightDay password"`
	// BaseURL is the API base URL.
	BaseURL string `yaml:"base_url" desc:"MyBrightDay API base URL"`
}
