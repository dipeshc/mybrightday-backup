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

// Photo is a fully processed media item (photo or video) ready for storage.
// CaptureTime must be in the center's local timezone so that storage backends
// can use it directly for date-based directory naming and filtering.
type Photo struct {
	// AttachmentID is the MyBrightDay attachment identifier, used for deduplication.
	AttachmentID string
	// Filename is the canonical file name, e.g. "daycare_2024-12-20_<id>.jpg"
	// or "daycare_2024-12-20_<id>.mp4".
	Filename string
	// Data is the processed media bytes with capture-time and GPS metadata
	// already injected (EXIF for photos, mvhd/©xyz for MP4 videos).
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
	// Name returns the identifier for this storage backend (e.g. "local").
	Name() string
	// Save is called once per photo; each backend is responsible for its own
	// deduplication and dry-run behaviour. It returns true if the photo was
	// newly saved/uploaded, and false if it was skipped (already exists).
	// One-time per-run setup (e.g creating remote resources, ensuring a local
	// directory exists, etc) is performed by each backend's constructor —
	// backends are fully ready to use the moment New returns.
	Save(ctx context.Context, photo Photo) (bool, error)
}
