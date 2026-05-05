package app

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"net/http"
	"time"

	exif "github.com/dsoprea/go-exif/v3"
	exifcommon "github.com/dsoprea/go-exif/v3/common"
	jpegstructure "github.com/dsoprea/go-jpeg-image-structure/v2"
)

// PhotoMeta holds metadata to inject into a JPEG image.
type PhotoMeta struct {
	DateTime       time.Time
	TimezoneOffset string
	Latitude       float64
	Longitude      float64
}

// downloadImage fetches an image from a URL and returns the raw bytes.
func downloadImage(ctx context.Context, client *http.Client, imgURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imgURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d for %s", resp.StatusCode, imgURL)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading image data: %w", err)
	}

	return data, nil
}

// convertToJPEG converts image data to JPEG format if it isn't already.
func convertToJPEG(data []byte) ([]byte, error) {
	// Check magic bytes to detect format.
	if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xD8 {
		// Already JPEG.
		return data, nil
	}

	// Try to decode as PNG.
	if len(data) >= 8 && string(data[:4]) == "\x89PNG" {
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("decoding PNG: %w", err)
		}
		return encodeJPEG(img)
	}

	// Try generic image decode as fallback.
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decoding image: %w", err)
	}
	return encodeJPEG(img)
}

// encodeJPEG encodes an image as JPEG with quality 95.
func encodeJPEG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		return nil, fmt.Errorf("encoding JPEG: %w", err)
	}
	return buf.Bytes(), nil
}

// addEXIF injects EXIF metadata (date/time, GPS, camera info) into JPEG data.
func addEXIF(jpegData []byte, meta PhotoMeta) ([]byte, error) {
	jmp := jpegstructure.NewJpegMediaParser()

	mc, err := jmp.ParseBytes(jpegData)
	if err != nil {
		return nil, fmt.Errorf("parsing JPEG structure: %w", err)
	}

	sl := mc.(*jpegstructure.SegmentList)

	rootIb, err := sl.ConstructExifBuilder()
	if err != nil {
		// No existing EXIF — create a new builder.
		im, err := exifcommon.NewIfdMappingWithStandard()
		if err != nil {
			return nil, fmt.Errorf("getting IFD mapping: %w", err)
		}

		ti := exif.NewTagIndex()
		if err := exif.LoadStandardTags(ti); err != nil {
			return nil, fmt.Errorf("loading standard tags: %w", err)
		}

		rootIb = exif.NewIfdBuilder(im, ti, exifcommon.IfdStandardIfdIdentity, exifcommon.EncodeDefaultByteOrder)
	}

	dateStr := meta.DateTime.Format("2006:01:02 15:04:05")

	// Set IFD0 (root) tags.
	setTag(rootIb, "Make", "Daycare Camera")
	setTag(rootIb, "Model", "Daycare")
	setTag(rootIb, "Software", "Daycare App")
	setTag(rootIb, "DateTime", dateStr)

	// Build EXIF IFD for date/time tags.
	im, err := exifcommon.NewIfdMappingWithStandard()
	if err != nil {
		return nil, fmt.Errorf("getting standard IFD mapping for EXIF: %w", err)
	}

	ti := exif.NewTagIndex()
	if err := exif.LoadStandardTags(ti); err != nil {
		return nil, fmt.Errorf("loading standard tags for EXIF: %w", err)
	}

	exifChildIb := exif.NewIfdBuilder(im, ti, exifcommon.IfdExifStandardIfdIdentity, exifcommon.EncodeDefaultByteOrder)
	setTag(exifChildIb, "DateTimeOriginal", dateStr)
	setTag(exifChildIb, "DateTimeDigitized", dateStr)
	setTag(exifChildIb, "OffsetTimeOriginal", meta.TimezoneOffset)
	setTag(exifChildIb, "OffsetTimeDigitized", meta.TimezoneOffset)
	setTag(exifChildIb, "SubSecTimeOriginal", "00")
	setTag(exifChildIb, "SubSecTimeDigitized", "00")

	if err := rootIb.AddChildIb(exifChildIb); err != nil {
		return nil, fmt.Errorf("adding EXIF IFD: %w", err)
	}

	// Build GPS IFD.
	gpsIb := exif.NewIfdBuilder(im, ti, exifcommon.IfdGpsInfoStandardIfdIdentity, exifcommon.EncodeDefaultByteOrder)

	latRef := "N"
	lat := meta.Latitude
	if lat < 0 {
		latRef = "S"
		lat = -lat
	}

	lonRef := "E"
	lon := meta.Longitude
	if lon < 0 {
		lonRef = "W"
		lon = -lon
	}

	setTag(gpsIb, "GPSLatitudeRef", latRef)
	setGPSCoordinate(gpsIb, "GPSLatitude", lat)
	setTag(gpsIb, "GPSLongitudeRef", lonRef)
	setGPSCoordinate(gpsIb, "GPSLongitude", lon)

	// Altitude = 0.
	setTag(gpsIb, "GPSAltitudeRef", []byte{0})
	setTagRational(gpsIb, "GPSAltitude", exifcommon.Rational{Numerator: 0, Denominator: 1})

	if err := rootIb.AddChildIb(gpsIb); err != nil {
		return nil, fmt.Errorf("adding GPS IFD: %w", err)
	}

	// Write EXIF back into the JPEG.
	if err := sl.SetExif(rootIb); err != nil {
		return nil, fmt.Errorf("setting EXIF: %w", err)
	}

	var buf bytes.Buffer
	if err := sl.Write(&buf); err != nil {
		return nil, fmt.Errorf("writing JPEG: %w", err)
	}

	return buf.Bytes(), nil
}

