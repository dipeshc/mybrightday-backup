package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// RunProcess searches for emails, extracts photos, and uploads them to Google Photos.
func RunProcess(ctx context.Context, configPath string, dryRun bool) error {
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return err
	}

	slog.Debug("Config loaded", "path", configPath)

	client, err := getOAuthClient(ctx, cfg)
	if err != nil {
		return err
	}

	// Create a dedicated HTTP client for image downloads with a timeout.
	downloadClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Resolve the Google Photos album and build the set of already-uploaded UUIDs.
	var albumID string
	if dryRun {
		albumID, err = findAlbumByTitle(ctx, client, cfg.AlbumName)
		if err != nil {
			return fmt.Errorf("searching for album: %w", err)
		}
		if albumID == "" {
			slog.Info("[DRY RUN] Would create album", "name", cfg.AlbumName)
		} else {
			slog.Debug("Found existing album", "name", cfg.AlbumName, "id", albumID)
		}
	} else {
		albumID, err = findOrCreateAlbum(ctx, client, cfg.AlbumName)
		if err != nil {
			return fmt.Errorf("resolving album: %w", err)
		}
		slog.Debug("Using album", "name", cfg.AlbumName, "id", albumID)
	}

	uploadedUUIDs := make(map[string]bool)
	if albumID != "" {
		uploadedUUIDs, err = listAlbumUUIDs(ctx, client, albumID)
		if err != nil {
			return fmt.Errorf("listing album contents: %w", err)
		}
		slog.Debug("Album content summary", "count", len(uploadedUUIDs))
	}

	gmailSrv, err := newGmailService(ctx, client)
	if err != nil {
		return fmt.Errorf("creating Gmail service: %w", err)
	}

	messages, err := searchEmails(ctx, gmailSrv, cfg)
	if err != nil {
		return fmt.Errorf("searching emails: %w", err)
	}

	if len(messages) == 0 {
		slog.Info("No matching emails found in inbox.")
		return nil
	}

	slog.Info("Found matching emails", "count", len(messages))

	totalPhotos := 0
	skippedPhotos := 0
	processedEmails := 0

	for _, msg := range messages {
		emailData, err := fetchEmail(ctx, gmailSrv, msg.Id)
		if err != nil {
			slog.Error("Error fetching email", "id", msg.Id, "error", err)
			continue
		}

		slog.Debug("Processing email",
			"id", emailData.ID,
			"subject", emailData.Subject,
			"date", emailData.Date.Format(time.RFC3339))

		images, err := extractImageURLs(emailData.HTMLBody)
		if err != nil {
			slog.Error("Error extracting image URLs",
				"id", emailData.ID,
				"subject", emailData.Subject,
				"error", err)
			continue
		}

		if len(images) == 0 {
			slog.Debug("No image URLs found in email",
				"id", emailData.ID,
				"subject", emailData.Subject)
			continue
		}

		slog.Info("Found images in email",
			"count", len(images),
			"subject", emailData.Subject)

		for i, img := range images {
			if uploadedUUIDs[img.UUID] {
				slog.Debug("Skipping already-uploaded image",
					"index", i+1,
					"total", len(images),
					"uuid", img.UUID)
				skippedPhotos++
				continue
			}

			filename := fmt.Sprintf("daycare_%s_%02d_%s.jpg",
				emailData.Date.Format("2006-01-02"),
				i+1,
				img.UUID,
			)

			slog.Debug("Downloading image",
				"index", i+1,
				"total", len(images),
				"url", img.URL)

			imgData, err := downloadImage(ctx, downloadClient, img.URL)
			if err != nil {
				slog.Error("Error downloading image", "index", i+1, "error", err)
				continue
			}

			jpegData, err := convertToJPEG(imgData)
			if err != nil {
				slog.Error("Error converting image to JPEG", "index", i+1, "error", err)
				continue
			}

			photoTime, err := calculatePhotoTime(emailData.Date, i, cfg)
			if err != nil {
				slog.Error("Error calculating photo time", "index", i+1, "error", err)
				continue
			}
			meta := PhotoMeta{
				DateTime:       photoTime,
				TimezoneOffset: cfg.Photo.TimezoneOffset,
				Latitude:       cfg.Photo.Latitude,
				Longitude:      cfg.Photo.Longitude,
			}

			jpegWithEXIF, err := addEXIF(jpegData, meta)
			if err != nil {
				slog.Error("Error adding EXIF to image", "index", i+1, "error", err)
				continue
			}

			if dryRun {
				slog.Info("[DRY RUN] Would upload photo",
					"filename", filename,
					"size", len(jpegWithEXIF),
					"time", photoTime.Format("15:04:05"))
			} else {
				mediaID, err := uploadToPhotos(ctx, client, jpegWithEXIF, filename, albumID)
				if err != nil {
					slog.Error("Error uploading image", "index", i+1, "error", err)
					continue
				}
				// Mark as uploaded so duplicates within the same run are skipped.
				uploadedUUIDs[img.UUID] = true
				slog.Debug("Uploaded photo", "filename", filename, "mediaID", mediaID)
			}

			totalPhotos++
		}

		processedEmails++
	}

	slog.Info("Run summary",
		"emails", processedEmails,
		"uploaded", totalPhotos,
		"skipped", skippedPhotos)

	return nil
}
