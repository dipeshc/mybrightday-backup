# Configuration Reference

MyBrightDay Backup uses a hierarchical, reflection-based configuration system. This allows settings to be defined in a `config.yaml` file, overridden by environment variables, and finally overridden by CLI flags.

## Resolution Hierarchy

Configuration values are resolved in the following priority order:

1.  **CLI Flags**: Highest priority. (e.g., `--mybrightday.email`)
2.  **Environment Variables**: Upper-case version of the configuration key. (e.g., `MYBRIGHTDAY_EMAIL`)
3.  **Config Files Directory**: The tool looks for files at `./config/<section>/<key>` by default. Override the base directory by setting `CONFIG_FILES_DIR`. (e.g., `mybrightday.password` -> `./config/mybrightday/password`)
4.  **Configuration File**: YAML file (default: `config.yaml`).
5.  **Defaults**: Hardcoded default values in the application.

### Config Files Directory

The config files directory is always active. It defaults to `./config/` relative to the working directory and can be overridden with the `CONFIG_FILES_DIR` environment variable.

Configuration keys are mapped to files by converting the nested YAML structure into a directory path — the section name is preserved as-is, and each nesting level is a subdirectory:

| YAML key | Config file path |
|---|---|
| `mybrightday.email` | `./config/mybrightday/email` |
| `mybrightday.password` | `./config/mybrightday/password` |
| `google_photos.refresh_token` | `./config/google_photos/refresh_token` |

The `./config/` directory is listed in `.gitignore` to prevent secrets from being accidentally committed.

To use a different directory (e.g. a Kubernetes Secret mount path):

```bash
CONFIG_FILES_DIR=/etc/secrets ./mbdb --googlephotos.enabled
```

In this case the tool would read `google_photos.refresh_token` from `/etc/secrets/google_photos/refresh_token`.

---

## Configuration Options

The following tables detail the available configuration options.

> **Flag name convention**: nested config keys use dot notation and underscores are removed.
> For example, `google_photos.refresh_token` becomes `--googlephotos.refreshtoken`.

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
| `--dryrun` | `DRY_RUN` | `dry_run` | `false` | Find and process images without saving or uploading |

### MyBrightDay

| Flag | Env Var | YAML Key | Default | Description |
|------|---------|----------|---------|-------------|
| `--mybrightday.email` | `MYBRIGHTDAY_EMAIL` | `mybrightday.email` | | MyBrightDay login email |
| `--mybrightday.password` | `MYBRIGHTDAY_PASSWORD` | `mybrightday.password` | | MyBrightDay login password |
| `--mybrightday.baseurl` | `MYBRIGHTDAY_BASE_URL` | `mybrightday.base_url` | `https://mybrightday.brighthorizons.com` | MyBrightDay API base URL |

### Local Storage

| Flag | Env Var | YAML Key | Default | Description |
|------|---------|----------|---------|-------------|
| `--local.enabled` | `LOCAL_ENABLED` | `local.enabled` | `true` | Save photos to the local filesystem |
| `--local.directory` | `LOCAL_DIRECTORY` | `local.directory` | `./photos` | Directory to save photos in |

### Google Photos

| Flag | Env Var | YAML Key | Default | Description |
|------|---------|----------|---------|-------------|
| `--googlephotos.enabled` | `GOOGLE_PHOTOS_ENABLED` | `google_photos.enabled` | `false` | Enable uploading to Google Photos |
| `--googlephotos.refreshtoken` | `GOOGLE_PHOTOS_REFRESH_TOKEN` | `google_photos.refresh_token` | | OAuth2 refresh token (set via `mbdb google-photos init`) |
| `--googlephotos.clientsecret` | `GOOGLE_PHOTOS_CLIENT_SECRET` | `google_photos.client_secret` | | Custom Google OAuth client secret JSON (overrides embedded credentials) |
| `--googlephotos.albumname` | `GOOGLE_PHOTOS_ALBUM_NAME` | `google_photos.album_name` | `Daycare Photos` | Album to upload photos to |

### Location Override

| Flag | Env Var | YAML Key | Default | Description |
|------|---------|----------|---------|-------------|
| `--locationoverride.latitude` | `LOCATION_OVERRIDE_LATITUDE` | `location_override.latitude` | | Manual GPS latitude (overrides geocoded center location) |
| `--locationoverride.longitude` | `LOCATION_OVERRIDE_LONGITUDE` | `location_override.longitude` | | Manual GPS longitude (overrides geocoded center location) |
