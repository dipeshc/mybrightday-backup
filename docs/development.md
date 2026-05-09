# Development Guide

This guide provides technical information for developers working on the MyBrightDay Backup project.

## Project Structure

```
mybrightday-backup/
├── cmd/mbdb/           # Binary entry point — delegates to internal/cmd
├── docs/               # Project documentation
├── internal/
│   ├── app/            # Root Config, Download orchestration, Version variable
│   ├── cmd/            # Cobra root command (the download action)
│   ├── config/         # Reflection-based config analysis, resolution, and YAML loading
│   ├── logging/        # Logging config and slog setup
│   ├── mybrightday/    # MyBrightDay auth flow, API client, and domain config
│   ├── processor/      # Image conversion (PNG→JPEG) and EXIF metadata injection
│   └── storage/
│       ├── storage.go          # Storage interface, Photo type, BaseConfig
│       ├── local/              # Local filesystem storage backend
│       └── googlephotos/       # Google Photos storage backend
│           ├── credential/     # Obfuscated default OAuth credentials
│           └── cmd.go          # "google-photos" cobra command group
├── tools/obscure/      # Dev utility: obfuscate/reveal credential values
└── Makefile
```

For a deeper explanation of how these packages interact, see [Architecture & Repo Structure](architecture.md).

## Building the Project

The project uses a `Makefile` to simplify common tasks.

### Prerequisites

*   Go 1.22+
*   `make`

### Commands

*   **Build**: Compile the binary.
    ```bash
    make build
    ```
    This will generate the `mbdb` binary in the root directory.
*   **Clean**: Remove build artifacts.
    ```bash
    make clean
    ```
*   **Format**: Format all Go files.
    ```bash
    make fmt
    ```
*   **Tidy**: Update and clean up `go.mod` and `go.sum`.
    ```bash
    make tidy
    ```

## Managing Default Credentials

The binary includes default Google OAuth credentials to simplify the getting started experience. To prevent these from appearing as plaintext in the compiled binary (deterring basic secret scanners), they are obfuscated.

The credential code lives in `internal/storage/googlephotos/credential/credential.go`.

### Obfuscation Tool

The `tools/obscure` utility is used to generate the obfuscated values stored in the credential package.

> **Note**: This is **NOT** cryptographic security. The AES key is hardcoded in the source code. It is purely for obfuscation to avoid plain-text detection.

#### Usage

To obfuscate a new client secret:
```bash
go run ./tools/obscure "YOUR_PLAINTEXT_SECRET"
```

To verify an existing obfuscated value:
```bash
go run ./tools/obscure -reveal "YOUR_OBFUSCATED_VALUE"
```

After generating a new obfuscated value, update the `encryptedClientSecret` constant in `internal/storage/googlephotos/credential/credential.go`.

## Code Conventions

*   **Error Handling**: Use the `%w` verb to wrap errors with context.
*   **Logging**: Use `slog` for structured logging throughout.
*   **Configuration**: Each domain package owns its config struct (e.g., `mybrightday.Config`, `local.Config`). The root `app.Config` in `internal/app/config.go` aggregates them. Use struct tags to define YAML keys (`yaml:"key_name"`) and flag descriptions (`desc:"description"`).
*   **Storage Backends**: New storage destinations should be added under `internal/storage/` as a new subdirectory. Implement the `storage.Storage` interface and embed `storage.BaseConfig` for the `Enabled` field. See [Architecture & Repo Structure](architecture.md) for the full pattern.
