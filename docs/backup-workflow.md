# Backup Workflow

This document explains the internal lifecycle of the `download` command and how the application orchestrates the backup process.

## Process Overview

The backup process follows a linear execution flow:

1.  **Authentication**: Authenticates with MyBrightDay and optionally Google Photos.
2.  **Date Resolution**: Parses the requested date or range.
3.  **Context Discovery**: Fetches child (dependent) and daycare center information.
4.  **Metadata Preparation**: Resolves the center's timezone and GPS coordinates.
5.  **Media Discovery**: Fetches a list of all media attachments for the resolved date range.
6.  **Processing Loop**: Iterates through each media item to download, process, and save/upload.

---

## 1. Authentication

The tool first logs into MyBrightDay using the provided email and password. This involves a multi-stage flow to exchange Auth0 credentials for a MyBrightDay session cookie.

If Google Photos is enabled, it also initializes an OAuth2 client. If a token is missing, it will prompt the user to run the `init` command.

## 2. Date Resolution

The `--date` flag is flexible. It can be:
*   **Absolute**: `2024-01-15`
*   **Relative**: `-1` (yesterday), `0` (today), `+1` (tomorrow)
*   **Range**: `-7:0` (last 7 days), `2024-01-01:2024-01-31`

The tool parses these strings into start and end `time.Time` objects.

## 3. Context Discovery

The tool fetches the list of "dependents" (children) associated with the account. For each child, it identifies the daycare center they are enrolled in.

It then fetches detailed information about the center, including its physical address and its timezone (e.g., `America/New_York`).

## 4. Metadata Preparation

Before downloading media, the tool prepares the metadata that will be injected into the photos:
*   **Timezone**: Used to correctly localize the capture timestamps.
*   **GPS Coordinates**: The center's address is geocoded using the Nominatim API to get latitude and longitude.

## 5. Media Discovery

The tool calls the MyBrightDay API for each child and date in the range. It looks for "daily reports" and extracts all "attachments" (photos).

Each attachment has a unique `AttachmentID` and a `CaptureTime`.

## 6. Processing Loop

For each discovered attachment, the tool performs the following:

### Deduplication
It checks if the file already exists locally (in the `--local-directory`) or in Google Photos (by searching for the `AttachmentID` in existing filenames). If it exists, the photo is skipped.

### Download & Conversion
The raw image data is downloaded from MyBrightDay. The tool ensures the image is in JPEG format, converting it if necessary.

### Metadata Injection (EXIF)
The tool injects the following into the JPEG's EXIF header:
*   **Original Timestamp**: Set to the `CaptureTime` from the MyBrightDay API, localized to the center's timezone.
*   **GPS Data**: Set to the geocoded coordinates of the daycare center.

### Storage
*   **Local**: Saves the processed JPEG to a date-partitioned directory structure (e.g., `photos/2024-01-15/daycare_...jpg`).
*   **Google Photos**: Uploads the processed JPEG with a descriptive filename and adds it to the specified album.
