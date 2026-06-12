package googlephotos

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dipeshc/mybrightday-backup/internal/storage"
)

func testGPPhoto() storage.Photo {
	return storage.Photo{
		AttachmentID: "att-1",
		Filename:     "daycare_2024-01-15_0123456789abcdef01234567.jpg",
		Data:         []byte("jpeg-data"),
		CaptureTime:  time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
	}
}

func TestStorageName(t *testing.T) {
	s := &GooglePhotosStorage{}
	if s.Name() != "google_photos" {
		t.Errorf("Name = %q, want google_photos", s.Name())
	}
}

func TestSaveSkipsAlreadyUploaded(t *testing.T) {
	requests := 0
	s := &GooglePhotosStorage{
		client: stubClient(func(req *http.Request) (int, any) {
			requests++
			return http.StatusOK, ""
		}),
		uploadedIDs: map[string]bool{"att-1": true},
	}

	saved, err := s.Save(context.Background(), testGPPhoto())
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if saved {
		t.Error("saved = true for duplicate, want false")
	}
	if requests != 0 {
		t.Errorf("requests = %d, want 0 (dedup must not hit the API)", requests)
	}
}

func TestSaveDryRun(t *testing.T) {
	requests := 0
	s := &GooglePhotosStorage{
		client: stubClient(func(req *http.Request) (int, any) {
			requests++
			return http.StatusOK, ""
		}),
		uploadedIDs: map[string]bool{},
		dryRun:      true,
	}

	saved, err := s.Save(context.Background(), testGPPhoto())
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if saved || requests != 0 {
		t.Errorf("saved=%t requests=%d, want false and 0 in dry-run", saved, requests)
	}
}

func TestSaveUploads(t *testing.T) {
	s := &GooglePhotosStorage{
		client: stubClient(func(req *http.Request) (int, any) {
			if strings.HasSuffix(req.URL.Path, "/uploads") {
				return http.StatusOK, "tok-1"
			}
			return http.StatusOK, batchCreateResponse{NewMediaItemResults: []mediaItemResult{
				{MediaItem: mediaItem{ID: "media-1"}},
			}}
		}),
		albumID:     "alb-1",
		uploadedIDs: map[string]bool{},
	}

	saved, err := s.Save(context.Background(), testGPPhoto())
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !saved {
		t.Error("saved = false, want true")
	}
	if !s.uploadedIDs["att-1"] {
		t.Error("uploadedIDs not updated after successful upload")
	}
}

func TestSaveUploadError(t *testing.T) {
	s := &GooglePhotosStorage{
		client: stubClient(func(req *http.Request) (int, any) {
			return http.StatusBadRequest, "bad"
		}),
		uploadedIDs: map[string]bool{},
	}

	_, err := s.Save(context.Background(), testGPPhoto())
	if err == nil || !strings.Contains(err.Error(), "uploading") {
		t.Errorf("err = %v, want uploading error", err)
	}
	if s.uploadedIDs["att-1"] {
		t.Error("failed upload must not be marked as uploaded")
	}
}

func TestNewErrorPaths(t *testing.T) {
	now := time.Now()

	t.Run("missing refresh token", func(t *testing.T) {
		_, err := New(context.Background(), Config{}, false, now, now)
		if err == nil || !strings.Contains(err.Error(), "refresh_token missing") {
			t.Errorf("err = %v, want refresh_token missing", err)
		}
	})

	t.Run("invalid client secret", func(t *testing.T) {
		_, err := New(context.Background(), Config{ClientSecret: "{not json"}, false, now, now)
		if err == nil || !strings.Contains(err.Error(), "parsing client secret") {
			t.Errorf("err = %v, want parsing client secret", err)
		}
	})
}
