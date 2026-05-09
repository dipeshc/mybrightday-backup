# Architecture & Repo Structure

This document explains the design of the codebase — how it is organised, why, and how the key patterns work. It is intended for anyone who wants to understand or extend the project.

---

## Repo Layout

```
mybrightday-backup/
├── cmd/mbdb/                    # Binary entry point
├── docs/                        # Project documentation
├── internal/
│   ├── app/                     # Orchestration, root Config, Version
│   ├── cmd/                     # Cobra root command (the download action)
│   ├── config/                  # Config analysis, resolution, and YAML loading
│   ├── logging/                 # Logging config and slog initialisation
│   ├── mybrightday/             # MyBrightDay auth flow, API client, domain config
│   ├── processor/               # Image conversion (PNG→JPEG) and EXIF injection
│   └── storage/
│       ├── storage.go           # Storage interface, Photo type, BaseConfig
│       ├── local/               # Local filesystem storage backend
│       └── googlephotos/        # Google Photos storage backend
│           └── credential/      # Obfuscated default Google OAuth credentials
├── tools/obscure/               # Dev utility: obfuscate/reveal credential values
└── Makefile
```

The code is organised by **domain** rather than by operation type. Each package owns its config struct, its business logic, and — where relevant — its CLI command.

---

## Package Responsibilities

### `cmd/mbdb`
Single `main.go` that calls `cmd.Execute()`. No logic lives here.

### `internal/cmd`
A single `root.go` file. The root Cobra command **is** the download action — running `mbdb` downloads photos. It also attaches the `google-photos` subcommand tree. The `--version` flag is provided by Cobra's built-in `RootCmd.Version` field.

### `internal/app`
The application entry-points and their aggregated configuration:

- **`config.go`**: The root `Config` struct that composes sub-configs from each domain package. Also contains `NewDefaultConfig()`, which sets all default values, and `Resolve()`, which applies the hierarchical flag/env/file resolution.
- **`app.go`**: The `Download()` function — the main orchestration loop. It authenticates, builds the list of enabled storage backends, fetches media, processes each photo, and calls `Save()` on every backend.
- **`version.go`**: A single `Version` variable injected at build time via `-ldflags`.

### `internal/config`
A reusable reflection-based configuration library with no dependency on application logic:

- **`analyze.go`**: Traverses a config struct using reflection and `yaml`/`desc` struct tags to produce a flat list of `ConfigField` records (name, env var, flag name, type, default).
- **`resolve.go`**: Applies hierarchical resolution over a struct — CLI flags take highest priority, then env vars, then files in `CONFIG_FILES_DIR`, then the existing value.
- **`load.go`**: Reads and unmarshals a YAML config file into any target struct.

### `internal/logging`
Owns the `Config` struct (format and level) and the `Setup(cfg Config)` function that initialises the global `slog` logger.

### `internal/mybrightday`
Everything specific to the MyBrightDay service:

- **`config.go`**: `Config` struct with email, password, base URL.
- **`auth.go`**: The 6-stage authentication flow that exchanges credentials for a session cookie (Auth0/OIDC with PKCE → FIC JWT → MBD token → session).
- **`client.go`**: The `Client` struct and its methods for fetching dependents, center info, geocoding, media lists, and downloading individual photos. Also `FormatOffset()` for timezone formatting.

### `internal/processor`
Stateless image processing functions, independent of all other application packages:

- **`ConvertToJPEG(data []byte)`**: Detects image format by magic bytes and converts to JPEG if needed.
- **`AddEXIF(jpegData []byte, meta PhotoMeta)`**: Injects EXIF tags (timestamps, timezone offset, GPS coordinates, camera make/model) using the `go-exif` library.

### `internal/storage`
Defines the contract all storage backends must satisfy.

---

## The Storage Module Pattern

Storage backends are the primary extension point of the codebase. Adding a new destination (e.g., Dropbox, S3) follows a consistent pattern.

### The Interface

```go
// internal/storage/storage.go

type Storage interface {
    Save(ctx context.Context, photo Photo) error
}
```

`Save` is called once per photo after it has been downloaded and processed. Each backend is fully responsible for its own deduplication and dry-run behaviour — the orchestration layer in `app.Download()` just iterates the backends and calls `Save`.

### The Photo Type

```go
type Photo struct {
    AttachmentID string    // MyBrightDay attachment ID, used for deduplication
    Filename     string    // e.g. "daycare_2024-12-20_<id>.jpg"
    Data         []byte    // Processed JPEG with EXIF already injected
    CaptureTime  time.Time // Capture time in the daycare center's local timezone
}
```

