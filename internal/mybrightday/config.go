package mybrightday

// Config holds connection settings for the MyBrightDay API.
type Config struct {
	// Session is the MyBrightDay session cookie obtained by logging in
	// through the browser. It is valid for about a year and does not change,
	// so it is stored as-is and used directly on every run.
	Session string `yaml:"session" desc:"MyBrightDay session cookie (obtained by logging in; see README)"`
	// BaseURL is the API base URL.
	BaseURL string `yaml:"base_url" desc:"MyBrightDay API base URL"`
}
