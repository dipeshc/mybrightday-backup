# MyBrightDay Authentication Flow Analysis

This document details the reverse-engineered authentication flow for MyBrightDay, specifically how to programmatically obtain a `session` cookie starting from a user's credentials.

## Overview
The authentication is a multi-stage process involving Auth0 (OIDC with PKCE), the Bright Horizons Family Info Center (FIC), and finally a token exchange with MyBrightDay (Tadpoles).

> **November 2026 update — bot detection.** Auth0 Bot Detection on the Bright Horizons tenant now injects a CAPTCHA (`auth0_v2`, a Cloudflare Turnstile widget) into the Universal Login form for requests it scores as risky, which includes every headless client we tried (GitHub-hosted runners and a residential IP after a handful of attempts). A form posted without a solved challenge is simply re-rendered — the identifier step answers 400 and the password step answers 200 with no `Location` — so the credential flow in stage 1 below can no longer be driven programmatically. The tool therefore logs in once through a real browser and keeps a **refresh token**, described in the "Refresh-token flow" section at the end. Stages 2–4 are unchanged.

## Authentication Stages

### 1. Auth0/Okta Authentication (OIDC with PKCE)
**Base Domain**: `bhloginsso.brighthorizons.com`
**Client ID**: `5VIzhuWNKxFc9etVvp5fonr2tlbBEZae`
**Redirect URI**: `https://familyinfocenter.brighthorizons.com/okta/callback`

**Flow**:
1.  **Authorize**: `GET /authorize` with OIDC parameters (`response_type=code`, `response_mode=query`, `scope=openid+offline_access+profile+email`), a generated `nonce`, `state`, and a PKCE `code_challenge` (method S256).
2.  **Identifier**: `POST /u/login/identifier` with the user's `username` (email), `action=default`, and the Auth0 `state`.
3.  **Password**: `POST /u/login/password` with the user's `username`, `password`, `action=default`, and the Auth0 `state`.
4.  **Callback**: Intercept the 302 redirect to the `Redirect URI` to capture the authorization `code`.
5.  **Token Exchange**: `POST /oauth/token` with the `code`, `client_id`, `redirect_uri`, `grant_type=authorization_code`, and PKCE `code_verifier` to obtain the FIC Access Token (JWT).

### 2. Family Info Center (FIC) Landing
**Status**: Once authenticated via Auth0, the user has an Access Token.
**Key Header**: `Authorization: Bearer <FIC_JWT>`
- This JWT is used by the FIC to communicate with `mbdwgateway.brighthorizons.com`.

### 3. MyBrightDay Token Acquisition (The "Exchange")
The FIC Access Token is exchanged for a MyBrightDay-specific JWT at the MBDW gateway.

**Request**:
- **Method**: `GET`
- **URL**: `https://mbdwgateway.brighthorizons.com/api/account/mbdtoken`
- **Headers**:
    - `Authorization: Bearer <FIC_JWT>`
    - `Accept: application/json, text/plain, */*`
    - `Origin: https://familyinfocenter.brighthorizons.com`
    - `Referer: https://familyinfocenter.brighthorizons.com/`

**Response**:
- **Status**: `200 OK`
- **Body**: `{"token": "<MBD_JWT>"}`

### 4. MyBrightDay Session Establishment
The `<MBD_JWT>` obtained in the previous step is used to establish a session on the MyBrightDay domain.

**Request**:
- **Method**: `GET`
- **URL**: `https://mybrightday.brighthorizons.com/auth/jwt/redirect?jwt=<MBD_JWT>`
- **Action**: This endpoint returns a `302 Found` and sets the `session` cookie.

**Final Cookie**: `session=<cookie_value>`
- This cookie is required for all subsequent API calls to `https://mybrightday.brighthorizons.com/api/v2/...` and `https://mybrightday.brighthorizons.com/remote/v1/...`.

## Programmatic Implementation Steps (Go)

1.  **Implement OIDC with PKCE**:
    *   Generate random `code_verifier`.
    *   Generate `code_challenge` (SHA256 hash of verifier, base64 raw URL encoded).
    *   Generate random `state` and `nonce`.
    *   Perform the multi-step login on `bhloginsso.brighthorizons.com`.
    *   Use a custom `CheckRedirect` on the `http.Client` to capture the code from the callback URL without following it.
