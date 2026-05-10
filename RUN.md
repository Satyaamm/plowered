# Plowered — first live in 60 seconds

The fastest path: `docker compose up -d`. The repo ships everything you
need (Postgres, Redis, NATS, MinIO, the API, the worker, and the web UI)
as a single compose stack. No Go/Node tooling required on the host.

## Prerequisites

- Docker Desktop ≥ 25 (this stack tested on 29.x)
- ~2 GB free RAM and ~5 GB disk

## Boot the stack

```sh
git clone https://github.com/Satyaamm/plowered.git
cd plowered

# .env.example values are fine for local dev; copy them so future
# `make` targets see the same defaults the containers see.
cp .env.example .env
cp web/.env.example web/.env.local

# Set a stable secrets master key so vault contents survive restarts.
# (Without this the API auto-generates an ephemeral key per process.)
echo "PLOWERED_SECRETS_MASTER_KEY=$(openssl rand -base64 32)" >> .env

# Optional: a Resend API key so verification emails actually leave the
# building. Sign up at resend.com → API keys → paste here. If unset,
# verification emails are written to the API container logs and you can
# verify by visiting /verify?token=<token> by hand.
# echo "PLOWERED_RESEND_API_KEY=re_..." >> .env

# Build the Go API + worker + Next.js web image, then bring everything up.
docker compose up -d
```

First run takes ~3–4 min (Go module download, Next.js build, image
exports). Subsequent boots are ~10 s.

When the dust settles you should see seven containers `Up` in
`docker compose ps`:

| Container          | Purpose                                                        |
|--------------------|----------------------------------------------------------------|
| `plowered-postgres`| Catalog + audit + metadata store. Migrations apply on startup. |
| `plowered-redis`   | Asynq queue, idempotency cache, rate-limit counters.           |
| `plowered-nats`    | Durable event bus. The outbox relay publishes to it.           |
| `plowered-minio`   | S3-compatible object store (logs, audit archive, DSR exports). |
| `plowered-api`     | The HTTP/gRPC API (the `plowered` binary).                     |
| `plowered-worker`  | Asynq consumer running pipeline / quality / **crawl** jobs.    |
| `plowered-web`     | Next.js UI on :3000 with a `/api/*` rewrite to the API.        |

## Sign up + verify (the live flow)

The dev-token shortcut is gone. You sign up like a real user.

