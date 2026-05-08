# MyBrightDay Authentication Flow Analysis

This document details the reverse-engineered authentication flow for MyBrightDay, specifically how to programmatically obtain a `session` cookie starting from a user's credentials.

## Overview
The authentication is a multi-stage process involving Auth0 (OIDC with PKCE), the Bright Horizons Family Info Center (FIC), and finally a token exchange with MyBrightDay (Tadpoles).

## Authentication Stages

### 1. Auth0/Okta Authentication (OIDC with PKCE)
**Base Domain**: `bhloginsso.brighthorizons.com`
**Client ID**: `5VIzhuWNKxFc9etVvp5fonr2tlbBEZae`
**Redirect URI**: `https://familyinfocenter.brighthorizons.com/okta/callback`

**Flow**:
1.  **Authorize**: `GET /authorize` with OIDC parameters and a PKCE `code_challenge`.
2.  **Identifier**: `POST /u/login/identifier` with the user's email.
3.  **Password**: `POST /u/login/password` with the user's password.
4.  **Callback**: Intercept the 302 redirect to the `Redirect URI` to capture the authorization `code`.
5.  **Token Exchange**: `POST /oauth/token` with the `code` and PKCE `code_verifier` to obtain the FIC Access Token (JWT).

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
- This cookie is required for all subsequent calls to `https://mybrightday.brighthorizons.com/remote/v1/...`.

## Programmatic Implementation Steps (Go)

1.  **Implement OIDC with PKCE**:
    *   Generate random `code_verifier`.
    *   Generate `code_challenge` (SHA256 hash of verifier, base64 raw URL encoded).
    *   Perform the multi-step login on `bhloginsso.brighthorizons.com`.
    *   Use a custom `CheckRedirect` on the `http.Client` to capture the code from the callback URL.
2.  **Exchange for Tokens**:
    *   Exchange the code for the FIC JWT using the standard OIDC token endpoint.
3.  **Exchange for MBD Token**:
    ```go
    req, _ := http.NewRequest("GET", "https://mbdwgateway.brighthorizons.com/api/account/mbdtoken", nil)
    req.Header.Set("Authorization", "Bearer " + ficToken)
    ```
4.  **Initialize MBD Session**:
    *   Request `https://mybrightday.brighthorizons.com/auth/jwt/redirect?jwt=<MBD_JWT>`.
    *   Extract the `session` cookie from the cookie jar using the final response URL.

## Data Structures
- **Dependent ID**: Often passed as a query param (e.g., `dependent_id=67ed74c7f545b75ea933f535`). This ID is found in the `memberships` or `guardians` API responses.
