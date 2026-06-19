# Installing Sprag

This guide covers the common deployment modes:

- plaintext intake, where Sprag can read uploaded files
- server-blind post-quantum E2E intake, where Sprag stores only ciphertext
- onion-only Tor ingress, where uploaders and admins reach Sprag through a `.onion` address

Sprag always needs three things:

- a SQLite metadata directory
- an S3-compatible bucket for file bodies
- a configured `.env`

## Prerequisites

- Docker with the Compose plugin for the Docker paths.
- Go 1.26+ and Node.js 22+ only if running from source.
- An S3-compatible bucket and credentials. AWS S3, Wasabi, Backblaze B2, and MinIO-style services work. Sprag does not create the bucket.

## Common Configuration

Create the local environment file:

```bash
cp .env.example .env
```

Generate a session secret and put it in `SESSION_SECRET`:

```bash
openssl rand -base64 32
```

Create an admin password hash. With Go installed:

```bash
go run ./cmd/sprag hash-password
```

With Docker only:

```bash
docker compose run --build --rm sprag-app hash-password
```

Put the printed value in `ADMIN_PASSWORD_HASH`. Leave `ADMIN_PASSWORD` empty unless you intentionally want plaintext config.

Fill in S3:

```env
S3_ENDPOINT=https://s3.eu-central-1.wasabisys.com
S3_REGION=eu-central-1
S3_BUCKET=sprag
S3_ACCESS_KEY=...
S3_SECRET_KEY=...
S3_USE_PATH_STYLE=false
S3_PREFIX=pages/
```

For MinIO or another path-style endpoint, set:

```env
S3_USE_PATH_STYLE=true
```

## Mode 1: Plaintext File Dump

Use this when Sprag is just an accountless intake box and the server may read uploaded files.

`.env`:

```env
BASE_URL=https://sprag.example.com
COOKIE_SECURE=auto
TRUSTED_PROXY_HOPS=1
ANONYMOUS_INGRESS=false
IP_STORAGE_MODE=plain
E2E_INTAKE_ENABLED=false
E2E_INTAKE_REQUIRED=false
```

Start with the bundled Caddy reverse proxy:

```bash
SPRAG_DOMAIN=sprag.example.com docker compose up --build -d
```

For a local trial:

```env
BASE_URL=https://localhost
```

```bash
SPRAG_DOMAIN=localhost docker compose up --build
```

First-run smoke test:

1. Open `/admin`.
2. Log in with `ADMIN_USERNAME` and your admin password.
3. Create a page without E2E.
4. Upload a small file from the public `/u/<slug>` page.
5. Confirm the admin file list shows the upload.
6. Download the file and, if needed, export the page ZIP.
7. Export the chain-of-custody manifest and confirm it contains the stored-object SHA-512.

## Mode 2: Server-Blind Post-Quantum E2E Intake

Use this when Sprag and S3 should only handle ciphertext.

`.env`:

```env
BASE_URL=https://sprag.example.com
COOKIE_SECURE=auto
TRUSTED_PROXY_HOPS=1
IP_STORAGE_MODE=hmac-sha256
IP_HASH_SECRET=<base64 32+ byte secret>
E2E_INTAKE_ENABLED=true
E2E_INTAKE_REQUIRED=true
```

Generate `IP_HASH_SECRET` with:

```bash
openssl rand -base64 32
```

Start:

```bash
SPRAG_DOMAIN=sprag.example.com docker compose up --build -d
```

Admin workflow:

1. Open `/admin`.
2. Create a page.
3. Generate or import the page encryption identity.
4. Back up the private key immediately. If it is lost, encrypted uploads cannot be recovered.
5. Share the upload URL.
6. Upload from another browser session.
7. In the admin UI, load the private key and decrypt the download in the browser.

Operational notes:

- The server-side SHA-512 is a ciphertext-object hash, not a plaintext hash.
- E2E pages intentionally do not expose server-side ZIP export as plaintext, because the server cannot decrypt.
- Browser-stored private keys are encrypted with an admin passphrase, but this does not defend against a compromised same-origin script after the key is unlocked.
- E2E protects stored uploads against passive at-rest compromise. It does not protect against a malicious host serving modified upload JavaScript or swapping a public key at upload time.

## Mode 3: Onion-Only Tor Service