1. Open <http://localhost:3000/signup>.
2. Fill in workspace name, email, password (≥ 8 chars).
3. The signup endpoint:
   - Creates a `tenant` row + a `user` row (with `email_verified_at = NULL`).
   - Generates a verification token, persists it in `email_verifications`.
   - Tries to send the email via Resend. **If the Resend key is unset
     (or the recipient isn't your verified Resend address)** the send
     is logged as a warning and the platform keeps going. You can pull
     the token from Postgres:

```sh
docker exec plowered-postgres psql -U plowered -d plowered -c \
  "SELECT token FROM email_verifications WHERE used_at IS NULL ORDER BY created_at DESC LIMIT 1;"
```

4. Visit `http://localhost:3000/verify?token=<TOKEN>` — the row's
   `email_verified_at` flips and the token is marked used.
5. Sign in at <http://localhost:3000/login>. The session cookie
   `plowered_session` is set automatically; every subsequent
   `/api/v1/*` request rides that cookie.

## Things to open

- **UI**: <http://localhost:3000> — sign-up / sign-in / portal home.
- **API health**: <http://localhost:8080/healthz>
- **Prometheus metrics**: <http://localhost:8080/metrics>
- **MinIO console**: <http://localhost:9001> (`minioadmin` / `minioadmin`).
- **NATS monitor**: <http://localhost:8222/varz>

## End-to-end demo (≤ 5 minutes)

The full Track 1 story, top to bottom:

1. **Sign up** at `/signup`. Verify your email.
2. **Add a connection** — Connections → New connection. Point it at any
   reachable Postgres (you can use the bundled one: host `postgres`,
   port `5432`, db `plowered`, user `plowered`, password `plowered`,
   sslmode `disable`).
3. **Test connection** — health flips to `healthy`.
4. **Crawl** — click Crawl. The async worker walks
   `information_schema`, projects schemas + tables + columns into the
   catalog, and auto-tags PII / PHI / PCI / secret columns by name.
5. **Catalog** → see your tables in a sortable Fluent DataGrid.
   Drill into any table → Schema tab shows columns with classification
   badges; Lineage tab shows the asset graph; Activity tab shows the
   audit chain entries that touched this resource.
6. **Compliance**:
   - Issue a Legal hold on a resource type → try to delete →
     HTTP 409 with `hold_id` in the body.
   - File a DSR → 30-day SLA clock starts.
   - Soft-delete anything → it lives in the recycle bin until a
     super_admin purges.
7. **Sign out** from the topbar account menu — session cookie is revoked
   server-side, not just cleared client-side.

## Compliance gates — exposed at the API

```sh
# Replace these with values from your /api/v1/auth/login response (the
# session cookie rides the /api/* proxy automatically when you're using
# the web UI; for raw curl you'd carry plowered_session).

# Issue a litigation hold scoped to all pipelines
curl -X POST http://localhost:8080/v1/legal-holds \
  -b /tmp/p.cookies -H "Content-Type: application/json" \
  -d '{"matter":"Acme v Plowered","scope":{"resource_types":["pipeline"]}}'

# A delete on any pipeline now returns 409 with hold_id
curl -i -X DELETE http://localhost:8080/v1/pipelines/<id> -b /tmp/p.cookies

# File a GDPR DSR — the 30-day clock starts immediately
curl -X POST http://localhost:8080/v1/dsr \
  -b /tmp/p.cookies -H "Content-Type: application/json" \
  -d '{"subject_id":"user_42","type":"erasure","notes":"via support"}'
```

## Email delivery in production

Resend keys come in two flavours:

- **Test keys** (the default when you sign up at resend.com) deliver
  *only* to the email tied to your Resend account. Anyone else's signup
  fails with `403 You can only send testing emails to your own email
  address`. The platform handles this gracefully: the API logs a
  warning and the user can still verify by clicking the token URL
  retrieved from `email_verifications`.
- **Domain-verified keys** deliver to anyone. To get there:
  1. resend.com/domains → Add domain (e.g. `plowered.io`).
  2. Add the DKIM CNAME / TXT records Resend gives you.
  3. Wait for verification (usually < 5 min).
  4. Update `PLOWERED_EMAIL_FROM` to use that domain
     (`Plowered <noreply@plowered.io>`).
  5. Use a production API key from the same domain.

Until you do that, signups for non-Resend-account emails will require
the manual token-pull above. Document this for your dev team.

## Stopping & resetting

```sh
docker compose stop                # keeps volumes
docker compose down                # removes containers, keeps volumes
docker compose down -v             # nukes Postgres / Redis / NATS / MinIO state
```

## What's running where

```
                        ┌──────────────────┐
                        │  web (Next.js)   │ :3000
                        │  /api/* rewrite  │
                        └─────────┬────────┘
                                  │ http (cookie-authed)
                        ┌─────────▼────────┐
            grpc :9090  │   plowered (API) │ :8080  ← health, metrics, /v1/*
                        │  · auth + sessions │
                        │  · audit chain   │
                        │  · recycle bin   │
                        │  · legal holds   │
                        │  · DSR queue     │
                        │  · connections   │
                        └─────────┬────────┘
                                  │
        ┌──────────┬──────────────┼──────────────┬──────────┐
        ▼          ▼              ▼              ▼          ▼
   postgres :5432  redis :6379   nats :4222   minio :9000   plowered-worker
   (catalog +      (Asynq queue + (outbox bus  (audit / log  (pipeline / quality /
    audit + secrets)  idempotency)   + JetStream) archive)    crawler jobs)
```

## Without Docker

If you want native Go + Node:

```sh
make env             # copies .env files
make migrate         # apply DB migrations against $PLOWERED_DATABASE_URL
make build           # → bin/plowered, bin/plowered-worker, bin/ploweredctl
./bin/plowered serve &
PLOWERED_DATABASE_URL=... PLOWERED_REDIS_URL=... ./bin/plowered-worker &
make web-install web-dev    # Next.js on :3000
```

You'll need Postgres 16+, Redis 7+, and a NATS server reachable; pointer
at <https://nats.io/download> or run only the dependencies via
`docker compose up -d postgres redis nats minio`.
