package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// parseDateString parses an absolute (YYYY-MM-DD) or relative (-1, +2, 0) date string.
func parseDateString(d string) (string, error) {
	if d == "" {
		return time.Now().Format("2006-01-02"), nil
	}

	if days, err := strconv.Atoi(d); err == nil {
		return time.Now().AddDate(0, 0, days).Format("2006-01-02"), nil
	}

	_, err := time.Parse("2006-01-02", d)
	if err != nil {
		return "", fmt.Errorf("invalid date format: %q (expected YYYY-MM-DD or relative like -1)", d)
	}

	return d, nil
}

// RunProcess fetches photos from the MyBrightDay API for the given date (or range) and saves them locally.
// If googlePhotos is true, it also uploads them to Google Photos.
// If date is empty it defaults to today. date can be a single date (YYYY-MM-DD) or a range (YYYY-MM-DD:YYYY-MM-DD).
func RunProcess(ctx context.Context, cfg *RunConfig) error {
	if cfg.MyBrightDay.SessionCookieSecret == "" {
		return errors.New("mybrightday_session_cookie_secret is required — set it via flag, env var, file, or config.yaml")
	}

	cookie := cfg.MyBrightDay.SessionCookieSecret

	rawStart := cfg.Date
	rawEnd := cfg.Date
	if strings.Contains(cfg.Date, ":") {
		parts := strings.Split(cfg.Date, ":")
		if len(parts) != 2 {
			return fmt.Errorf("invalid date range format: %q (expected YYYY-MM-DD:YYYY-MM-DD)", cfg.Date)
		}
		rawStart = parts[0]
		rawEnd = parts[1]
	}

	startDate, err := parseDateString(rawStart)
	if err != nil {
		return err
	}
	endDate, err := parseDateString(rawEnd)
	if err != nil {
		return err
	}

	slog.Debug("Processing range", "start_date", startDate, "end_date", endDate)

	var oauthClient *http.Client
	var albumID string
	var uploadedIDs = make(map[string]bool)

	if cfg.GooglePhotos.Enabled {
		oauthClient, err = getOAuthClient(ctx, cfg)
		if err != nil {
			return err
		}

		// Resolve the Google Photos album.
		if cfg.DryRun {
			albumID, err = findAlbumByTitle(ctx, oauthClient, cfg.GooglePhotos.AlbumName)
			if err != nil {
				return fmt.Errorf("searching for album: %w", err)
			}
			if albumID == "" {
				slog.Info("[DRY RUN] Would create album", "name", cfg.GooglePhotos.AlbumName)
			} else {
				slog.Debug("Found existing album", "name", cfg.GooglePhotos.AlbumName, "id", albumID)
			}
		} else {
			albumID, err = findOrCreateAlbum(ctx, oauthClient, cfg.GooglePhotos.AlbumName)
			if err != nil {
				return fmt.Errorf("resolving album: %w", err)
			}
			slog.Debug("Using album", "name", cfg.GooglePhotos.AlbumName, "id", albumID)
		}

		if albumID != "" {
			uploadedIDs, err = listAlbumAttachmentIDs(ctx, oauthClient, albumID)
			if err != nil {
				return fmt.Errorf("listing album contents: %w", err)
			}
			slog.Debug("Album content summary", "count", len(uploadedIDs))
		}
	}

	mbd := NewMyBrightDayClient(cfg.MyBrightDay.BaseURL, cookie)

	dependentIDs, centerIDs, err := mbd.GetDependentIDs(ctx)
	if err != nil {
		return fmt.Errorf("getting dependent IDs: %w", err)
	}
	slog.Debug("Found dependents", "count", len(dependentIDs))

	if len(dependentIDs) == 0 {
		return errors.New("no dependents found")
	}

	// Fetch center info and geocode for the first dependent (assume all centers share same locale/timezone for now).
	centerID := centerIDs[dependentIDs[0]]
	center, err := mbd.GetCenterInfo(ctx, centerID)
	if err != nil {
		return fmt.Errorf("getting center info for %s: %w", centerID, err)
	}

	location, err := time.LoadLocation(center.Timezone)
	if err != nil {
		return fmt.Errorf("loading timezone %q: %w", center.Timezone, err)
	}
	slog.Debug("Using center location", "center", center.Name, "timezone", center.Timezone)

	var lat, lon float64
	if cfg.LocationOverride != nil {
		lat = cfg.LocationOverride.Latitude
		lon = cfg.LocationOverride.Longitude
		slog.Debug("Using manual location override", "lat", lat, "lon", lon)
	} else {
		var err error
		lat, lon, err = mbd.GeocodeCenterAddress(ctx, center)
		if err != nil {
			addr := center.Address
			return fmt.Errorf("could not geocode center address (%s, %s, %s %s): %w",
				addr.AddressLine1, addr.City, addr.State, addr.PostalCode, err)
		}
		slog.Debug("Geocoded center", "lat", lat, "lon", lon)
	}

	mediaItems, err := mbd.GetMediaForDateRange(ctx, dependentIDs, startDate, endDate)
	if err != nil {
		return fmt.Errorf("getting media for %s to %s: %w", startDate, endDate, err)
	}

	slog.Info("Found media items", "start_date", startDate, "end_date", endDate, "count", len(mediaItems))

	totalPhotos := 0
	skippedPhotos := 0

	for _, item := range mediaItems {
		photoTime := item.CaptureTime.In(location)
		captureDate := photoTime.Format("2006-01-02")
		filename := fmt.Sprintf("daycare_%s_%s.jpg", captureDate, item.AttachmentID)
		outputDir := filepath.Join(cfg.Local.Directory, captureDate)
		localPath := filepath.Join(outputDir, filename)

		alreadyProcessed := false
		if cfg.GooglePhotos.Enabled && uploadedIDs[item.AttachmentID] {
			alreadyProcessed = true
		}
		if cfg.Local.Enabled && !cfg.DryRun {
			if _, err := os.Stat(localPath); err == nil {
				alreadyProcessed = true
			}
		}

		if alreadyProcessed {
			if !cfg.GooglePhotos.Enabled {
				slog.Debug("Skipping already-processed attachment", "id", item.AttachmentID)
				skippedPhotos++
				continue
			}
			if cfg.GooglePhotos.Enabled && uploadedIDs[item.AttachmentID] {
				slog.Debug("Skipping already-uploaded attachment", "id", item.AttachmentID)
				skippedPhotos++
				continue
			}
		}

		slog.Debug("Downloading media", "attachment_id", item.AttachmentID)

		imgData, err := mbd.DownloadMedia(ctx, item.AttachmentID)
		if err != nil {
			slog.Error("Error downloading media", "attachment_id", item.AttachmentID, "error", err)
			continue
		}

		jpegData, err := convertToJPEG(imgData)
		if err != nil {
			slog.Error("Error converting image to JPEG", "attachment_id", item.AttachmentID, "error", err)
			continue
		}

		_, offsetSecs := photoTime.Zone()
		offsetStr := FormatOffset(offsetSecs)

		meta := PhotoMeta{
			DateTime:       photoTime,
			TimezoneOffset: offsetStr,
			Latitude:       lat,
			Longitude:      lon,
		}

		jpegWithEXIF, err := addEXIF(jpegData, meta)
		if err != nil {
			slog.Error("Error adding EXIF", "attachment_id", item.AttachmentID, "error", err)
			continue
		}

		if cfg.Local.Enabled {
			if cfg.DryRun {
				slog.Info("[DRY RUN] Would save photo locally", "path", localPath)
			} else {
				if err := os.MkdirAll(outputDir, 0755); err != nil {
					slog.Error("Error creating output directory", "path", outputDir, "error", err)
					continue
				}
				if err := os.WriteFile(localPath, jpegWithEXIF, 0644); err != nil {
					slog.Error("Error saving photo locally", "path", localPath, "error", err)
				} else {
					slog.Debug("Saved photo locally", "path", localPath)
				}
			}
		}

		if cfg.GooglePhotos.Enabled {
			if uploadedIDs[item.AttachmentID] {
				slog.Debug("Already uploaded to Google Photos, skipping upload", "id", item.AttachmentID)
			} else if cfg.DryRun {
				slog.Info("[DRY RUN] Would upload photo to Google Photos",
					"filename", filename,
					"size", len(jpegWithEXIF),
					"time", photoTime.Format("2006-01-02 15:04:05 -07:00"))
			} else {
				mediaID, err := uploadToPhotos(ctx, oauthClient, jpegWithEXIF, filename, albumID)
				if err != nil {
					slog.Error("Error uploading photo", "attachment_id", item.AttachmentID, "error", err)
				} else {
					uploadedIDs[item.AttachmentID] = true
					slog.Debug("Uploaded photo", "filename", filename, "mediaID", mediaID)
				}
			}
		}

		totalPhotos++
	}

	slog.Info("Run summary",
		"start_date", startDate,
		"end_date", endDate,
		"processed", totalPhotos,
		"skipped", skippedPhotos)

	return nil
}
