# MyBrightDay Backup

A tool to automatically back up photos from the MyBrightDay application, with optional upload to Google Photos.

## Getting Started

### Download
Download the latest binary for your operating system from the [Releases](https://github.com/dipesh/mybrightday-backup/releases) page.

### Running Commands
Initialize authentication (MyBrightDay session and optionally Google Photos):
```bash
./mbdb init
./mbdb init --google-photos
```

Download photos (defaults to today):
```bash
./mbdb download
./mbdb download --date -7:0  # Last 7 days
```

### Configuration
Configuration is hierarchical and can be set via flags, environment variables, or a `config.yaml` file.

Key options:
- `--date`: Date or range (e.g., `YYYY-MM-DD`, `-1` for yesterday, `-7:0` for last 7 days).
- `--local-directory`: Where to save photos (default: `./photos/`).
- `--google-photos-enabled`: Enable uploading to Google Photos.
- `--dry-run`: Preview actions without downloading or uploading.

**Setting up Google Photos**
Run `init --google-photos` to authenticate. It will prompt for authorization and save your token. By default, it uploads to an album named "Daycare Photos" (customizable via `--google-photos-album-name`).

## Development
Build the binary locally using the Makefile:
```bash
make build
```

### [Advanced] Google Cloud Setup
The distributed binary includes default Google OAuth credentials. To use your own Google Cloud project during development:
1. Create a project in the [Google Cloud Console](https://console.cloud.google.com/).
2. Enable the **Google Photos Library API** and configure the OAuth consent screen with the below scopes:
   - `https://www.googleapis.com/auth/photoslibrary.appendonly`
   - `https://www.googleapis.com/auth/photoslibrary.readonly.appcreateddata`
3. Create an **OAuth 2.0 Client ID** (Desktop app) and download the JSON secret.
4. Provide the secret via the `google_photos.client_secret` config key (e.g., in `config.yaml` or via the `GOOGLE_PHOTOS_CLIENT_SECRET` environment variable).
