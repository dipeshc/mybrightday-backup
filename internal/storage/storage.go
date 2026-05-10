package storage

import (
	"context"
	"time"
)

// BaseConfig is the common base for all storage backend configurations.
type BaseConfig struct {
	// Enabled controls whether this storage backend is active.
	Enabled bool `yaml:"enabled" desc:"Enable this storage destination"`
}

// Photo is a fully processed photo ready for storage.
// CaptureTime must be in the center's local timezone so that storage backends
// can use it directly for date-based directory naming and filtering.
type Photo struct {
	// AttachmentID is the MyBrightDay attachment identifier, used for deduplication.
	AttachmentID string
	// Filename is the canonical file name, e.g. "daycare_2024-12-20_<id>.jpg".
	Filename string
	// Data is the processed JPEG bytes with EXIF metadata already injected.
	Data []byte
	// CaptureTime is the photo's capture time in the daycare center's local timezone.
	CaptureTime time.Time
}

// Storage is the interface implemented by all storage backends.
// Save is called once per photo; each backend is responsible for its own
// deduplication and dry-run behaviour. One-time per-run setup (e.g creating
// remote resources, ensuring a local directory exists, etc) is performed by
// each backend's constructor — backends are fully ready to use the moment New
// returns.
type Storage interface {
	Save(ctx context.Context, photo Photo) error
}
