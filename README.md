<p align="center">
  <img src="docs/assets/logo.svg" width="200" height="200" alt="MyBrightDay Backup Logo">
</p>

# MyBrightDay Backup

A tool to automatically backup photos from the Bright Horizons MyBrightDay application, with optional upload to Google Photos.

## Getting Started

### Automated Daily Backup (GitHub Actions) — Recommended

This repository includes a GitHub Actions workflow to automatically download your photos every day and back them up directly to Google Photos, requiring no local server. This is the easiest way to get started.

To set this up for your own account:
1.  **Fork this repository**: Click the "Fork" button at the top right of this page to create your own copy.
2.  **Get a Google Photos Refresh Token**: Download the latest binary for your operating system from the [Releases](https://github.com/dipeshc/mybrightday-backup/releases) page, then run the initialization command locally to authenticate and generate a refresh token.
    ```bash
    ./mbdb google-photos init
    cat ./config/google_photos/refresh_token
    ```
3.  **Add Repository Secrets**: Go to your forked repository's **Settings** > **Secrets and variables** > **Actions** and add the following repository secrets:
    *   `GOOGLE_PHOTOS_REFRESH_TOKEN`: The content of the refresh token file generated in step 2.
    *   `MBD_STATE_KEY`: A random key that encrypts your MyBrightDay login between runs. Generate one with `openssl rand -hex 32` and paste the output exactly. It must be random, not a passphrase you made up: the encrypted login is stored in the repository's Actions cache, which anyone able to run a pull-request workflow in your fork can read, and only a strong key keeps it safe there.
4.  **Enable the Workflows**: Go to the **Actions** tab in your fork and enable both the "Daily Backup" and "Keep Alive" workflows. Keep Alive runs weekly and exists only to stop GitHub auto-disabling the Daily Backup after 60 days of repository inactivity.
5.  **Log in to MyBrightDay (two runs)**: Open "Daily Backup", click **Run workflow**, and run it with the boxes empty. It will stop with a message in the job summary containing a login link. Open the link, log in, and you will land on a page that says "This site can't be reached" — that is expected. Copy the whole address from the address bar (it starts with `http://localhost:4200/okta/callback?code=`), run the workflow again with that address pasted into the **callback_url** box, and that run does your first backup. The link is valid for ten minutes.

That's it. Your login is kept encrypted in the repository's Actions cache and renewed on every run. If it ever expires or is lost, the next run shows the same login message and you repeat step 5. Only run the workflow from your default branch, and only paste an address you landed on yourself.

### Running Manually

Download the latest binary for your operating system from the [Releases](https://github.com/dipeshc/mybrightday-backup/releases) page.

1.  **Log in to MyBrightDay** (once):
    ```bash
    ./mbdb login
    ```
    This opens your browser, waits for you to log in, and saves a token under `./config/mybrightday/`. The token is renewed automatically every time you run a backup. Use a separate login on each machine you run `mbdb` from — a token used from two places is revoked by Bright Horizons.

1.  **Download Photos**:
    Download photos for a specific date or range.
    ```bash
    ./mbdb --date=2026-06-15
    ```

    You can also specify date ranges or relative offsets:
    ```bash
    ./mbdb --date -7:0                   # Last 7 days
    ./mbdb --date 2024-01-01:2024-01-31  # Entire month
    ./mbdb                               # Defaults to today
    ```

1.  **Upload to Google Photos** (Optional):
    If you want to upload photos to Google Photos, run the initialization command once to authenticate.
    ```bash
    ./mbdb google-photos init
    ```

    Then run the download with Google Photos upload enabled.
    ```bash
    ./mbdb --googlephotos.enabled
    ```

## Configuration Overview

Configuration can be set via CLI flags, environment variables, or a `config.yaml` file. The application follows a strict resolution hierarchy: **CLI Flags → Env Vars → Secret Files → Config File → Defaults**.

Commonly used options:
- `--date`: Date or range (e.g., `YYYY-MM-DD`, `-1`, `-7:0`).
- `--local.directory`: Where to save photos locally (default: `./photos`).
- `--googlephotos.enabled`: Enable uploading to Google Photos.
- `--dryrun`: Preview actions without downloading or uploading.

For a full list of options and details on the configuration system, see the [Configuration Reference](docs/configuration.md).

## Troubleshooting & Known Issues

* **Google Photos Unverified App Warning**: By default, the application uses an embedded Google Cloud project for OAuth. Because this project is not officially verified by Google, you will see a warning screen saying "Google hasn't verified this app" during the `google-photos init` step. You can safely click "Advanced" and "Go to MyBrightDay Backup (unsafe)" to proceed. Alternatively, you can configure your own Custom Google Cloud Project (see the [Google Photos Guide](docs/google-photos.md)).
* **"MyBrightDay rejected the stored token"**: The login token expired or was used from another machine. The tool clears it. Locally, run `./mbdb login` again; on GitHub Actions, the same run writes the two-run login message to its job summary.
* **Login message after a failed run**: If a run failed partway for an unrelated reason and the next one asks you to log in again, the previous good token may still be in an older cache entry. Under **Actions** > **Caches**, delete the newest `mbd-state-*` entries created after the last successful run, then run the workflow again before logging in afresh.
* **"The login link has expired"**: Login links last ten minutes. Start again to get a fresh one (re-run the workflow with the `callback_url` box empty, or run `./mbdb login` again).
* **"MyBrightDay login needs port 4200"**: Bright Horizons only allows the login to return to `localhost:4200`, so the port cannot be changed. Free the port, or follow the on-screen instructions to finish the login by pasting the address you land on.
* **Why a password login no longer works**: Bright Horizons' login provider shows a CAPTCHA to anything that is not a real browser, which is why the tool now logs in through your browser once and keeps a renewable token instead of your password.

---

## Documentation

For more detailed information, please refer to the following guides:

*   [**Configuration Reference**](docs/configuration.md): Full list of all flags, environment variables, and YAML keys.
*   [**Google Photos Integration**](docs/google-photos.md): Detailed setup guide and explanation of the deduplication strategy.
*   [**Backup Workflow**](docs/backup-workflow.md): A step-by-step look at how the application fetches and processes data.
*   [**Metadata & Image Processing**](docs/metadata-and-processing.md): Information on how EXIF data (GPS and timestamps) is generated and injected.
*   [**Architecture & Repo Structure**](docs/architecture.md): Codebase layout, storage module pattern, and the config system.
*   [**Development Guide**](docs/development.md): Instructions for building the project and contributing code.
