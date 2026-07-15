package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dipeshc/mybrightday-backup/internal/mybrightday"
	"github.com/dipeshc/mybrightday-backup/internal/processor"
	"github.com/dipeshc/mybrightday-backup/internal/storage"
	"github.com/dipeshc/mybrightday-backup/internal/storage/googlephotos"
	"github.com/dipeshc/mybrightday-backup/internal/storage/local"
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

// parseDates resolves the user-supplied date string to start/end date strings and time.Time values.
func parseDates(dateStr string) (startDate, endDate string, startTime, endTime time.Time, err error) {
	rawStart := dateStr
	rawEnd := dateStr
	if strings.Contains(dateStr, ":") {
		parts := strings.SplitN(dateStr, ":", 2)
		if len(parts) != 2 {
			err = fmt.Errorf("invalid date range format: %q (expected YYYY-MM-DD:YYYY-MM-DD)", dateStr)
			return
		}
		rawStart = parts[0]
		rawEnd = parts[1]
	}

	startDate, err = parseDateString(rawStart)
	if err != nil {
		return
	}
	endDate, err = parseDateString(rawEnd)
	if err != nil {
		return
	}

	startTime, err = time.Parse("2006-01-02", startDate)
	if err != nil {
		return
	}
	endTime, err = time.Parse("2006-01-02", endDate)
	if err != nil {
		return
	}
	return
}

type storeStats struct {
	Saved   int
	Skipped int
}

// authenticate is a variable so tests can stub the MyBrightDay auth flow.
var authenticate = mybrightday.Authenticate

