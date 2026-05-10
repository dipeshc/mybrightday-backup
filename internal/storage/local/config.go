package local

import "github.com/dipeshc/mybrightday-backup/internal/storage"

// Config holds settings for local filesystem storage.
type Config struct {
	storage.BaseConfig `yaml:",inline"`
	// Directory is the root directory where photos will be saved.
	Directory string `yaml:"directory" desc:"Directory where photos will be saved"`
}
