package logging

// Config holds settings for application logging.
type Config struct {
	// Format sets the log output format (text-simple, text-full, or json).
	Format string `yaml:"format" desc:"Log output format (text-simple, text-full, or json)"`
	// Level sets the log level (DEBUG, INFO, WARN, ERROR).
	Level string `yaml:"level" desc:"Log level (DEBUG, INFO, WARN, ERROR)"`
}
