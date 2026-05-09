package processor

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"math"
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

// ConvertToJPEG converts image data to JPEG format if it isn't already.
func ConvertToJPEG(data []byte) ([]byte, error) {
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

// encodeJPEG encodes an image as JPEG with quality 100 to minimise lossy artefacts.
func encodeJPEG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 100}); err != nil {
		return nil, fmt.Errorf("encoding JPEG: %w", err)
	}
	return buf.Bytes(), nil
}

// AddEXIF injects EXIF metadata (date/time, GPS, camera info) into JPEG data.
func AddEXIF(jpegData []byte, meta PhotoMeta) ([]byte, error) {
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
