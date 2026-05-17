# Backup Workflow

This document explains the internal lifecycle of the `mbdb` command and how the application orchestrates the backup process.

## Process Overview

The backup process follows a linear execution flow:

1.  **Date Resolution**: Parses the requested date or range.
2.  **Authentication**: Authenticates with MyBrightDay.
3.  **Storage Initialisation**: Constructs and initialises every enabled storage backend (local and/or Google Photos).
4.  **Context Discovery**: Fetches child (dependent) and daycare center information.
5.  **Metadata Preparation**: Resolves the center's timezone and GPS coordinates.
6.  **Media Discovery**: Fetches a list of all media attachments for the resolved date range.
7.  **Processing Loop**: Iterates through each media item to download, process, and save.

---

## 1. Date Resolution

The `--date` flag is flexible. It can be:
*   **Absolute**: `2024-01-15`
*   **Relative**: `-1` (yesterday), `0` (today), `+1` (tomorrow)
*   **Range**: `-7:0` (last 7 days), `2024-01-01:2024-01-31`

The tool parses these strings into start and end dates.

## 2. Authentication

The tool then logs into MyBrightDay using the provided email and password. This involves a multi-stage flow to exchange Auth0 credentials for a MyBrightDay session cookie.

## 3. Storage Initialisation

After MyBrightDay authentication and before any per-photo work, every enabled storage backend is constructed. Each constructor performs the backend's one-time setup and returns an error on failure, so credential, network, or filesystem problems surface here rather than mid-loop. Currently supported backends:

*   **Local**: Creates the configured root directory if it does not already exist. Per-date subdirectories are still created on demand inside `Save`.
*   **Google Photos**: Exchanges the persisted refresh token for a short-lived access token, finds or creates the target album, and pre-fetches the set of already-uploaded attachment IDs for efficient deduplication. The access token lives only in memory for the duration of the run.

If a backend is disabled (via `local.enabled: false` or `google_photos.enabled: false`), it is simply not constructed and has no effect on the run.

## 4. Context Discovery

The tool fetches the list of "dependents" (children) associated with the account. For each child, it identifies the daycare center they are enrolled in.

It then fetches detailed information about the center, including its physical address and its timezone (e.g., `America/New_York`).

## 5. Metadata Preparation

Before downloading media, the tool prepares the metadata that will be injected into the photos:
*   **Timezone**: Used to correctly localize the capture timestamps.
*   **GPS Coordinates**: The center's address is geocoded using the Nominatim API to get latitude and longitude. A manual override can be provided via `location_override` in the config.

## 6. Media Discovery

The tool calls the MyBrightDay API for each child and date in the range. It looks for "daily reports" and extracts all "attachments". Most attachments are photos, but the feed also returns non-image documents (e.g. PDF daily-report summaries); those are filtered out during the processing loop.

Each attachment has a unique `AttachmentID` and a `CaptureTime`.

## 7. Processing Loop

For each discovered attachment, the tool performs the following:

### Download & Conversion
The raw bytes are downloaded from MyBrightDay along with the response's `Content-Type`. Attachments whose media type is not `image/*` (e.g. PDFs) are skipped before the image pipeline runs. For image attachments, the tool then ensures the data is in JPEG format, converting it if necessary.

### Metadata Injection (EXIF)
The tool injects the following into the JPEG's EXIF header:
*   **Original Timestamp**: Set to the `CaptureTime` from the MyBrightDay API, localized to the center's timezone.
*   **GPS Data**: Set to the geocoded coordinates of the daycare center.

### Storage
The processed photo is passed to each enabled storage backend's `Save` method. Each backend handles its own deduplication:

*   **Local**: Saves the processed JPEG to a date-partitioned directory structure (e.g., `photos/2024-01-15/daycare_...jpg`). Skips the file if it already exists.
*   **Google Photos**: Uploads the processed JPEG with a descriptive filename and adds it to the specified album. Skips items whose attachment ID was found in the pre-fetched deduplication set.