2.  **Exchange for Tokens**:
    *   Exchange the code for the FIC JWT using the standard OIDC token endpoint.
3.  **Exchange for MBD Token**:
    *   Send a `GET` request to `https://mbdwgateway.brighthorizons.com/api/account/mbdtoken` with the FIC JWT as a Bearer token.
4.  **Initialize MBD Session**:
    *   Request `https://mybrightday.brighthorizons.com/auth/jwt/redirect?jwt=<MBD_JWT>`.
    *   Extract the `session` cookie from the cookie jar using the final response URL.
    *   Ensure a descriptive `User-Agent` header is sent with all requests.

## Data Structures & Workflows
- **Guardian Profile**: Fetch the user's internal ID via `GET /api/v2/user/profile`.
- **Dependent IDs**: Fetch the children (dependents) and their associated daycare center via `GET /api/v2/dependents/guardian/{guardianID}`.
- **Center Information**: Fetch daycare center details (including timezone and address) via `GET /api/v2/center/{centerID}`.
- **Media Feeds**: Fetch photo attachments for specific dates via `POST /api/v2/dependent/memories/media`.

## Refresh-token flow (what the tool does now)

### One-time login (`mbdb login`)
Identical to stage 1 above except that the browser does the credential and CAPTCHA steps, and the redirect URI is `http://localhost:4200/okta/callback`, which is registered on the same client (it is Bright Horizons' own dev-server address; no other localhost port is accepted).

1.  The tool generates a PKCE verifier/challenge and `state`, and opens `GET /authorize` in the browser.
2.  The user logs in. Auth0 redirects the browser to `http://localhost:4200/okta/callback?code=…&state=…`.
3.  Locally the tool is listening on that port and catches the code. Where it cannot listen (CI, or port busy), the user pastes the address the browser landed on; the two halves can run as separate processes, with the verifier kept in `config/mybrightday/pkce_state`.
4.  `POST /oauth/token` with `grant_type=authorization_code`, `client_id`, `code`, `code_verifier`, `redirect_uri`. The response includes a `refresh_token` because the scope asked for `offline_access`. It is saved to `config/mybrightday/refresh_token`.

Authorization codes are single use and valid for about ten minutes.

### Every run
1.  `POST /oauth/token` with `grant_type=refresh_token`, `client_id=<FIC client id>`, `refresh_token`. No client secret is needed. The response contains a 15-minute `access_token` (the FIC JWT), an `id_token`, and a **new** `refresh_token`.
2.  **Register the login with the gateway**: `POST https://mbdwgateway.brighthorizons.com/api/account/oktaservice` with body `{"idToken": "<id_token>"}` and `Authorization: Bearer <access_token>`. This is what the Family Info Center web app does right after every browser login. The gateway answers `{"isAuthorized": true, ...}` and, from then on, will mint MBD tokens for this login. Without it, `mbdtoken` returns a bare `401` even for a perfectly valid access token, and the record the gateway keeps expires after a while, so the tool makes this call on every run.
3.  Stages 3 and 4 above, unchanged.

### Rotation and reuse
Auth0 rotates refresh tokens with reuse detection. Measured on 2026-11-15: an old token is still accepted for a grace window of somewhere between 5 and 20 minutes after rotation; after that, presenting it returns `403 invalid_grant "Unknown or invalid refresh token."` **and revokes the whole token family**, including the newest token. Consequences:

- The rotated token must be persisted immediately after every grant. The tool rewrites `config/mybrightday/refresh_token` before touching any storage backend.
- A token must only ever be used from one place. Two machines sharing a token will revoke each other.
- A rejected token is deleted so the next run asks for a login instead of failing again. Only Auth0's `invalid_grant` counts as rejection; rate limiting (`429`) or an `access_denied` block leave the token valid and are reported as plain errors.
- The absolute lifetime of a refresh token has not been measured.
- Inherent to rotation: if Auth0 processes a grant but the response is lost (timeout), the new token is never seen, the next run presents the consumed one, and the family is revoked. The tool deliberately does not retry the grant, since a retry of a request that actually succeeded would do the same damage.

### Not available on this client
- Device authorization grant (`/oauth/device/code`): `unauthorized_client`.
- Any redirect URI other than the registered ones; `localhost:4200/okta/callback` is the only localhost option.
