# Metadata & Image Processing

To ensure your backed-up photos are as useful as possible, the tool performs automatic image processing and metadata injection. This document details how GPS coordinates and timestamps are handled.

## Image Processing

Photos downloaded from MyBrightDay are processed to ensure compatibility and enriched with EXIF metadata.

1.  **JPEG Normalization**: The tool ensures all images are in JPEG format. If a photo is downloaded as a PNG, it is automatically converted to a high-quality JPEG (Quality 95).
2.  **EXIF Injection**: The tool injects a standard set of EXIF tags into the JPEG header. This allows photo management software (like Apple Photos, Google Photos, or Adobe Lightroom) to correctly sort and map the images.

---

## 1. Timestamps & Timezones

MyBrightDay provides the capture time of each photo in UTC. To ensure the photo appears at the correct local time in your library, the tool:

1.  **Resolves Center Timezone**: Fetches the daycare center's timezone (e.g., `America/Los_Angeles`) from the MyBrightDay API.
2.  **Localizes the Time**: Converts the UTC capture time into the center's local time.
3.  **Injects EXIF Tags**: Sets the following tags:
    *   `DateTimeOriginal`
    *   `OffsetTimeOriginal` (e.g., `-08:00`)
    *   `DateTimeDigitized`

## 2. Geocoding & GPS Data

By default, MyBrightDay photos do not contain GPS coordinates. The tool automatically adds the location of the daycare center to every photo.

### Automatic Geocoding (Nominatim)

The tool uses the [Nominatim OpenStreetMap API](https://nominatim.openstreetmap.org/) to resolve the physical address of the daycare center into GPS coordinates.

1.  **Address Lookup**: The tool fetches the center's address from MyBrightDay.
2.  **Geocoding Request**: It sends a request to Nominatim with the address.
3.  **Fallback**: If the specific street address cannot be geocoded, the tool falls back to searching for the center's name and city.
4.  **Injected Tags**:
    *   `GPSLatitude` / `GPSLatitudeRef`
    *   `GPSLongitude` / `GPSLongitudeRef`
    *   `GPSAltitude` (defaulted to 0)

### Manual Override

If the automatic geocoding is incorrect, or if you prefer to use a specific location, you can manually override the coordinates using the following configuration options:

```bash
./mbdb download --location-override.latitude 40.7128 --location-override.longitude -74.0060
```

*   **Flag**: `--location-override.latitude` / `--location-override.longitude`
*   **Env Var**: `LOCATION_OVERRIDE_LATITUDE` / `LOCATION_OVERRIDE_LONGITUDE`
*   **YAML Key**: `location_override.latitude` / `location_override.longitude`

---

## Technical Details

The metadata injection is performed using the `github.com/dsoprea/go-exif` library. The tool creates a standard IFD structure:
*   **IFD0 (Root)**: Contains camera make/model ("Daycare Camera") and software version.
*   **Exif IFD**: Contains high-precision timestamps and timezone offsets.
*   **GPS IFD**: Contains latitude, longitude, and reference tags.