// setTag sets a string or byte-slice tag on an IFD builder, ignoring errors for optional tags.
func setTag(ib *exif.IfdBuilder, tagName string, value any) {
	_ = ib.SetStandardWithName(tagName, value)
}

// setTagRational sets a rational tag on an IFD builder.
func setTagRational(ib *exif.IfdBuilder, tagName string, value exifcommon.Rational) {
	_ = ib.SetStandardWithName(tagName, []exifcommon.Rational{value})
}

// setGPSCoordinate converts a decimal degree coordinate to degrees/minutes/seconds
// and sets it as a GPS coordinate tag.
func setGPSCoordinate(ib *exif.IfdBuilder, tagName string, decimal float64) {
	degrees := int(decimal)
	minutesFloat := (decimal - float64(degrees)) * 60
	minutes := int(minutesFloat)
	seconds := (minutesFloat - float64(minutes)) * 60

	// Use a denominator of 100 for sub-second precision.
	secondsNumerator := uint32(math.Round(seconds * 100))

	rationals := []exifcommon.Rational{
		{Numerator: uint32(degrees), Denominator: 1},
		{Numerator: uint32(minutes), Denominator: 1},
		{Numerator: secondsNumerator, Denominator: 100},
	}

	_ = ib.SetStandardWithName(tagName, rationals)
}

// calculatePhotoTime computes the timestamp for a photo based on the email date
// and the photo's position in the sequence.
func calculatePhotoTime(emailDate time.Time, photoIndex int, cfg *Config) (time.Time, error) {
	// Use the email date but replace the time with the configured start hour.
	year, month, day := emailDate.Date()

	totalMinutes := (photoIndex * cfg.Photo.IntervalMinutes)
	hour := cfg.Photo.StartHour + totalMinutes/60
	minute := totalMinutes % 60

	// Parse the timezone offset.
	offset, err := parseOffsetSeconds(cfg.Photo.TimezoneOffset)
	if err != nil {
		return time.Time{}, err
	}
	loc := time.FixedZone("Daycare", offset)

	return time.Date(year, month, day, hour, minute, 0, 0, loc), nil
}

// parseOffsetSeconds converts a timezone offset string like "-08:00" to seconds.
func parseOffsetSeconds(offset string) (int, error) {
	if len(offset) < 5 {
		return 0, fmt.Errorf("invalid offset format: %q", offset)
	}

	sign := 1
	if offset[0] == '-' {
		sign = -1
	} else if offset[0] != '+' {
		return 0, fmt.Errorf("offset must start with + or -: %q", offset)
	}

	var hours, minutes int
	n, err := fmt.Sscanf(offset[1:], "%d:%d", &hours, &minutes)
	if err != nil {
		return 0, fmt.Errorf("parsing offset %q: %w", offset, err)
	}
	if n != 2 {
		return 0, fmt.Errorf("invalid offset format (expected H:M): %q", offset)
	}

	return sign * (hours*3600 + minutes*60), nil
}