`CaptureTime` is in the center's local timezone (not UTC) so backends can use it directly for date-based directory naming or date filtering without needing to know the timezone.

### BaseConfig

```go
type BaseConfig struct {
    Enabled bool `yaml:"enabled" desc:"Enable this storage destination"`
}
```

Every backend's config struct embeds `BaseConfig` using a YAML inline tag:

```go
type Config struct {
    storage.BaseConfig `yaml:",inline"`
    Directory string `yaml:"directory" desc:"..."`
}
```

The inline tag means `enabled` appears at the same YAML level as the other fields:
```yaml
local:
  enabled: true
  directory: ./photos
```

### Adding a New Backend

1.  Create `internal/storage/<name>/` with at minimum:
    - `config.go`: a `Config` struct embedding `storage.BaseConfig`
    - `storage.go`: a struct implementing `Storage` with a `New(cfg Config, dryRun bool) *<Name>Storage` constructor and a `Save` method
2.  In `internal/app/config.go`, add a field to `Config`:
    ```go
    MyNewBackend newbackend.Config `yaml:"my_new_backend"`
    ```
    and set a sensible default in `NewDefaultConfig()`.
3.  In `internal/app/app.go`, append to the `stores` slice:
    ```go
    if cfg.MyNewBackend.Enabled {
        stores = append(stores, newbackend.New(cfg.MyNewBackend, cfg.DryRun))
    }
    ```

If the backend needs its own CLI subcommand (like `google-photos init`), add a `cmd.go` to the package that exports a `Command() *cobra.Command` function, then call `RootCmd.AddCommand(newbackend.Command())` in `internal/cmd/root.go`.

---

## The Config System

### Resolution Order

For any configuration value, the priority is:

```
CLI flag  >  Env var  >  config/<section>/<key>  >  config.yaml  >  default
```

The config files directory defaults to `./config/` and can be overridden with `CONFIG_FILES_DIR`. The path is derived by preserving the YAML section name and using `/` as the nesting separator — `google_photos.token_secret` maps to `./config/google_photos/token_secret`. This is implemented in `internal/config/resolve.go:ResolveValue`.

### How Defaults Work

Defaults are set in `app.NewDefaultConfig()` **before** YAML loading. Because `yaml.Unmarshal` only writes fields that are present in the YAML document, absent keys retain their constructor values. There is no separate `ApplyDefaults` pass.

```go
func NewDefaultConfig() *Config {
    return &Config{
        Local: local.Config{
            BaseConfig: storage.BaseConfig{Enabled: true},  // on by default
            Directory:  "./photos",
        },
        ...
    }
}
```

### How Flags Are Generated

Rather than hand-coding every flag, the commands use `config.Analyze()` to introspect the config struct via reflection and produce a flat list of metadata records. The command's `init()` function iterates this list and registers a cobra flag for each field.

Flag names are derived from YAML tag names with underscores removed and dots as separators for nesting:

| YAML key | Env var | Config file path | Flag |
|----------|---------|------------------|------|
| `google_photos.token_secret` | `GOOGLE_PHOTOS_TOKEN_SECRET` | `config/google_photos/token_secret` | `--googlephotos.tokensecret` |
| `local.enabled` | `LOCAL_ENABLED` | `config/local/enabled` | `--local.enabled` |
| `dry_run` | `DRY_RUN` | `config/dry_run` | `--dryrun` |

---

## CLI Structure

```
mbdb                     # Downloads photos (the root command IS the action)
mbdb --version           # Prints the version (Cobra built-in)
mbdb google-photos       # Parent command for Google Photos management
mbdb google-photos init  # Runs the interactive OAuth2 flow and saves the token
```

The `google-photos` command tree is defined and exported entirely within the `internal/storage/googlephotos` package (`cmd.go`). The root command simply calls `AddCommand(googlephotos.Command())`. This keeps CLI concerns co-located with the feature they belong to.

---

## Dependency Graph (simplified)

```
cmd/mbdb
  └── internal/cmd          (cobra root)
        ├── internal/app    (orchestration + config)
        │     ├── internal/mybrightday
        │     ├── internal/processor
        │     └── internal/storage/{local,googlephotos}
        │           └── internal/storage/googlephotos/credential
        └── internal/storage/googlephotos  (for Command())

internal/config  ← used by internal/app and internal/storage/googlephotos
internal/logging ← used by internal/app and internal/storage/googlephotos
```

No package outside `internal/` imports anything from `internal/` — this is enforced by Go's `internal` package visibility rules.
