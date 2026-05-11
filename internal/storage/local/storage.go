package local

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/dipeshc/mybrightday-backup/internal/storage"
)

// LocalStorage saves photos to the local filesystem under a date-based directory hierarchy.
type LocalStorage struct {
	cfg    Config
	dryRun bool
}

// New creates a LocalStorage and ensures the configured root directory exists.
// Per-date subdirectories are created on demand inside Save, since the set of
// dates is not known until media discovery completes. In dry-run mode no
// directory is created.
func New(cfg Config, dryRun bool) (*LocalStorage, error) {
	if !dryRun {
		if err := os.MkdirAll(cfg.Directory, 0755); err != nil {
			return nil, fmt.Errorf("creating local storage directory %q: %w", cfg.Directory, err)
		}
	}
	return &LocalStorage{cfg: cfg, dryRun: dryRun}, nil
}

// Name returns "local".
func (s *LocalStorage) Name() string {
	return "local"
}

// Save writes the photo to {Directory}/{YYYY-MM-DD}/{filename}.
// If the file already exists it is skipped. In dry-run mode the write is logged but not performed.
func (s *LocalStorage) Save(_ context.Context, photo storage.Photo) (bool, error) {
	captureDate := photo.CaptureTime.Format("2006-01-02")
	dir := filepath.Join(s.cfg.Directory, captureDate)
	path := filepath.Join(dir, photo.Filename)

	if s.dryRun {
		slog.Info("[DRY RUN] Would save photo locally", "path", path)
		return false, nil
	}

	if _, err := os.Stat(path); err == nil {
		slog.Debug("Skipping already-saved photo", "path", path)
		return false, nil
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return false, fmt.Errorf("creating output directory: %w", err)
	}

	if err := os.WriteFile(path, photo.Data, 0644); err != nil {
		return false, fmt.Errorf("writing photo: %w", err)
	}

	slog.Debug("Saved photo locally", "path", path)
	return true, nil
}
