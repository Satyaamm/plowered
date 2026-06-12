# Infrastructure

What Plowered needs to run. Every component is described here with: what
it does, why it's required, sizing for a small deploy, and which managed
alternative to swap in when you outgrow the bundled version.

The recipes that **provision** each of these (Dockerfiles, compose files,
nginx config, bootstrap scripts) live in `deploy/`. This folder is the
spec; `deploy/` is the implementation.

---

## Topology

```
                ┌────────────────────────────────────────────────────┐
                │                  PUBLIC INTERNET                   │
                └─────────────────────┬──────────────────────────────┘
                                      │  HTTPS :443
                                      ▼
                       ┌────────────────────────────┐
                       │  nginx     (edge, TLS,     │
                       │            rate-limit,     │
                       │            gateway-auth)   │
                       └──────┬────────────────┬────┘
                              │                │
                  /api/*  ────┘                └──── ploweredapi.*
                  rewritten                    (direct CLI / SDK path,
                  via Next.js                   caller sends X-Gateway-Auth)
                  Edge middleware                       │
                              │                        │
                              ▼                        ▼
                 ┌───────────────────────────────────────────┐
                 │            PRIVATE DOCKER NETWORK         │  ◄── internal: true
                 │  (no public IP on any of these services)  │
                 │                                           │
                 │   plowered-web ─┐                         │
                 │                 │                         │
                 │   plowered-api ─┼──► postgres (catalog)   │
                 │                 │                         │
                 │   plowered-     ├──► redis    (asynq)     │
                 │   worker  ──────┘                         │
                 │                                           │
                 │   nats (events outbox) ◄──── api  ────┐   │
                 │                                       │   │
                 │   minio (S3-compat, AI/DSR mirrors) ◄─┘   │
                 │                                           │
                 │   certbot (Let's Encrypt renewals)        │
                 └───────────────────────────────────────────┘
```

Two networks in the compose stack: `public_net` (just nginx + certbot),
`private_net` (`internal: true` — everything else). The DB never gets a
public address.

---

## Components

### Postgres 16 — the metadata store

**Role.** Catalog, users, tenants, audit, secrets vault, ai-provider
configs, vector-store configs, jobs ledger, feedback, contracts,
certifications. The source of truth for everything except blob payloads.

**Required?** Yes for any non-trivial deployment. The API also has a
memory-mode for the unit-test path but it doesn't persist.

**Extensions.**
- `pgvector` — semantic search default backend
- `uuid-ossp` — for UUID generation

**Sizing.**

| Deploy | vCPU | RAM | Disk |
|---|---|---|---|
| Single-tenant dev | 1 | 1 GB | 10 GB |
| Single-tenant prod | 2 | 4 GB | 50 GB SSD |
| Multi-tenant prod | 4 | 16 GB | 200 GB SSD + replica |

**Managed alternative.** AWS RDS for PostgreSQL, GCP Cloud SQL, Azure
Database for PostgreSQL. Set `PLOWERED_DATABASE_URL` to the managed
endpoint and stop running the compose `postgres` service.

**Backups.** `pg_basebackup` + WAL archiving for prod. The bundled
compose stack ships zero backup automation — wire your own.

---

### Redis 7 — async job queue

**Role.** Backs Asynq, the durable job queue. Long migrations, the
classify runner, search reindex, and any other `>2 sec` operation
enqueues here.

**Required?** No — the API falls back to an in-process sync enqueuer
when `PLOWERED_REDIS_URL` is unset. But then there's no durability:
jobs that crash mid-flight are lost.

**Sizing.** 512 MB RAM per ~100k queued jobs. CPU is negligible.

**Managed alternative.** AWS ElastiCache for Redis, GCP Memorystore,
Upstash. Point `PLOWERED_REDIS_URL` at the managed endpoint.

---

### NATS 2 with JetStream — event outbox relay

**Role.** Cross-process event delivery. The `events.Bus` is in-process
pub/sub for handlers running inside the API binary; for handlers
running in the worker or a future external service, the `outbox`
relay flushes durable events through NATS JetStream.

**Required?** Optional — useful when you split api + worker across
machines or add downstream consumers. Single-binary deployments can
ignore it.

**Sizing.** 1 GB RAM and ~5 GB disk handles ~10k events / sec on a
single stream.

**Managed alternative.** Synadia Cloud (NATS as a service); or swap
out for Kafka / Pulsar if you already have one. The `outbox` package
abstracts the publisher interface — write a new adapter, swap in.

---

### MinIO or S3-compatible object store

**Role.** Holds blob payloads — AI request / response transcripts
(deterministic keys so retries are idempotent), DSR exports (Art. 15 /
20 GDPR bundles), profile snapshots, migration intermediate output.

**Required?** Optional — the API uses an in-memory store when no S3
URL is configured. Without it, transcript history and DSR exports
don't survive a restart.

**Sizing.** 50 GB to start; AI transcripts compress well.

**Managed alternative.** AWS S3, GCP Cloud Storage, Azure Blob,
Cloudflare R2. Set `PLOWERED_OBJECT_STORE_*` env vars to the managed
endpoint + credentials.

