# Configuration Reference

MyBrightDay Backup uses a hierarchical, reflection-based configuration system. This allows settings to be defined in a `config.yaml` file, overridden by environment variables, and finally overridden by CLI flags.

## Resolution Hierarchy

Configuration values are resolved in the following priority order:

1.  **CLI Flags**: Highest priority. (e.g., `--mybrightday-email`)
2.  **Environment Variables**: Upper-case version of the configuration key. (e.g., `MYBRIGHTDAY_EMAIL`)
3.  **Config Files Directory**: Useful for Kubernetes Secrets or Docker Compose. If `CONFIG_FILES_DIR` is set, the tool looks for files matching the key structure. (e.g., `MYBRIGHTDAY_PASSWORD` -> `$CONFIG_FILES_DIR/mybrightday/password`)
4.  **Configuration File**: YAML file (default: `config.yaml`).
5.  **Defaults**: Hardcoded default values in the application.

### Config Files Directory (Kubernetes/Docker)

For environments where configuration or secrets are mounted as individual files (like Kubernetes Secrets), you can set the `CONFIG_FILES_DIR` environment variable to point to the mount path.

The application automatically maps configuration keys to a directory structure. For example, if `CONFIG_FILES_DIR=/etc/configs`:
- `mybrightday.email` (key: `mybrightday_email`) -> `/etc/configs/mybrightday/email`
- `google_photos.token_secret` (key: `google_photos_token_secret`) -> `/etc/configs/google_photos/token_secret`

---

## Configuration Options

The following tables detail the available configuration options.

### General

| Flag | Env Var | YAML Key | Default | Description |
|------|---------|----------|---------|-------------|
| `--config` | `CONFIG` | — | `config.yaml` | Path to YAML configuration file |

### Logging

| Flag | Env Var | YAML Key | Default | Description |
|------|---------|----------|---------|-------------|
| `--logging.format` | `LOGGING_FORMAT` | `logging.format` | `text-simple` | Log output format (`text-simple`, `text-full`, or `json`) |
| `--logging.level` | `LOGGING_LEVEL` | `logging.level` | `INFO` | Log level (`DEBUG`, `INFO`, `WARN`, `ERROR`) |

### Download

| Flag | Env Var | YAML Key | Default | Description |
|------|---------|----------|---------|-------------|
| `--date` | `DATE` | `date` | `today` | Date or range to fetch. Accepts `YYYY-MM-DD`, relative offsets (`-1`, `+1`), or ranges (`-7:0`). |
| `--dry-run` | `DRY_RUN` | `dry_run` | `false` | Find and process images without saving or uploading |

### MyBrightDay

| Flag | Env Var | YAML Key | Default | Description |
|------|---------|----------|---------|-------------|
| `--mybrightday.email` | `MYBRIGHTDAY_EMAIL` | `mybrightday.email` | | MyBrightDay login email |
| `--mybrightday.password` | `MYBRIGHTDAY_PASSWORD` | `mybrightday.password` | | MyBrightDay login password |
| `--mybrightday.base-url` | `MYBRIGHTDAY_BASE_URL` | `mybrightday.base_url` | `https://mybrightday.brighthorizons.com` | MyBrightDay API base URL |

### Local Storage

| Flag | Env Var | YAML Key | Default | Description |
|------|---------|----------|---------|-------------|
| `--local.enabled` | `LOCAL_ENABLED` | `local.enabled` | `true` | Save photos to the local filesystem |
| `--local.directory` | `LOCAL_DIRECTORY` | `local.directory` | `./photos` | Directory to save photos in |

### Google Photos

| Flag | Env Var | YAML Key | Default | Description |
|------|---------|----------|---------|-------------|
| `--google-photos.enabled` | `GOOGLE_PHOTOS_ENABLED` | `google_photos.enabled` | `false` | Enable uploading to Google Photos |
| `--google-photos.token-secret` | `GOOGLE_PHOTOS_TOKEN_SECRET` | `google_photos.token_secret` | | OAuth2 token JSON (set via `google-photos-init`) |
| `--google-photos.client-secret` | `GOOGLE_PHOTOS_CLIENT_SECRET` | `google_photos.client_secret` | | Custom Google OAuth client secret JSON (overrides embedded credentials) |
| `--google-photos.album-name` | `GOOGLE_PHOTOS_ALBUM_NAME` | `google_photos.album_name` | `Daycare Photos` | Album to upload photos to |

### Location Override

| Flag | Env Var | YAML Key | Default | Description |
|------|---------|----------|---------|-------------|
| `--location-override.latitude` | `LOCATION_OVERRIDE_LATITUDE` | `location_override.latitude` | | Manual GPS latitude (overrides geocoded center location) |
| `--location-override.longitude` | `LOCATION_OVERRIDE_LONGITUDE` | `location_override.longitude` | | Manual GPS longitude (overrides geocoded center location) |
