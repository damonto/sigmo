# Authentication and delivery configuration

Sigmo uses one explicit authentication mode: `none` (anonymous), `otp`, `oidc`, or `cloudflare_access` (Access required). Disabling notifications does not disable OTP; disabling OTP does not automatically allow anonymous access. Web and API traffic must share the same origin.

## Configure channels once

`channels` stores delivery credentials; `notifications.channels` selects notification destinations; `auth.otp.channels` selects verification-code destinations. Channels no longer have an `enabled` field. For example, to use email only for verification codes:

```json
{
  "channels": {"email": {"smtpHost": "smtp.example.com"}},
  "notifications": {"channels": []},
  "auth": {
    "mode": "otp",
    "sessionMaxDays": 30,
    "otp": {"channels": ["email"]}
  }
}
```

This example shows only the storage structure; complete all required email fields on the settings page. The settings API uses flat fields such as `otpChannels` and `oidcIssuer`, which the server maps to this storage structure. Save the channel configuration and send a test notification before enabling OTP. When multiple OTP channels are selected, successful delivery through at least one channel is sufficient to proceed with login. Each send creates an independent challenge that expires after 5 minutes and allows at most 5 attempts.

Channel saves validate the configuration even when neither notifications nor OTP selects the channel. Invalid saves leave the previous configuration unchanged. Both clients offer saved channels for selection; saving credentials alone does not enable either use. Local configuration validation does not contact the provider or prove successful delivery.

## Standard OIDC

1. Create a confidential server-side client at the identity provider with support for Discovery, Authorization Code, PKCE S256, and the `openid` scope.
2. Register the exact callback `https://<Sigmo host>/api/v1/auth/oidc/callback` with the identity provider. Include the port and any reverse-proxy path prefix used to access Sigmo. Register each address used by Web or iOS explicitly; do not use wildcard redirects.
3. Select `oidc` and configure the HTTPS issuer, client ID, client secret, and allowed **subjects (`sub`)**. Sigmo derives its own address during login; there is no Public URL setting. Use stable subjects instead of mutable email addresses, and explicitly authorize the intended users.
4. Web and iOS share the same server-side client. The client secret stays on the Sigmo backend and is omitted from settings responses. Leaving it blank preserves the existing secret for the same issuer/client ID; switching clients requires a new secret.
5. Verify subjects and redirect URLs before enabling OIDC, and ensure the offline recovery procedure below is available. Saving the policy invalidates existing sessions. The current implementation does not offer a test OIDC login before saving.

The login origin comes from the request's `Host` and Echo v5's scheme detection.
Reverse proxies must preserve the external `Host` (including a non-default port)
and overwrite `X-Forwarded-Proto`. Echo's default scheme detection accepts proxy
headers only from loopback, link-local, private-network, or local Unix-socket peers;
other peers cannot select HTTPS through a forwarded header. `X-Forwarded-Host` is
not used. Web and iOS derive any path prefix from their current API URL, so a proxy
may strip that prefix before forwarding. For example, `/sigmo/api/v1` produces
the callback `/sigmo/api/v1/auth/oidc/callback`.

Each login transaction retains its original callback URL for both authorization
and token exchange. The browser entry point and callback must use the initiating
origin, and Web login returns to that same address. OIDC login still requires
HTTPS because the browser-binding cookie is Secure.

Web uses a host-only, HttpOnly, SameSite=Lax cookie and validates the Origin of state-changing requests. Production requires HTTPS; the browser does not store bearer tokens. iOS uses the system browser to preserve the IdP login session. After validating state, nonce, issuer, audience, signature, expiry, and subject, the server returns a short-lived handoff code for the app only. This code is also bound to the app's own PKCE challenge; the upstream authorization code is never used as an application session.

Sigmo access tokens on iOS last at most 15 minutes. Refresh tokens rotate on every use, and reuse revokes the entire session. Sessions cannot renew indefinitely: OTP sessions are limited to the configured number of days, and OIDC sessions last at most 24 hours. A failed refresh does not automatically replay application writes; an uncertain refresh outcome requires a new login. Logging out or revoking access at the IdP does not immediately revoke existing Sigmo sessions. Local logout and policy changes revoke or invalidate the relevant sessions, and OIDC retains its 24-hour limit. OIDC back-channel logout is not currently implemented.

## Cloudflare Access

1. Place Sigmo behind an Access Self-hosted application, preferably connected through Cloudflare Tunnel. Block direct public access to the origin; do not treat spoofable proxy headers as authentication results.
2. Enable Managed OAuth and Dynamic Client Registration, allowing the **exact callback** `https://sigmo-pro.952007.xyz/auth/callback`. Do not allow arbitrary localhost/loopback callbacks. Configure suitable access-token and session lifetimes.
3. Set Sigmo's authentication mode to `cloudflare_access` and enter `https://<team>.cloudflareaccess.com`, the application AUD, and allowed subjects. The backend validates the signature, issuer, audience, expiry, and subject of `Cf-Access-Jwt-Assertion`. All APIs, including authentication discovery, licensing, and MCP, enforce this boundary; a Sigmo token cannot bypass it.
4. On first access, iOS parses the 401 response's `WWW-Authenticate` / `resource_metadata`, discovers the fixed Cloudflare team issuer through HTTPS resource metadata from the same origin, registers a public client, and signs in with code + PKCE. Access/refresh tokens are stored in Keychain and used to access the backend through the Authorization header.
5. Access applications that provide only a standard 302 login redirect without Managed OAuth metadata cannot authenticate native API clients. The app prompts the user to complete the Access configuration. Do not extract Safari cookies or store Access Service Tokens in the app.

