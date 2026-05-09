# Backup Workflow

This document explains the internal lifecycle of the `mbdb` command and how the application orchestrates the backup process.

## Process Overview

The backup process follows a linear execution flow:

1.  **Authentication**: Authenticates with MyBrightDay.
2.  **Date Resolution**: Parses the requested date or range.
3.  **Storage Initialisation**: Sets up enabled storage backends (local and/or Google Photos).
4.  **Context Discovery**: Fetches child (dependent) and daycare center information.
5.  **Metadata Preparation**: Resolves the center's timezone and GPS coordinates.
6.  **Media Discovery**: Fetches a list of all media attachments for the resolved date range.
7.  **Processing Loop**: Iterates through each media item to download, process, and save.

---

## 1. Authentication

The tool first logs into MyBrightDay using the provided email and password. This involves a multi-stage flow to exchange Auth0 credentials for a MyBrightDay session cookie.

## 2. Date Resolution

The `--date` flag is flexible. It can be:
*   **Absolute**: `2024-01-15`
*   **Relative**: `-1` (yesterday), `0` (today), `+1` (tomorrow)
*   **Range**: `-7:0` (last 7 days), `2024-01-01:2024-01-31`

The tool parses these strings into start and end dates.

## 3. Storage Initialisation

Each enabled storage backend is initialised before any photos are downloaded. Currently supported backends:

*   **Local**: Creates a `LocalStorage` instance pointing at the configured directory.
*   **Google Photos**: Creates a `GooglePhotosStorage` instance — this authenticates with Google, finds or creates the target album, and pre-fetches the set of already-uploaded attachment IDs for efficient deduplication.

If a backend is disabled (via `local.enabled: false` or `google_photos.enabled: false`), it is simply not included and has no effect on the run.

## 4. Context Discovery

The tool fetches the list of "dependents" (children) associated with the account. For each child, it identifies the daycare center they are enrolled in.

It then fetches detailed information about the center, including its physical address and its timezone (e.g., `America/New_York`).

## 5. Metadata Preparation

Before downloading media, the tool prepares the metadata that will be injected into the photos:
*   **Timezone**: Used to correctly localize the capture timestamps.
*   **GPS Coordinates**: The center's address is geocoded using the Nominatim API to get latitude and longitude. A manual override can be provided via `location_override` in the config.

## 6. Media Discovery

The tool calls the MyBrightDay API for each child and date in the range. It looks for "daily reports" and extracts all "attachments" (photos).

Each attachment has a unique `AttachmentID` and a `CaptureTime`.

## 7. Processing Loop

For each discovered attachment, the tool performs the following:

### Download & Conversion
The raw image data is downloaded from MyBrightDay. The tool ensures the image is in JPEG format, converting it if necessary.

### Metadata Injection (EXIF)
The tool injects the following into the JPEG's EXIF header:
*   **Original Timestamp**: Set to the `CaptureTime` from the MyBrightDay API, localized to the center's timezone.
*   **GPS Data**: Set to the geocoded coordinates of the daycare center.

### Storage
The processed photo is passed to each enabled storage backend's `Save` method. Each backend handles its own deduplication:

*   **Local**: Saves the processed JPEG to a date-partitioned directory structure (e.g., `photos/2024-01-15/daycare_...jpg`). Skips the file if it already exists.
*   **Google Photos**: Uploads the processed JPEG with a descriptive filename and adds it to the specified album. Skips items whose attachment ID was found in the pre-fetched deduplication set.
