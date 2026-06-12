package local

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/dipeshc/mybrightday-backup/internal/storage"
)

func testPhoto() storage.Photo {
	return storage.Photo{
		AttachmentID: "abc123",
		Filename:     "daycare_2024-01-15_abc123.jpg",
		Data:         []byte("jpeg-bytes"),
		CaptureTime:  time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
	}
}

func TestNewCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "photos")
	s, err := New(Config{Directory: dir}, false)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s.Name() != "local" {
		t.Errorf("Name = %q, want local", s.Name())
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("directory not created: %v", err)
	}
}

func TestNewDryRunSkipsDirectoryCreation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "photos")
	if _, err := New(Config{Directory: dir}, true); err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("directory should not exist in dry-run, stat err = %v", err)
	}
}

func TestNewDirectoryCreationFailure(t *testing.T) {
	// A regular file where a directory is needed makes MkdirAll fail.
	file := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("writing blocker: %v", err)
	}
	if _, err := New(Config{Directory: filepath.Join(file, "photos")}, false); err == nil {
		t.Error("expected error when directory path is under a file")
	}
}

func TestSaveWritesPhoto(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Config{Directory: dir}, false)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	photo := testPhoto()
	saved, err := s.Save(context.Background(), photo)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !saved {
		t.Error("saved = false, want true")
	}

	path := filepath.Join(dir, "2024-01-15", photo.Filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading saved photo: %v", err)
	}
	if string(data) != "jpeg-bytes" {
		t.Errorf("data = %q, want jpeg-bytes", data)
	}
}

func TestSaveSkipsExistingPhoto(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Config{Directory: dir}, false)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	photo := testPhoto()
	if _, err := s.Save(context.Background(), photo); err != nil {
		t.Fatalf("first Save: %v", err)
	}

	saved, err := s.Save(context.Background(), photo)
	if err != nil {
		t.Fatalf("second Save: %v", err)
	}
	if saved {
		t.Error("saved = true on existing file, want false")
	}
}

func TestSaveDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Config{Directory: dir}, true)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	saved, err := s.Save(context.Background(), testPhoto())
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if saved {
		t.Error("saved = true in dry-run, want false")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("dry-run wrote %d entries, want 0", len(entries))
	}
}

func TestSaveMkdirFailure(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Config{Directory: dir}, false)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// A regular file named after the capture date blocks the subdirectory.
	if err := os.WriteFile(filepath.Join(dir, "2024-01-15"), []byte("x"), 0o644); err != nil {
		t.Fatalf("writing blocker: %v", err)
	}

	if _, err := s.Save(context.Background(), testPhoto()); err == nil {
		t.Error("expected error when date directory is blocked by a file")
	}
}

func TestSaveWriteFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not enforced the same way on windows")
	}
	dir := t.TempDir()
	s, err := New(Config{Directory: dir}, false)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	dateDir := filepath.Join(dir, "2024-01-15")
	if err := os.MkdirAll(dateDir, 0o555); err != nil {
		t.Fatalf("creating read-only dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dateDir, 0o755) })

	if _, err := s.Save(context.Background(), testPhoto()); err == nil {
		t.Error("expected error writing into read-only directory")
	}
}