Use this when Sprag should have no clearnet ingress and users should open it in Tor Browser.

The Tor compose file maps `ONION_BASE_URL` to the app's internal `BASE_URL`. This avoids hardcoding a generated onion hostname in the repository.

Initial `.env`:

```env
ONION_BASE_URL=
COOKIE_SECURE=auto
TRUSTED_PROXY_HOPS=0
ANONYMOUS_INGRESS=true
IP_STORAGE_MODE=hmac-sha256
IP_HASH_SECRET=<base64 32+ byte secret>
E2E_INTAKE_ENABLED=true
E2E_INTAKE_REQUIRED=true
```

Start the onion stack:

```bash
docker compose -f docker-compose.tor.yml up --build -d
```

Wait until Tor finishes bootstrapping:

```bash
docker compose -f docker-compose.tor.yml logs -f tor
```

Look for:

```text
Bootstrapped 100% (done): Done
```

Read the generated hostname:

```bash
docker compose -f docker-compose.tor.yml exec tor cat /var/lib/tor/sprag/hostname
```

Set it in `.env`:

```env
ONION_BASE_URL=http://<hostname>.onion
```

Recreate the app container so new share and receipt URLs use the onion origin:

```bash
docker compose -f docker-compose.tor.yml up -d --force-recreate sprag-app
```

Tor smoke test:

1. Open `ONION_BASE_URL` in Tor Browser.
2. Log in at `/admin`.
3. Create a page.
4. Confirm copied upload and receipt URLs use the onion origin.
5. Upload through Tor Browser.
6. Confirm the admin file list does not show a Tor container IP as uploader metadata.
7. If E2E is enabled, decrypt the uploaded file in the admin browser.

Important Tor notes:

- Always include `-f docker-compose.tor.yml` when managing this stack. Plain `docker compose ...` uses the default Caddy topology.
- `docker-compose.tor.yml` publishes no host ports. Tor connects internally to `sprag-app:8080`.
- `ANONYMOUS_INGRESS=true` stores no uploader IP metadata. Login throttling is global; PIN throttling is page-scoped.
- The `tor-hidden-service` Docker volume contains the onion identity. Back it up and protect it. Deleting it creates a new onion address.
- Tor mode does not hide Sprag's outbound connection to a cloud S3 provider. Use local MinIO or private storage if storage-provider egress metadata matters.

## From Source

Build the frontend once, then run the Go server:

```bash
cd frontend
npm install
npm run build
cd ..
go run ./cmd/sprag
```

Build a standalone binary:

```bash
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o sprag ./cmd/sprag
```

The binary embeds the frontend and uses pure-Go SQLite, so no CGO runtime is required.

## Backups

Back up these items together:

- SQLite directory at `DB_PATH`, including `-wal` and `-shm` sidecar files
- the S3 bucket/prefix configured by `S3_BUCKET` and `S3_PREFIX`
- `SESSION_SECRET`, because rotating it invalidates all sessions
- `IP_HASH_SECRET`, if `IP_STORAGE_MODE=hmac-sha256`
- every E2E private key generated for a page
- the `tor-hidden-service` volume for onion deployments

For legal or compliance workflows, export the chain-of-custody manifest before moving material into a case file or evidence archive.

## Troubleshooting

**Startup says required config is missing.**
Check `BASE_URL`, `SESSION_SECRET`, admin password or hash, and all required S3 values.

**Admin login succeeds but the session does not stick.**
Check `BASE_URL` and `COOKIE_SECURE`. Plain HTTP public origins are rejected in `auto` mode. HTTP `.onion`, localhost, and loopback are allowed.

**Onion site not found in Tor Browser.**
Wait for `Bootstrapped 100% (done): Done`, verify the hostname from `/var/lib/tor/sprag/hostname`, and make sure you recreated `sprag-app` after setting `ONION_BASE_URL`.

**Tor container restarts with hidden-service ownership warnings.**
Use the named `tor-hidden-service` volume from `docker-compose.tor.yml`. Host bind mounts can appear owned by the wrong user inside the container.

**Share URLs have the wrong origin.**
Fix `BASE_URL` for Caddy/from-source deployments or `ONION_BASE_URL` for Tor deployments, then recreate the app container.

**E2E upload cannot be recovered.**
The matching private key is required. Sprag cannot decrypt stored ciphertext without it.
