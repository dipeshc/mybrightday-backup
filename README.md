# Daycare Photos

A Go-based tool that automatically downloads daycare photos from Gmail "Daily Report" emails and uploads them to a Google Photos album.

## Getting Started

### 1. Download
Download the latest binary for your OS from the [Releases](https://github.com/dipesh/daycare-photos/releases) page.

### 2. Configuration & Authentication
Initialize the application by running the `init` command:

```bash
./daycare-photos init
```

This will:
1. Create a default `config.yaml` in your current directory (if it doesn't exist).
2. Open your browser to authorize the application with your Google account.

Once authenticated, edit the `config.yaml` with your Gmail search parameters and photo metadata preferences.

### 3. Run
On subsequent runs, you can execute the tool directly:

```bash
# Full run with output
./daycare-photos --verbose

# Quiet run
./daycare-photos
```

## Development
Compile the binary using the provided Makefile:

```bash
make
```

## Google Cloud Setup
The distributed binary ships with default Google OAuth credentials. To use your own Google Cloud project instead, point `auth.client_secret_file` in `config.yaml` at a standard Google OAuth JSON file:

```yaml
auth:
  client_secret_file: /path/to/client_secret.json
```

To obtain the JSON:

1. Create a project in the [Google Cloud Console](https://console.cloud.google.com/).
2. Enable the **Gmail API** and **Photos Library API**.
3. Configure the **OAuth consent screen** (External) and add yourself as a test user.
4. Add scopes:
   - `https://www.googleapis.com/auth/gmail.readonly`
   - `https://www.googleapis.com/auth/photoslibrary.appendonly`
   - `https://www.googleapis.com/auth/photoslibrary.readonly.appcreateddata`
5. Create an **OAuth 2.0 Client ID** (Desktop app) and download the JSON.
