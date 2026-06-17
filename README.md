# Zener

**A tiny, self-hostable, server-blind secure intake box.**
One Go binary. Anonymous uploads. Post-quantum end-to-end encryption. Nothing flows back out.

[![License: GPL-3.0](https://img.shields.io/badge/license-GPL--3.0-blue.svg)](LICENSE.md)
[![Go 1.26](https://img.shields.io/badge/go-1.26-00ADD8.svg)](go.mod)
[![Single binary](https://img.shields.io/badge/deploy-single%20binary-brightgreen.svg)](#installation)
[![E2E: ML-KEM-1024 hybrid](https://img.shields.io/badge/E2E-ML--KEM--1024%20%2B%20P--384-6f42c1.svg)](#server-blind-post-quantum-e2e-intake)

---

Zener is named after the [Zener diode](https://en.wikipedia.org/wiki/Zener_diode): data flows **one way only**. Uploaders push files into an unguessable upload-page URL — they can never list, download, or even see what else has arrived. Only the authenticated admin can read what came in.

It is **not** a file-sharing product. It is an **asymmetric, anonymous intake box**: the admin creates a capability URL, hands it out, and someone on the other side drops files in. That is the whole shape of the product, and everything else is built to keep that shape small and legible.

With **server-blind E2E intake** enabled, the uploader's browser encrypts every file with **post-quantum hybrid cryptography** *before a single byte leaves the device*. The Go server and your S3 bucket only ever touch ciphertext. The admin decrypts client-side at download time. There is no plaintext for the server — or anyone who compromises it — to read.

> A SecureDrop-grade intake capability without SecureDrop's operational weight: no Tor, no hardened workstation, no multi-server deployment. Just one binary behind your existing reverse proxy.

## Who it's for

- **Lawyers** receiving privileged or sensitive client documents
- **Journalists** receiving source material and leaks
- **HR / compliance teams** running whistleblower channels
- **Doctors and researchers** collecting sensitive records
- **Anyone** who needs to collect files from people who should not need an account

## Why Zener

- **One-way by construction.** The uploader API surface is exactly three routes. There is no listing endpoint a sender can reach. Knowing one page's URL reveals nothing about any other page or the admin area.
- **No accounts for uploaders. Ever.** The unguessable URL *is* the capability. That same trusted channel also carries the page's public key, so server-blind encryption needs no separate PKI or key-exchange ceremony.
- **Server-blind, post-quantum E2E.** Optional per-deployment, optional or required per-page. ML-KEM-1024 + P-384 hybrid KEM, HKDF-SHA-512, AES-256-GCM — encrypted in the browser before upload.
- **Tiny and legible.** A single CGO-free Go binary with an embedded React frontend, one `.env`, one SQLite file, one S3 bucket. You can read the whole threat model in an afternoon.
- **Bounded memory at any file size.** Uploads stream straight into an S3 multipart upload and downloads stream straight back out. A 5 GB file never lands on local disk or fills RAM.

## How it works

```mermaid
sequenceDiagram
    participant U as Uploader browser
    participant Z as Zener (Go server)
    participant S as S3 bucket
    participant A as Admin browser

    Note over A: Admin generates an ML-KEM-1024 + P-384 keypair.<br/>Public key is attached to the upload page.<br/>Private key never leaves the admin device.
    U->>Z: GET /u/{slug}  (page metadata + public key)
    Note over U: Browser encrypts file and metadata locally:<br/>ML-KEM-1024 + P-384 -> HKDF-SHA-512 -> AES-256-GCM
    U->>Z: POST ciphertext + opaque envelope
    Z->>S: store {uuid}.zener  (ciphertext only)
    Note over Z,S: Server and bucket never see plaintext,<br/>original filename, or any private key.
    A->>Z: GET ciphertext + envelope  (authenticated)
    Z-->>A: ciphertext + envelope
    Note over A: Browser decrypts with the private key.<br/>Plaintext exists only on the two endpoints.
```

Without E2E, Zener is still a strict one-way intake box: streaming uploads to S3, unguessable slugs, optional PINs, and admin-only listing and download. E2E mode adds the server-blindness on top.

## How it compares

Most "send me a file" tools are **outbound** sharing products retrofitted for inbound use, and their servers can read your files in normal operation. Zener is built the other way around: inbound-only intake is the *only* thing it does, which is exactly why server blindness fits naturally instead of being bolted on.

The category itself is not empty — self-hosted "reverse share" tools exist, and at least one already pairs anonymous upload with S3 storage. What none of them do is the structural thing: a persistent, self-hosted, S3-backed intake box where the operator's server *provably cannot read* what was uploaded, with post-quantum encryption of the stored file.

| Project | Intake model | Self-host / license | Client-side E2E of file | Post-quantum at rest | Footprint |
| --- | --- | --- | --- | --- | --- |
| **Zener** | One-way anonymous, no uploader account | Yes, GPL-3.0 | Yes | **Yes** (ML-KEM-1024 + P-384 hybrid) | Tiny, single Go binary |
| Pingvin Share X | Reverse shares plus accounts | Yes, BSD-2-Clause | No | No | Medium (Node, Docker, optional ClamAV) |
| Sharry | Alias pages, anonymous upload to a user | Yes, GPL-3.0 | No | No | Medium (Scala/JVM) |
| Nextcloud File Drop | Public upload link into a folder, no account | Yes, AGPL-3.0 | No | No | Heavy (PHP plus database) |
| OnionShare (receive) | Anonymous inbound over Tor, no account | Yes, GPL-3.0 | Effectively, via Tor | No | Desktop/CLI, not a persistent service |
| SecureDrop | Anonymous whistleblower intake | Yes, AGPL-3.0 | No (server-side GPG; Tor plus air-gap) | No | Very heavy (dedicated hardware) |
| GlobaLeaks | Anonymous whistleblower intake | Yes, AGPL-3.0 | No (server-side; PQ only in TLS) | No (roadmap) | Light-to-medium (single server) |
| Bitwarden Send | One-way send link, sender account | Yes, AGPL/GPL | Yes | No | Medium / SaaS |
| Tresorit Send | One-way send link, no account | No (proprietary, SaaS) | Yes | Announced, not shipped | SaaS |
| Internxt Send | One-way send link, no account | Partial (MIT; self-host reportedly broken) | Yes | Partial (Kyber-512, storage only, lowest level) | SaaS |
| timvisee/send (Firefox Send fork) | One-way send link, no account | Yes, MPL-2.0 | Yes (AES-128) | No | Light (single container) |
| WeTransfer | Outbound send, no account | No (proprietary, SaaS) | No (provider holds keys) | No | SaaS |

Honest caveats so the matrix stays bulletproof: **Tresorit** has publicly chosen the same ML-KEM-1024 hybrid design Zener uses, but as of 2026 it is roadmap, not shipping, and not an anonymous-intake tool. **Internxt** genuinely ships some post-quantum encryption, but it is storage-only, Kyber-512 (NIST category 1, the lowest level), apparently not hybrid, and not confirmed for its Send product. **GlobaLeaks** runs live hybrid post-quantum TLS in transit but still stores submissions under classical encryption. The combination that is unique to Zener is the intersection: one-way anonymous intake, no uploader account, client-side post-quantum E2E of the file, and a tiny self-hosted footprint.

Zener deliberately does **not** try to be a Dropbox, a ticketing system, or a form builder. There are no folders, no comment threads, no multi-tenant sharing permissions. That restraint is the moat.

## Features

- **Unguessable upload pages** — 24-character base62 slugs from `crypto/rand`. A page has a title, optional description, optional PIN, optional expiry, optional per-page max file size, an optional allow-list of extensions, and an active flag.
- **Drag-and-drop uploads** with multi-file support and per-file progress (bytes + ETA).
- **Optional PIN** per page (bcrypt-hashed, rate-limited per slug+IP).
- **Admin dashboard** — create/edit/delete pages, list uploads with name/size/time, download a single file, or download a whole page as a streamed `.zip`.
- **QR codes and copy buttons** for sharing capability URLs.
- **Server-blind post-quantum E2E intake** (see below).
- **Single static binary** with the frontend embedded via `embed.FS`. Pure-Go SQLite means CGO-free builds and trivial cross-compilation.

## Security model

- **No uploader-reachable listing.** The public surface is exactly `GET /api/u/:slug` (metadata), `POST /api/u/:slug/pin`, and `POST /api/u/:slug`. Upload responses never include other files.
- **Unguessable capability URLs.** At least 24 chars, base62, cryptographically random.
- **Admin password** hashed with bcrypt. Supply it as plaintext (`ADMIN_PASSWORD`) or — better — as a precomputed bcrypt hash (`ADMIN_PASSWORD_HASH`) so the plaintext never lives in your config. Passwords beyond bcrypt's 72-byte limit are handled via an internal SHA-256 prehash.
- **Rate limiting.** Admin login 5/min/IP, PIN attempts 10/min per slug+IP, keyed on the real client IP (see `TRUSTED_PROXY_HOPS`).
- **Sessions.** Stateless HMAC-signed cookies, 7-day expiry, `HttpOnly` + `Secure` + `SameSite=Lax`.
- **CSRF.** Admin mutations require the `X-Zener-CSRF` custom header in addition to the same-site cookie.
- **Streaming with hard caps.** The size limit is enforced by a counting reader while streaming; an oversized upload aborts the S3 multipart upload instead of trusting `Content-Length`. Files are never buffered whole in memory or on disk.
- **Path-safe storage.** S3 keys use server-generated UUID paths (`S3_PREFIX/<slug>/<uuid>/<filename>`); the original filename is metadata only, so a malicious name can't traverse or collide.
- **Downloads are always `Content-Disposition: attachment`**, never inline, so the bucket can't be used as an XSS host.
- **Secrets are never logged.** Startup echoes a redacted config.

## Installation

Zener needs three things to run: a place for metadata (a local SQLite file, created automatically), an S3-compatible bucket for file bodies, and a handful of secrets in a `.env` file. Startup fails fast with a clear message if anything required is missing.

### Prerequisites

- An **S3-compatible bucket** with credentials. Any provider works: AWS S3, Wasabi, Backblaze B2, or a self-hosted MinIO. The bucket must already exist; Zener does not create it.
- For the **Docker** path: Docker with the Compose plugin.
- For the **from-source** path: Go 1.26+ and Node.js 22+.

### Step 1: Create and fill in `.env`

```bash
cp .env.example .env
```

Generate a session secret (base64 that decodes to at least 32 bytes) and put it in `.env` as `SESSION_SECRET`:

```bash
openssl rand -base64 32
```

Set the admin password. Either set `ADMIN_PASSWORD` directly, or — recommended — store only a bcrypt hash so the plaintext never lives in your config:

```bash
go run ./cmd/zener hash-password            # prompts for the password
go run ./cmd/zener hash-password 'your-pw'  # or pass it as an argument
```

Put the printed hash in `ADMIN_PASSWORD_HASH`. If both are set, the hash wins.

Finally, fill in the S3 settings (`S3_ENDPOINT`, `S3_REGION`, `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`) and set `BASE_URL` to the public URL the app is reached at. To turn on the post-quantum intake feature, set `E2E_INTAKE_ENABLED=true` (and optionally `E2E_INTAKE_REQUIRED=true` to reject any plaintext page or upload).

### Option A: Docker Compose (recommended)

The bundled `docker-compose.yml` is a self-contained demo: it builds the app (multi-stage build to a distroless static image) and runs a Caddy reverse proxy in front of it. The app listens on port 8080 and Caddy forwards to it.

Set `BASE_URL` in `.env` to the URL Caddy will serve, for example `https://zener.example.com`, or `https://localhost` for a local trial. Keep `TRUSTED_PROXY_HOPS=1`, because Caddy is the single proxy appending to `X-Forwarded-For`.

Local trial (Caddy issues a local CA certificate for `localhost`):

```bash
ZENER_DOMAIN=localhost docker compose up --build
```

Production with automatic HTTPS (point your domain's DNS at the host first):

```bash
ZENER_DOMAIN=zener.example.com docker compose up --build -d
```

Caddy listens on ports 80 and 443. The app container publishes no ports of its own; it is only reachable through the proxy.

### Option B: From source

Build the frontend once, then run the server:

```bash
cd frontend
npm install
npm run build
cd ..
go run ./cmd/zener
```

Open `http://localhost:8080/admin` and log in with `ADMIN_USERNAME` (default `admin`) and the admin password from your `.env`. For a fully local stack with no cloud account, run MinIO and set `S3_USE_PATH_STYLE=true`.

To produce a standalone binary instead:

```bash
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o zener ./cmd/zener
```

The build is CGO-free (pure-Go SQLite driver), so cross-compilation and single-binary distribution are trivial.

### Option C: Behind an existing shared Caddy

If you already run a shared Caddy on an external network, drop the bundled `caddy` service and attach the app to that network instead. Keep the unique service name `zener-app` to avoid a DNS-alias collision with another project's container, and add a block to your existing Caddyfile:

```caddyfile
zener.example.com {
    reverse_proxy zener-app:8080
}
```

Set `TRUSTED_PROXY_HOPS` to the number of proxies that append to `X-Forwarded-For` in front of the app (1 for a single Caddy). If you expose the app directly with no proxy, set `TRUSTED_PROXY_HOPS=0` so the header is ignored and the real TCP peer is used. Setting it too high lets clients spoof their IP and bypass rate limiting.

### Step 2: First run

1. Log in at `/admin`.
2. Click **New page**, give it a title, and optionally set a PIN, an expiry, a max file size, or an allow-list of extensions.
3. If E2E intake is enabled, generate a keypair for the page. **Back up the private key immediately and securely.** If you lose it, the encrypted uploads are gone.
4. Share the `BASE_URL/u/<slug>` URL (a QR code is provided). Anyone with the link can upload; nobody but you can read what arrives.

## Configuration

Zener loads `.env` if present and then reads environment variables. Startup fails fast if required secrets or S3 values are missing.

| Variable | Required | Default | Notes |
|---|:---:|---|---|
| `PORT` | | `8080` | Listen port. |
| `BASE_URL` | Yes | | Used to build shareable `/u/<slug>` URLs. |
| `SESSION_SECRET` | Yes | | Base64; must decode to at least 32 bytes. Rotating it invalidates all sessions. |
| `ADMIN_USERNAME` | | `admin` | |
| `ADMIN_PASSWORD` | Yes* | | Plaintext, bcrypt-hashed in memory at boot. |
| `ADMIN_PASSWORD_HASH` | Yes* | | Precomputed bcrypt hash (preferred). Takes precedence over `ADMIN_PASSWORD`. |
| `MAX_FILE_SIZE` | | `5368709120` (5 GiB) | Global default; per-page limits may only lower it. |
| `ALLOWED_EXT` | | *(any)* | Comma list, e.g. `pdf,png,zip`. A hard ceiling per-page lists may narrow but not widen. |
| `TRUSTED_PROXY_HOPS` | | `1` | Number of trusted proxies appending to `X-Forwarded-For`. `0` = directly exposed. |
| `DB_PATH` | | `/data/zener.db` | SQLite path (WAL mode; back up the whole directory). |
| `S3_ENDPOINT` | Yes | | S3-compatible endpoint. |
| `S3_REGION` | Yes | | |
| `S3_BUCKET` | Yes | | |
| `S3_ACCESS_KEY` | Yes | | |
| `S3_SECRET_KEY` | Yes | | |
| `S3_USE_PATH_STYLE` | | `false` | `true` for MinIO. |
| `S3_PREFIX` | | `pages/` | Key namespace inside the bucket. |
| `E2E_INTAKE_ENABLED` | | `false` | Enables server-blind E2E intake. |
| `E2E_INTAKE_REQUIRED` | | `false` | Rejects plaintext pages/uploads. Requires `E2E_INTAKE_ENABLED=true`. |
| `E2E_INTAKE_ALGORITHM` | | `ML-KEM-1024-P384-HKDF-SHA512-AES-256-GCM` | The only supported profile. |

\* Exactly one of `ADMIN_PASSWORD` / `ADMIN_PASSWORD_HASH` is required.

`MAX_FILE_SIZE` defaults to 5 GiB. Admin sessions are stateless signed cookies (7-day expiry). Rotating `SESSION_SECRET` invalidates every outstanding session immediately; changing only the admin password does not, so rotate the secret too if you need to force existing sessions to log out.

## Server-blind post-quantum E2E intake

When `E2E_INTAKE_ENABLED=true`, admins can create pages whose uploads are encrypted in the uploader's browser **before any bytes leave the device**. Set `E2E_INTAKE_REQUIRED=true` to reject plaintext pages and plaintext uploads entirely.

**How it works:**

1. The admin generates an encryption identity in the browser. The **public key** is attached to the upload page; the **private key** never touches the server.
2. The public key rides on the same unguessable capability URL the admin already shares — no separate PKI or key server.
3. The uploader's browser encrypts each file and its metadata locally and uploads only ciphertext.
4. The server stores page public keys and encrypted upload envelopes, but **never** stores E2E private keys.
5. The admin decrypts downloads client-side with the private key.

**The cryptographic profile** is `ML-KEM-1024-P384-HKDF-SHA512-AES-256-GCM`: the browser combines **ML-KEM-1024** (post-quantum KEM, via [`@noble/post-quantum`](https://github.com/paulmillr/noble-post-quantum)) with **P-384 ECDH** in a hybrid construction, derives **AES-256-GCM** keys with **HKDF-SHA-512**, encrypts file bytes and metadata locally, and uploads only ciphertext. The hybrid design means an attacker would have to break *both* a classical and a post-quantum primitive — and recorded ciphertext stays safe against future quantum "harvest now, decrypt later" attacks. Both the HKDF `info` and the AES-GCM additional data fold in the canonical envelope, so no envelope field can be swapped without breaking decryption. ML-KEM-1024 targets NIST security category 5, the highest level.

**Back up each generated private key.** If it is lost, the matching encrypted uploads **cannot be recovered** — not by you, not by anyone. The server is blind by design.

The admin UI can optionally store a generated private key encrypted in browser IndexedDB. The stored value is wrapped with a key derived from an admin-supplied passphrase via memory-hard Argon2id and sealed with AES-256-GCM; Zener never saves the passphrase. This is safer than keeping an unencrypted downloaded key, but weaker than a password manager or offline backup, and still does not protect against a compromised admin-origin script after the key is unlocked.

**What server-blind does and does not protect against.** A trust product lives or dies on the precision of this claim, so state it plainly. Zener's E2E mode protects you against the passive, at-rest adversary: a stolen S3 bucket, a compromised admin session, an honest-but-curious operator, a backup that walks out the door, a full server compromise after the fact. In every one of those, the attacker gets ciphertext and an opaque envelope and nothing else. The true line is *the operator cannot read stored uploads; a breach yields only ciphertext.* What it does **not** defend against is an actively malicious or compromised host at upload time: the encryption runs in JavaScript the server delivers, and the public key arrives over the same channel, so a host that deliberately serves modified code or swaps the key could defeat it. The admin sees the key fingerprint in the dashboard, but it is not yet surfaced on the upload page for a source to compare out of band. This is the same caveat that applies to Firefox Send and to webmail-based "E2E"; it is inherent to browser-delivered cryptography, not a flaw in the construction. There is also no forward secrecy — the recipient key is static, so a leaked private key exposes the whole historical bucket — and because E2E encryption happens in a single pass in the uploader's browser, keep E2E uploads to a few hundred megabytes for now. The line you must **not** read into Zener is "zero-trust even against the host who runs it."

## Storage

Metadata is stored in SQLite at `DB_PATH`. The database runs in WAL mode with a busy timeout so concurrent uploads don't fail under lock contention; this creates `-wal` and `-shm` sidecar files next to `DB_PATH`, so back up the whole directory. File bodies are streamed to S3-compatible object storage under `S3_PREFIX/<slug>/<uuid>/<filename>`. In E2E mode the stored object is opaque ciphertext (a `.zener` blob), the content type is forced to `application/octet-stream`, and the original filename lives only inside the encrypted envelope — never in the object key.

## Tech stack

- **Backend:** Go (stdlib `net/http` + `chi`), pure-Go SQLite (`modernc.org/sqlite`, CGO-free), AWS SDK v2 for any S3-compatible endpoint, `log/slog` JSON logging.
- **Frontend:** React + TypeScript + Vite + Tailwind CSS, built to `frontend/dist/` and embedded into the binary.
- **Crypto:** bcrypt for passwords/PINs; `@noble/post-quantum` (ML-KEM-1024) + WebCrypto (P-384 ECDH, HKDF-SHA-512, AES-256-GCM) for E2E; memory-hard Argon2id (`@noble/hashes`) for the optional in-browser private-key store.
- **Deployment:** multi-stage Dockerfile to `gcr.io/distroless/static`, `docker-compose.yml` with Caddy for automatic HTTPS.

## Development

```bash
go test ./...            # backend tests
cd frontend && npm test  # frontend tests (Vitest), including the E2E crypto round-trip
```

## License

Zener is free software under the **GNU General Public License v3.0** (or later). See [LICENSE.md](LICENSE.md).
