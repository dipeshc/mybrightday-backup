package googlephotos

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/dipesh/mybrightday-backup/internal/storage"
)

// GooglePhotosStorage uploads photos to a Google Photos album.
// It pre-fetches already-uploaded attachment IDs on construction to avoid duplicates.
type GooglePhotosStorage struct {
	client      *http.Client
	albumID     string
	uploadedIDs map[string]bool
	dryRun      bool
}

// New creates a GooglePhotosStorage instance for the given date range.
// startDate and endDate are used to pre-fetch the deduplication set from Google Photos.
func New(ctx context.Context, cfg Config, dryRun bool, startDate, endDate time.Time) (*GooglePhotosStorage, error) {
	client, err := getOAuthClient(ctx, cfg)
	if err != nil {
		return nil, err
	}

	var albumID string
	if dryRun {
		albumID, err = findAlbumByTitle(ctx, client, cfg.AlbumName)
		if err != nil {
			return nil, fmt.Errorf("searching for album: %w", err)
		}
		if albumID == "" {
			slog.Info("[DRY RUN] Would create album", "name", cfg.AlbumName)
		} else {
			slog.Debug("Found existing album", "name", cfg.AlbumName, "id", albumID)
		}
	} else {
		albumID, err = findOrCreateAlbum(ctx, client, cfg.AlbumName)
		if err != nil {
			return nil, fmt.Errorf("resolving album: %w", err)
		}
		slog.Debug("Using album", "name", cfg.AlbumName, "id", albumID)
	}

	uploadedIDs, err := listUploadedAttachmentIDs(ctx, client, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("listing uploaded photos: %w", err)
	}
	slog.Debug("Uploaded photos in date window", "count", len(uploadedIDs))

	return &GooglePhotosStorage{
		client:      client,
		albumID:     albumID,
		uploadedIDs: uploadedIDs,
		dryRun:      dryRun,
	}, nil
}

// Save uploads the photo to Google Photos, skipping it if already uploaded.
// In dry-run mode the upload is logged but not performed.
func (s *GooglePhotosStorage) Save(ctx context.Context, photo storage.Photo) error {
	if s.uploadedIDs[photo.AttachmentID] {
		slog.Debug("Skipping already-uploaded photo", "id", photo.AttachmentID)
		return nil
	}

	if s.dryRun {
		slog.Info("[DRY RUN] Would upload photo to Google Photos",
			"filename", photo.Filename,
			"size", len(photo.Data),
			"time", photo.CaptureTime.Format("2006-01-02 15:04:05 -07:00"))
		return nil
	}

	mediaID, err := uploadToPhotos(ctx, s.client, photo.Data, photo.Filename, s.albumID)
	if err != nil {
		return fmt.Errorf("uploading %s: %w", photo.Filename, err)
	}

	// Track the upload so subsequent calls for the same attachment skip.
	s.uploadedIDs[photo.AttachmentID] = true
	slog.Debug("Uploaded photo", "filename", photo.Filename, "mediaID", mediaID)
	return nil
}