Web uses the Access browser session and redirects to `/cdn-cgi/access/logout` on logout. iOS revokes access/refresh tokens through the provider's revocation endpoint and always deletes local credentials. If the provider has no revocation endpoint, the app indicates that the remote session must still be handled in the Access administration console.

Carrier websheets use separate, short-lived random capability URLs (10 minutes by default), authorizing only that page's proxy and callback. Authentication changes, flow completion, or expiry invalidate them. Carrier iframes do not have Sigmo's same-origin privileges, and general session credentials never appear in URLs or HTML. iOS uses a separate WebKit scheme and attaches credentials on the native side only for the current page path. Access mode still requires edge authentication: the Web Access application cookie must be **HttpOnly, Secure, SameSite=None** to support isolated iframes. If the carrier sends preflight requests, Cloudflare Access must also allow OPTIONS to reach the origin. The origin handles unauthenticated OPTIONS only on websheet capability routes, validates the capability, and returns empty CORS metadata. Actual requests still require authentication; other APIs do not allow cross-origin access. If the browser blocks these cookies, adjust the site's cookie policy or validate the flow through the native iOS entry point.

References: [Managed OAuth](https://developers.cloudflare.com/cloudflare-one/access-controls/authenticate-agents/), [Access Cookie](https://developers.cloudflare.com/cloudflare-one/access-controls/applications/http-apps/authorization-cookie/).

## pro-api callback hosting and iOS signing

pro-api serves two static resources that require no session:

- `https://sigmo-pro.952007.xyz/auth/callback`: a system-browser callback fallback page that does not reflect code/state or load scripts, analytics, or external resources. Caching, embedding, and referrers are disabled.
- `https://sigmo-pro.952007.xyz/.well-known/apple-app-site-association`: associates `465NH4KUMT.com.damon.sigmo`, limits Universal Links to `/auth/callback`, and provides the webcredentials association.

`apps/ios/project.yml` already configures Associated Domains for this domain. Use the same Apple Team ID / Bundle ID and signing configuration with the Associated Domains capability. Update AASA if the app is signed under a different identity. iOS uses `ASWebAuthenticationSession.Callback.https(host:path:)`, validates the exact host/path/state, and rejects duplicate parameters and invalid callbacks.

The Worker configuration includes a custom-domain route. To deploy, run `bun run type-check`, `bun run test`, and `bun run dry-run` in `pro-api/`, then run `bun run deploy` using the existing Cloudflare account. These callback routes add no D1 tables or secrets and require no production database migration. After deployment, confirm that AASA returns a direct HTTPS 200 JSON response without Access, login redirects, or HTML rewrites. Automatic request logs and traces are disabled to avoid recording callback query parameters; application logs retain only route templates. Other Cloudflare, reverse-proxy, and WAF logging pipelines should apply the same treatment to callback query parameters.

Local builds and simulator tests do not replace HTTPS callback acceptance with a correctly signed app. After deployment, use real OIDC and Access accounts to verify first login, repeat login with a retained IdP session, token-expiry refresh, logout, revocation, and an unauthorized subject.

## One-time upgrade and recovery

Before starting the new version, stop the old version and back up the entire SQLite database, including a consistent WAL state. Goose migration 5 moves old channel configuration to `delivery.channels`, removes `enabled`, and maps notification and OTP selections separately. Missing references or corrupt configuration abort the migration instead of falling back to anonymous access. Migration 6 removes `auth_tokens`, creates `auth_sessions` / `auth_credentials`, invalidates old sessions, and binds native push to session IDs. New data-access code is generated by sqlc and can be reproduced with `go generate ./internal/pkg/storage`.

All clients must sign in again after upgrading; old clients and APIs are no longer supported. Do not point an old binary at an upgraded database. To roll back, stop the service and restore both the old binary and the pre-upgrade backup together.

If an incorrect authentication policy prevents login, stop Sigmo, restrict origin access to localhost or the maintenance network, and use the SQLite CLI to edit `app_state` in the current database. For example, temporarily restore anonymous access during an **isolated maintenance window**:

```sql
BEGIN IMMEDIATE;
UPDATE app_state
SET value_json = json_set(value_json, '$.mode', 'none')
WHERE scope = 'global' AND key = 'auth.settings';
DELETE FROM auth_credentials;
DELETE FROM auth_sessions;
COMMIT;
```

After restarting, correct the configuration and re-enable authentication before restoring external access. This operation does not change Cloudflare edge policies; Access configuration errors must be corrected in the Cloudflare administration console. OIDC client secrets are stored in the local settings database, so restrict database permissions and protect backups.

For Web development, keep `VITE_API_BASE_URL=/api/v1` and configure Vite's same-origin proxy with `SIGMO_DEV_BACKEND=http://127.0.0.1:<port> bun run dev`. Do not point the browser's API base at another origin. Use the correct HTTPS domain for OIDC / Access integration acceptance.
