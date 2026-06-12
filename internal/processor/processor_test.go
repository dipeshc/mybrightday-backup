package processor

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"strings"
	"testing"
	"time"

	exif "github.com/dsoprea/go-exif/v3"
	exifcommon "github.com/dsoprea/go-exif/v3/common"
)

func newTestImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	img.Set(1, 0, color.RGBA{G: 255, A: 255})
	img.Set(0, 1, color.RGBA{B: 255, A: 255})
	img.Set(1, 1, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	return img
}

func encodePNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, newTestImage()); err != nil {
		t.Fatalf("encoding PNG: %v", err)
	}
	return buf.Bytes()
}

func encodeTestJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, newTestImage(), nil); err != nil {
		t.Fatalf("encoding JPEG: %v", err)
	}
	return buf.Bytes()
}

func encodeGIF(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := gif.Encode(&buf, newTestImage(), nil); err != nil {
		t.Fatalf("encoding GIF: %v", err)
	}
	return buf.Bytes()
}

func isJPEG(data []byte) bool {
	return len(data) >= 2 && data[0] == 0xFF && data[1] == 0xD8
}

func TestConvertToJPEG(t *testing.T) {
	jpegData := encodeTestJPEG(t)

	t.Run("jpeg passthrough", func(t *testing.T) {
		out, err := ConvertToJPEG(jpegData)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !bytes.Equal(out, jpegData) {
			t.Error("JPEG input was re-encoded, expected identical bytes")
		}
	})

	t.Run("png converted", func(t *testing.T) {
		out, err := ConvertToJPEG(encodePNG(t))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !isJPEG(out) {
			t.Error("output is not JPEG")
		}
	})

	t.Run("gif via generic fallback", func(t *testing.T) {
		out, err := ConvertToJPEG(encodeGIF(t))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !isJPEG(out) {
			t.Error("output is not JPEG")
		}
	})

	t.Run("corrupt png", func(t *testing.T) {
		data := append([]byte("\x89PNG\r\n\x1a\n"), []byte("not a real png")...)
		_, err := ConvertToJPEG(data)
		if err == nil || !strings.Contains(err.Error(), "decoding PNG") {
			t.Errorf("err = %v, want decoding PNG error", err)
		}
	})

	t.Run("garbage", func(t *testing.T) {
		_, err := ConvertToJPEG([]byte("definitely not an image"))
		if err == nil || !strings.Contains(err.Error(), "decoding image") {
			t.Errorf("err = %v, want decoding image error", err)
		}
	})
}

func testMeta() PhotoMeta {
	return PhotoMeta{
		DateTime:       time.Date(2024, 1, 15, 12, 34, 56, 0, time.FixedZone("PST", -8*3600)),
		TimezoneOffset: "-08:00",
		Latitude:       37.4220,
		Longitude:      -122.0841,
	}
}

// extractTags parses EXIF out of JPEG bytes into a tag-name → formatted-value map.
func extractTags(t *testing.T, data []byte) map[string]string {
	t.Helper()
	rawExif, err := exif.SearchAndExtractExif(data)
	if err != nil {
		t.Fatalf("extracting EXIF: %v", err)
	}
	entries, _, err := exif.GetFlatExifData(rawExif, nil)
	if err != nil {
		t.Fatalf("parsing EXIF: %v", err)
	}
	tags := make(map[string]string)
	for _, e := range entries {
		tags[e.TagName] = e.Formatted
	}
	return tags
}

func TestAddEXIF(t *testing.T) {
	out, err := AddEXIF(encodeTestJPEG(t), testMeta())
	if err != nil {
		t.Fatalf("AddEXIF: %v", err)
	}
	if !isJPEG(out) {
		t.Fatal("output is not JPEG")
	}

	tags := extractTags(t, out)
	want := map[string]string{
		"Make":               "Daycare Camera",
		"Model":              "Daycare",
		"Software":           "Daycare App",
		"DateTime":           "2024:01:15 12:34:56",
		"DateTimeOriginal":   "2024:01:15 12:34:56",
		"DateTimeDigitized":  "2024:01:15 12:34:56",
		"OffsetTimeOriginal": "-08:00",
		"GPSLatitudeRef":     "N",
		"GPSLongitudeRef":    "W",
	}
	for name, val := range want {
		if got := tags[name]; got != val {
			t.Errorf("tag %s = %q, want %q", name, got, val)
		}
	}
}

func TestAddEXIFGPSCoordinates(t *testing.T) {
	meta := testMeta()
	out, err := AddEXIF(encodeTestJPEG(t), meta)
	if err != nil {
		t.Fatalf("AddEXIF: %v", err)
	}

	rawExif, err := exif.SearchAndExtractExif(out)
	if err != nil {
		t.Fatalf("extracting EXIF: %v", err)
	}
	im, err := exifcommon.NewIfdMappingWithStandard()
	if err != nil {
		t.Fatalf("creating IFD mapping: %v", err)
	}
	_, index, err := exif.Collect(im, exif.NewTagIndex(), rawExif)
	if err != nil {
		t.Fatalf("collecting EXIF: %v", err)
	}
	gpsIfd, err := exif.FindIfdFromRootIfd(index.RootIfd, "IFD/GPSInfo")
	if err != nil {
		t.Fatalf("finding GPS IFD: %v", err)
	}
	info, err := gpsIfd.GpsInfo()
	if err != nil {
		t.Fatalf("decoding GPS info: %v", err)
	}

	if math.Abs(info.Latitude.Decimal()-meta.Latitude) > 0.001 {
		t.Errorf("latitude = %f, want %f", info.Latitude.Decimal(), meta.Latitude)
	}
	if math.Abs(info.Longitude.Decimal()-meta.Longitude) > 0.001 {
		t.Errorf("longitude = %f, want %f", info.Longitude.Decimal(), meta.Longitude)
	}
}

func TestAddEXIFSouthernEasternHemisphere(t *testing.T) {
	meta := PhotoMeta{
		DateTime:       time.Date(2024, 6, 1, 9, 0, 0, 0, time.UTC),
		TimezoneOffset: "+10:00",
		Latitude:       -33.8688,
		Longitude:      151.2093,
	}
	out, err := AddEXIF(encodeTestJPEG(t), meta)
	if err != nil {
		t.Fatalf("AddEXIF: %v", err)
	}

	tags := extractTags(t, out)
	if tags["GPSLatitudeRef"] != "S" {
		t.Errorf("GPSLatitudeRef = %q, want S", tags["GPSLatitudeRef"])
	}
	if tags["GPSLongitudeRef"] != "E" {
		t.Errorf("GPSLongitudeRef = %q, want E", tags["GPSLongitudeRef"])
	}
}

func TestAddEXIFOverExistingEXIF(t *testing.T) {
	first, err := AddEXIF(encodeTestJPEG(t), testMeta())
	if err != nil {
		t.Fatalf("first AddEXIF: %v", err)
	}

	meta := testMeta()
	meta.DateTime = time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC)
	second, err := AddEXIF(first, meta)
	if err != nil {
		t.Fatalf("second AddEXIF: %v", err)
	}

	tags := extractTags(t, second)
	if got := tags["DateTime"]; got != "2025:02:03 04:05:06" {
		t.Errorf("DateTime = %q, want 2025:02:03 04:05:06", got)
	}
}

func TestAddEXIFRejectsNonJPEG(t *testing.T) {
	_, err := AddEXIF([]byte("not a jpeg"), testMeta())
	if err == nil || !strings.Contains(err.Error(), "parsing JPEG structure") {
		t.Errorf("err = %v, want parsing JPEG structure error", err)
	}
}
