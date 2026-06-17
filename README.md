# Zener

Zener is a tiny anonymous file dropbox. Uploaders can push files to unguessable upload-page URLs, and only the admin can list or download what arrived.

## Run Locally

1. Copy `.env.example` to `.env`.
2. Set `SESSION_SECRET` to at least 32 random bytes encoded as base64.
3. Set the admin password and the S3 settings. Either set `ADMIN_PASSWORD`
   directly, or — to keep the plaintext out of the configuration — store a
   bcrypt hash in `ADMIN_PASSWORD_HASH`. Generate one with `go run ./cmd/zener
   hash-password` (prompts for the password) or `go run ./cmd/zener
   hash-password 'your-password'`. When both are set, the hash takes precedence.
4. Build the frontend and run the server:

```bash
cd frontend
npm install
npm run build
cd ..
go run ./cmd/zener
```

The admin UI is at `/admin`. Public upload pages are created from the admin dashboard.

## Docker

The bundled `docker-compose.yml` is a self-contained demo: it builds the app and
runs a Caddy reverse proxy in front of it.

```bash
cp .env.example .env   # fill in the secrets and S3 settings
ZENER_DOMAIN=localhost docker compose up --build
```

Caddy listens on ports 80/443 and forwards to `zener-app:8080`. Set
`ZENER_DOMAIN` to a real domain (with DNS pointing at the host) to get automatic
HTTPS; it defaults to `localhost`, for which Caddy issues a local CA certificate.

Because Caddy is the single proxy in front of the app, keep `TRUSTED_PROXY_HOPS=1`
in `.env`. Zener then reads the client address that Caddy appends to
`X-Forwarded-For` and ignores any spoofed value a client puts on the left, so
login and PIN rate limiting key on the real client IP.

### Using an existing shared Caddy instead

If you already run a shared Caddy on an external network, drop the `caddy`
service and attach the app to that network instead. Use a unique service name
(`zener-app`) to avoid a DNS-alias collision with another project's container,
and add a block to your existing Caddyfile:

```caddyfile
zener.example.com {
    reverse_proxy zener-app:8080
}
```

Keep `TRUSTED_PROXY_HOPS` equal to the number of proxies that append to
`X-Forwarded-For` in front of the app (1 for a single Caddy). If you expose the
app directly with no proxy, set `TRUSTED_PROXY_HOPS=0` so the header is ignored
and the real TCP peer is used.

## Configuration

Zener loads `.env` if present and then reads environment variables. Startup fails fast if required secrets or S3 values are missing.

`SESSION_SECRET` must be base64-encoded and decode to at least 32 bytes. `MAX_FILE_SIZE` defaults to 5 GiB. Per-page limits can only lower the global limit, and a non-empty `ALLOWED_EXT` is a hard ceiling that per-page extension lists may narrow but not widen.

Admin sessions are stateless signed cookies (7-day expiry). Rotating `SESSION_SECRET` invalidates every outstanding session immediately; changing only the admin password (`ADMIN_PASSWORD` or `ADMIN_PASSWORD_HASH`) does not, so rotate the secret too if you need to force existing sessions to log out.

Server-blind E2E intake is controlled by `E2E_INTAKE_ENABLED`,
`E2E_INTAKE_REQUIRED`, and `E2E_INTAKE_ALGORITHM`. The supported cryptographic
profile is `ML-KEM-1024-P384-HKDF-SHA512-AES-256-GCM`: the uploader browser
combines ML-KEM-1024 with P-384 ECDH, derives AES-256-GCM keys with
HKDF-SHA-512, encrypts file bytes and metadata locally, and uploads only
ciphertext. The server stores page public keys and encrypted upload envelopes,
but never stores E2E private keys. Back up each generated private key; if it is
lost, matching encrypted uploads cannot be recovered.

The admin UI can optionally store a generated private key encrypted in browser
IndexedDB. The stored value is encrypted with a passphrase that the admin
supplies and that Zener does not save. This is safer than keeping an unencrypted
downloaded key, but weaker than a password manager or offline backup and still
does not protect against a compromised admin-origin script after the key is
unlocked.

## Storage

Metadata is stored in SQLite at `DB_PATH`. The database runs in WAL mode with a
busy timeout so concurrent uploads don't fail under lock contention; this creates
`-wal` and `-shm` sidecar files next to `DB_PATH`, so back up the whole directory.
File bodies are streamed to S3-compatible object storage under
`S3_PREFIX/<slug>/<uuid>/<filename>`.
