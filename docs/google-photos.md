# Google Photos Integration

The MyBrightDay Backup tool can automatically upload your downloaded photos to Google Photos. This guide covers how the integration works, how to set it up, and how the tool avoids duplicate uploads.

## How it Works

When Google Photos integration is enabled, the tool performs the following steps for each photo:
1.  **Deduplication Check**: Before uploading, it checks if the photo has already been uploaded by this application.
2.  **Upload**: If the photo is new, it is uploaded to Google's servers.
3.  **Album Creation/Assignment**: The uploaded photo is added to a specific album (default: "Daycare Photos").

### Deduplication Strategy

To prevent redundant uploads and save bandwidth, the tool uses a filename-based deduplication strategy.

1.  **Unique Filenames**: Every photo downloaded from MyBrightDay is saved with a filename that includes its unique MyBrightDay Attachment ID (a 24-character hex MongoDB ObjectID).
    *   Example: `daycare_2024-01-15_69f9390f8d9c1412adacc127.jpg`
2.  **Library Search**: Before processing a date range, the tool queries your Google Photos library for items uploaded by this application within that date range (plus a 2-day buffer for timezone safety).
3.  **ID Extraction**: It parses the filenames of existing items in your library using a regular expression to extract the Attachment IDs.
4.  **Skip Existing**: Any photo whose Attachment ID is already found in the library is skipped.

> **Note**: This deduplication only works for photos uploaded *by this application*, as it relies on the specific filename format and the `photoslibrary.readonly.appcreateddata` scope.

---

## Authentication & Scopes

The application uses OAuth 2.0 to interact with the Google Photos API. It requests the following minimal scopes:

*   `photoslibrary.appendonly`: Allows the app to upload photos and create media items/albums.
*   `photoslibrary.readonly.appcreateddata`: Allows the app to see and search for media items that *it* created.

The OAuth flow is split in two:

*   **One-time setup** (`mbdb google-photos init`) drives the interactive browser flow with `access_type=offline` and `prompt=consent`, then persists **only** the long-lived refresh token to disk.
*   **Each run** uses the refresh token to mint a short-lived access token in memory. The access token is never written to disk.

### Initial Setup

To authenticate the application with your Google account, run the following command:

```bash
./mbdb google-photos init
```

This will:
1.  Open your web browser to the Google authorization page.
    > **Note**: Because the default built-in project is not officially verified by Google, you will see a warning that "Google hasn't verified this app". This is expected. Click **Advanced** and then **Go to MyBrightDay Backup (unsafe)** to continue.
2.  Prompt you to grant the required permissions (the consent screen is always shown so a refresh token is always issued).
3.  Receive an authorization code via a temporary local server.
4.  Exchange the code for an OAuth2 token, extract the refresh token, and save it to `./config/google_photos/refresh_token` as a plain string.

The refresh token file is automatically picked up on subsequent runs — no additional configuration is needed. The `./config/` directory is gitignored so the token is never accidentally committed.

> **Refresh token expiry**: For the refresh token to remain valid indefinitely, the OAuth consent screen for the Google Cloud project must be in **In production** status. Refresh tokens issued by projects in **Testing** mode expire after 7 days and the init flow must be re-run.

---

## [Advanced] Custom Google Cloud Project

By default, the distributed binary includes embedded credentials for a default Google Cloud project. If you wish to use your own project (e.g., for development or to avoid shared rate limits):

1.  **Create a Project**: Go to the [Google Cloud Console](https://console.cloud.google.com/).
2.  **Enable API**: Enable the **Photos Library API** for your project.
3.  **Configure OAuth Consent**: Set up the OAuth consent screen (Internal or External).
4.  **Create Credentials**:
    *   Go to **Credentials** -> **Create Credentials** -> **OAuth 2.0 Client ID**.
    *   Select **Desktop app** as the application type.
5.  **Download JSON**: Download the client secret JSON file.
6.  **Provide to Tool**: Provide the JSON content to the tool using the `--googlephotos.clientsecret` flag or the `GOOGLE_PHOTOS_CLIENT_SECRET` environment variable.

```bash
./mbdb --googlephotos.clientsecret "$(cat client_secret.json)"
```
