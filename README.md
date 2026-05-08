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

## Configuration Reference

Configuration is resolved in priority order: **CLI flag → env var → `FILE__` env var → local file → config file**.

For any env var `FOO`, setting `FILE__FOO` to a file path reads the value from that file (useful for Docker secrets and CI). As a final fallback the env var name lowercased (e.g. `mybrightday_session_cookie_secret`) is tried as a local file path.

The flags below apply to the `download` and `config` commands. The `init` command accepts the `--config`, `--logging-*`, `--mybrightday-*`, and `--google-photos-*` flags. The `version` command accepts `--config` and `--logging-*` flags.

### Config file

| Flag | Env Var | Config Key | Default | Description |
|------|---------|------------|---------|-------------|
| `--config` | `CONFIG` | — | `config.yaml` | Path to YAML configuration file |

### Logging

| Flag | Env Var | Config Key | Default | Description |
|------|---------|------------|---------|-------------|
| `--logging-format` | `LOGGING_FORMAT` | `logging.format` | `text` | Log output format (`text` or `json`) |
| `--logging-verbose` | `LOGGING_VERBOSE` | `logging.verbose` | `false` | Enable verbose logging |

### Download

| Flag | Env Var | Config Key | Default | Description |
|------|---------|------------|---------|-------------|
| `--date` | `DATE` | `date` | today | Date or range to fetch. Accepts `YYYY-MM-DD`, relative offsets (`-1`, `+1`), or ranges (`-7:0`). |
| `--dry-run` | `DRY_RUN` | `dry_run` | `false` | Find and process images without saving or uploading |

### MyBrightDay

| Flag | Env Var | Config Key | Default | Description |
|------|---------|------------|---------|-------------|
| `--mybrightday-session-cookie-secret` | `MYBRIGHTDAY_SESSION_COOKIE_SECRET` | `mybrightday.session_cookie_secret` | | Authenticated session cookie (set via `init`) |
| `--mybrightday-base-url` | `MYBRIGHTDAY_BASE_URL` | `mybrightday.base_url` | `https://mybrightday.brighthorizons.com` | MyBrightDay API base URL |

### Local storage

| Flag | Env Var | Config Key | Default | Description |
|------|---------|------------|---------|-------------|
| `--local-enabled` | `LOCAL_ENABLED` | `local.enabled` | `true` | Save photos to the local filesystem |
| `--local-directory` | `LOCAL_DIRECTORY` | `local.directory` | `./photos/` | Directory to save photos in |

### Google Photos

| Flag | Env Var | Config Key | Default | Description |
|------|---------|------------|---------|-------------|
| `--google-photos-enabled` | `GOOGLE_PHOTOS_ENABLED` | `google_photos.enabled` | `false` | Enable uploading to Google Photos |
| `--google-photos-token-secret` | `GOOGLE_PHOTOS_TOKEN_SECRET` | `google_photos.token_secret` | | OAuth2 token JSON (set via `init --google-photos`) |
| `--google-photos-client-secret` | `GOOGLE_PHOTOS_CLIENT_SECRET` | `google_photos.client_secret` | | Custom Google OAuth client secret JSON (overrides embedded credentials) |
| `--google-photos-album-name` | `GOOGLE_PHOTOS_ALBUM_NAME` | `google_photos.album_name` | `Daycare Photos` | Album to upload photos to |

### Location override

| Flag | Env Var | Config Key | Default | Description |
|------|---------|------------|---------|-------------|
| `--location-override-latitude` | `LOCATION_OVERRIDE_LATITUDE` | `location_override.latitude` | | Manual GPS latitude (overrides geocoded centre location) |
| `--location-override-longitude` | `LOCATION_OVERRIDE_LONGITUDE` | `location_override.longitude` | | Manual GPS longitude (overrides geocoded centre location) |

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
