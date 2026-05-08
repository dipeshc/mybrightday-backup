# MyBrightDay Backup

A tool to automatically backup photos from the MyBrightDay application, with optional upload to Google Photos.

## Getting Started

### Download
Download the latest binary for your operating system from the [Releases](https://github.com/dipeshc/mybrightday-backup/releases) page.

### Running Commands

1.  **Download Photos**:
    Download photos for a specific date or range. You must provide your MyBrightDay login information.
    ```bash
    ./mbdb download --mybrightday.email "user@example.com" --mybrightday.password "secret" --date=2026-06-15
    ```

    You can also specify date ranges or relative offsets:
    ```bash
    ./mbdb download --date -7:0                   # Last 7 days
    ./mbdb download --date 2024-01-01:2024-01-31  # Entire month
    ./mbdb download                               # Defaults to today
    ```

1.  **Upload to Google Photos** (Optional):
    If you want to upload photos to Google Photos, run the initialization command to authenticate just once.
    ```bash
    ./mbdb google-photos-init
    ```

    Then run the download command with Google Photos upload enabled.
    ./mbdb download --googlephotos.enabled
    ```

## Configuration Overview

Configuration can be set via CLI flags, environment variables, or a `config.yaml` file. The application follows a strict resolution hierarchy: **CLI Flags → Env Vars → Secret Files → Config File → Defaults**.

Commonly used options:
- `--date`: Date or range (e.g., `YYYY-MM-DD`, `-1`, `-7:0`).
- `--local.directory`: Where to save photos locally (default: `./photos`).
- `--google-photos.enabled`: Enable uploading to Google Photos.
- `--dry-run`: Preview actions without downloading or uploading.

For a full list of options and details on the configuration system, see the [Configuration Reference](docs/configuration.md).

---

## Documentation

For more detailed information, please refer to the following guides:

*   [**Configuration Reference**](docs/configuration.md): Full list of all flags, environment variables, and YAML keys.
*   [**Google Photos Integration**](docs/google-photos.md): Detailed setup guide and explanation of the deduplication strategy.
*   [**Backup Workflow**](docs/backup-workflow.md): A step-by-step look at how the application fetches and processes data.
*   [**Metadata & Image Processing**](docs/metadata-and-processing.md): Information on how EXIF data (GPS and timestamps) is generated and injected.
*   [**Development Guide**](docs/development.md): Instructions for building the project and contributing code.