---

### nginx — edge layer

**Role.**
- TLS termination (Let's Encrypt certs)
- Rate-limit zones (per-IP on `/v1/auth/*`, general on the rest)
- `X-Gateway-Auth` shared-secret check on the API surface
- Bot blocking via user-agent map
- Custom JSON error pages
- ACME challenge passthrough for cert renewals

**Required?** Yes for any internet-facing deploy. Behind a corporate
load balancer (ALB / Cloud Run / etc.) the LB usually replaces it.

**Config.** `deploy/nginx/`.

**Managed alternative.** AWS ALB / CloudFront, GCP Cloud Run + Cloud
CDN, Cloudflare in front of the origin. If you swap nginx out, port
the gateway-auth check (`X-Gateway-Auth`) onto the new layer or you
lose that defence.

---

### certbot — TLS certificate renewals

**Role.** Issues + renews Let's Encrypt certs for both subdomains via
the webroot ACME challenge.

**Required?** Yes if you use the bundled nginx. Behind a managed LB,
the LB handles TLS — drop certbot.

**Bootstrap.** `./deploy/init-certs.sh you@yourdomain.com` (one-shot;
spins a temporary HTTP-only nginx so certbot has a place to land the
challenge before the real nginx is up).

**Managed alternative.** AWS Certificate Manager (free for ALB +
CloudFront), GCP managed certificates, Cloudflare-managed TLS.

---

### Email transport — Resend (or any SMTP)

**Role.** Verification emails on signup, password-reset links, invite
links to teammates.

**Required?** Optional — without it, the verification token gets
logged to the API console (grep for `verify` in `/tmp/plowered-run.log`).
That's fine for dev but unacceptable for production.

**Config.** `PLOWERED_RESEND_API_KEY=re_xxx` + `PLOWERED_EMAIL_FROM=…`.

**Managed alternative.** AWS SES, Postmark, SendGrid, Mailgun. The
`internal/core/email` package has a `Sender` interface — write a new
adapter, swap in.

---

### Vector store — pluggable per tenant

**Role.** Backs semantic search, the AI describer, `/ask`, similar-asset
lookups.

**Required?** Optional — the default is `pgvector` running inside the
same Postgres instance, so no separate infra. Tenants can switch to
Pinecone / Weaviate / Qdrant from `Management → Vector stores`.

**Sizing (pgvector default).** ~3 KB / asset embedding @ 768
dimensions. 100k assets → ~300 MB. Index build is roughly linear in
asset count.

**External backends.**

| Kind | Notes |
|---|---|
| Pinecone | Managed; needs API key + index name (`PLOWERED_PINECONE_*` per tenant config) |
| Weaviate | Self-hosted or managed; URL + API key |
| Qdrant | Self-hosted or Qdrant Cloud; URL + API key |

---

## Required versus optional, at a glance

| Component | Required? | Bundled image | Swap for |
|---|---|---|---|
| Postgres 16 | yes | `postgres:16-alpine` | RDS / Cloud SQL |
| Redis 7 | for durability | `redis:7-alpine` | ElastiCache / Memorystore |
| NATS JetStream | multi-process only | `nats:2-alpine` | Synadia Cloud / Kafka |
| MinIO | for durable blobs | `minio/minio:latest` | S3 / GCS / R2 |
| nginx | for HTTPS | `nginx:1.27-alpine` | ALB / Cloudflare |
| certbot | with bundled nginx | `certbot/certbot:latest` | ACM / GCP managed certs |
| Resend / SES | for production email | — | any SMTP provider |
| Vector store | search uses pgvector by default | none | Pinecone / Weaviate / Qdrant |

---

## Reference machine sizing

A single VPS that hosts the whole stack (the configuration the bundled
compose file targets):

| Use case | vCPU | RAM | Disk |
|---|---|---|---|
| Dev / demo | 2 | 4 GB | 40 GB |
| Single-tenant prod | 4 | 8 GB | 80 GB SSD |
| Small multi-tenant (≤10 workspaces) | 4-8 | 16 GB | 200 GB SSD |
| Growing (≥10 workspaces) | externalise the DB | — | — |

Past "small multi-tenant," you'll want to externalise Postgres + Redis
+ S3 onto managed services and keep only the app containers + nginx on
the VPS. The compose file's two-network split makes that an env-var
swap, not a refactor.

---

## What's deliberately NOT in this folder

- **The actual provisioning recipes** — they live in `deploy/`
  (`Dockerfile.*`, `nginx/*`, `init-certs.sh`, `secrets-gen.sh`,
  `docker-compose.production.yml`). This folder describes what's
  needed; that folder makes it happen.
- **Terraform / Helm / IaC modules** — not written yet. When the
  deploy graduates from "one VPS + compose" to "managed cloud," the
  IaC modules will live in `deploy/terraform/` and `deploy/helm/`.
  Tracked as a follow-up.
- **Monitoring / alerting recipes** — Prometheus is exposed at
  `/metrics`; everything downstream (Grafana, Honeycomb, PagerDuty)
  is BYO.