// Download fetches photos from the MyBrightDay API for the configured date range,
// processes them, and saves them to all enabled storage backends.
func Download(ctx context.Context, cfg *Config) error {
	if cfg.MyBrightDay.Email == "" || cfg.MyBrightDay.Password == "" {
		return errors.New("mybrightday_email and mybrightday_password are required — set them via flag, env var, file, or config.yaml")
	}

	startDate, endDate, startTime, endTime, err := parseDates(cfg.Date)
	if err != nil {
		return err
	}
	slog.Debug("Processing range", "start_date", startDate, "end_date", endDate)

	slog.Info("Authenticating with MyBrightDay...")
	cookie, err := authenticate(ctx, cfg.MyBrightDay.Email, cfg.MyBrightDay.Password)
	if err != nil {
		return fmt.Errorf("mybrightday authentication: %w", err)
	}

	// Build storage backends. Each constructor performs its own one-time
	// setup (directory creation, OAuth client, album lookup, dedup pre-fetch)
	// and returns an error on failure, so problems surface here rather than
	// mid-loop.
	var stores []storage.Storage
	if cfg.Local.Enabled {
		ls, err := local.New(cfg.Local, cfg.DryRun)
		if err != nil {
			return fmt.Errorf("initialising local storage: %w", err)
		}
		stores = append(stores, ls)
	}
	if cfg.GooglePhotos.Enabled {
		gp, err := googlephotos.New(ctx, cfg.GooglePhotos, cfg.DryRun, startTime, endTime)
		if err != nil {
			return fmt.Errorf("initialising google photos storage: %w", err)
		}
		stores = append(stores, gp)
	}

	mbd := mybrightday.NewClient(cfg.MyBrightDay.BaseURL, cookie)

	dependentIDs, centerIDs, err := mbd.GetDependentIDs(ctx)
	if err != nil {
		return fmt.Errorf("getting dependent IDs: %w", err)
	}
	slog.Debug("Found dependents", "count", len(dependentIDs))

	if len(dependentIDs) == 0 {
		return errors.New("no dependents found")
	}

	// Fetch center info for the first dependent (all dependents share the same locale for now).
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

	sort.Slice(mediaItems, func(i, j int) bool {
		return mediaItems[i].CaptureTime.Before(mediaItems[j].CaptureTime)
	})

	slog.Info("Found media items", "start_date", startDate, "end_date", endDate, "attachments", len(mediaItems))

	countsByDate := make(map[string]int)
	for _, item := range mediaItems {
		countsByDate[item.CaptureTime.In(location).Format("2006-01-02")]++
	}

	globalStats := make(map[string]*storeStats)
	dailyStats := make(map[string]*storeStats)
	for _, s := range stores {
		globalStats[s.Name()] = &storeStats{}
		dailyStats[s.Name()] = &storeStats{}
	}

	formatStats := func(stats map[string]*storeStats) []any {
		var args []any
		var keys []string
		for k := range stats {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			s := stats[k]
			args = append(args, k+"_saved", s.Saved, k+"_skipped", s.Skipped)
		}
		return args
	}

	totalPhotos := 0
	totalErrors := 0
	dailyPhotos := 0
	dailyErrors := 0
	currentProcessingDate := ""

	logAndResetDaily := func() {
		if currentProcessingDate == "" {
			return
		}
		args := append([]any{
			"date", currentProcessingDate,
			"attachments", countsByDate[currentProcessingDate],
			"photos", dailyPhotos,
			"errors", dailyErrors,
		}, formatStats(dailyStats)...)
		slog.Info("Processed media for date", args...)
		dailyPhotos = 0
		dailyErrors = 0
		for _, s := range dailyStats {
			s.Saved = 0
			s.Skipped = 0
		}
	}

	for _, item := range mediaItems {
		// Convert capture time to center-local timezone for filenames and directory structure.
		photoTime := item.CaptureTime.In(location)
		captureDate := photoTime.Format("2006-01-02")

		if captureDate != currentProcessingDate {
			logAndResetDaily()
			currentProcessingDate = captureDate
			slog.Info("Processing media for date", "date", currentProcessingDate, "attachments", countsByDate[currentProcessingDate])
		}

		slog.Debug("Downloading media", "attachment_id", item.AttachmentID)
		mediaData, contentType, err := mbd.DownloadMedia(ctx, item.AttachmentID)
		if err != nil {
			slog.Error("Error downloading media", "attachment_id", item.AttachmentID, "error", err)
			totalErrors++
			dailyErrors++
			continue
		}

		_, offsetSecs := photoTime.Zone()
		meta := processor.PhotoMeta{
			DateTime:       photoTime,
			TimezoneOffset: mybrightday.FormatOffset(offsetSecs),
			Latitude:       lat,
			Longitude:      lon,
		}

		var processedData []byte
		var ext string
		switch {
		case strings.HasPrefix(contentType, "image/"):
			ext = ".jpg"
			jpegData, err := processor.ConvertToJPEG(mediaData)
			if err != nil {
				slog.Error("Error converting image to JPEG", "attachment_id", item.AttachmentID, "error", err)
				totalErrors++
				dailyErrors++
				continue
			}
			processedData, err = processor.AddEXIF(jpegData, meta)
			if err != nil {
				slog.Error("Error adding EXIF", "attachment_id", item.AttachmentID, "error", err)
				totalErrors++
				dailyErrors++
				continue
			}
		case strings.HasPrefix(contentType, "video/"):
			// ponytail: only mp4/mov are mapped; other video subtypes get
			// labelled .mp4. Extend the mapping (and the googlephotos dedup
			// regex) if the feed ever serves another container.
			ext = ".mp4"
			if contentType == "video/quicktime" {
				ext = ".mov"
			}
			processedData, err = processor.AddMP4Metadata(mediaData, meta)
			if err != nil {
				slog.Error("Error adding MP4 metadata", "attachment_id", item.AttachmentID, "error", err)
				totalErrors++
				dailyErrors++
				continue
			}
		default:
			// Attachments are not guaranteed to be media — the feed also
			// returns documents (e.g. PDFs).
			slog.Debug("Skipping non-media attachment", "attachment_id", item.AttachmentID, "content_type", contentType)
			continue
		}

		photo := storage.Photo{
			AttachmentID: item.AttachmentID,
			Filename:     fmt.Sprintf("daycare_%s_%s%s", captureDate, item.AttachmentID, ext),
			Data:         processedData,
			CaptureTime:  photoTime,
		}

		for _, s := range stores {
			saved, err := s.Save(ctx, photo)
			if err != nil {
				slog.Error("Error saving photo", "store", s.Name(), "attachment_id", item.AttachmentID, "error", err)
				totalErrors++
				dailyErrors++
				continue
			}
			if saved {
				globalStats[s.Name()].Saved++
				dailyStats[s.Name()].Saved++
			} else {
				globalStats[s.Name()].Skipped++
				dailyStats[s.Name()].Skipped++
			}
		}

		totalPhotos++
		dailyPhotos++
	}

	logAndResetDaily()

	runArgs := append([]any{
		"start_date", startDate,
		"end_date", endDate,
		"attachments", len(mediaItems),
		"photos", totalPhotos,
		"errors", totalErrors,
	}, formatStats(globalStats)...)
	slog.Info("Run summary", runArgs...)

	if totalErrors > 0 {
		return fmt.Errorf("run completed with %d per-item error(s); see logs above", totalErrors)
	}

	return nil
}
