# Development Guide

This guide provides technical information for developers working on the MyBrightDay Backup project.

## Project Structure

*   `cmd/mbdb/`: The main entry point for the CLI application.
*   `internal/app/`: Core application logic, including authentication, configuration, and the backup workflow.
*   `internal/cmd/`: Implementation of the Cobra CLI commands.
*   `internal/credential/`: Handles obfuscated default Google OAuth credentials.
*   `pkg/config/`: A reusable reflection-based configuration library.
*   `docs/`: Project documentation.
*   `tools/obscure/`: A development utility for obfuscating secrets.

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

### Obfuscation Tool

The `tools/obscure` utility is used to generate the obfuscated values stored in `internal/credential/credential.go`.

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

After generating a new obfuscated value, update the `encryptedClientSecret` constant in `internal/credential/credential.go`.

## Code Conventions

*   **Error Handling**: Use the `%w` verb to wrap errors with context.
*   **Logging**: Use `slog` for structured logging.
*   **Configuration**: Add new configuration options to the structs in `internal/app/config.go`. Use struct tags to define YAML keys (`yaml:"key_name"`) and descriptions (`desc:"description"`).
