# Plowered

A **data context platform** — a catalog + governance + lineage layer that data
teams (and the AI agents that act on their behalf) can trust. Go-native,
single-binary, BYO-LLM.

## What's in the box

| Surface | What ships today |
|---|---|
| **Catalog** | Assets, qualified-name lookups, lineage edges, glossary terms, search, classifications, owners, tags |
| **Auth** | Email + password signup (Argon2id), email verification, sessions (HttpOnly cookies, server-side rows, revocable), forgot-password + reset flow, account lockout after 5 fails in 15 min, invite-based teammate onboarding, 5-role RBAC |
| **Account self-service** | Profile editor, change-password (kills all sessions), active sessions list with "sign out everywhere", GDPR export + erasure under `/v1/account/*` |
| **Orchestration** | Pipelines, scheduled runs, run logs, quality checks, audit log |
| **Async jobs** | Asynq (Redis Streams) or in-process sync mode. Bookmarkable per-job detail page at `/jobs/{id}`; API at `GET /v1/jobs/{id}` |
| **Connectors** | Postgres (live), Snowflake (live), BigQuery (scaffold; activate by registering the client driver). SSRF guard on every outbound URL |
| **AI / BYOM** | Bring your own Anthropic, OpenAI, DeepSeek or OpenAI-compatible endpoint. Keys sealed in the secrets vault. Test button on the settings page validates credentials |
| **Search** | Local deterministic embeddings out of the box; switch to any BYOM provider via the AI providers page |
| **MCP** | Native Model Context Protocol server (`cmd/plowered-mcp`) so any MCP-aware agent can read the catalog under your policy engine |
| **Rate limiting** | Per-IP token bucket on auth endpoints + per-principal limiter on the authenticated API (120 read / 30 write / min). RFC 9239 `RateLimit-Limit / Remaining / Reset` headers everywhere |
| **Security headers** | HSTS, strict CSP, `X-Frame-Options: DENY`, Referrer-Policy, Permissions-Policy lockdown, COOP + CORP — set globally by `SecurityHeadersMW` |
| **Compliance** | Legal holds, admin DSR + self-service GDPR (Art. 15/17/20), audit trail, recycle bin. See `COMPLIANCE.md` for the SOC 2 / GDPR / HIPAA control matrix |
| **API** | REST + JSON over `/v1/*`. OpenAPI 3.1 spec served at `/openapi.yaml` and Swagger UI at `/docs` |

## Tech stack

| Layer | Choice |
|---|---|
| Core engine | Go 1.24 |
| API | net/http + OpenAPI 3.1 (Swagger UI at `/docs`) |
| Metadata store | PostgreSQL 16 (pgx/v5). In-memory mode for dev/tests. |
| Async jobs | Asynq (Redis Streams). Falls back to an in-process sync enqueuer when `PLOWERED_REDIS_URL` is unset. |
| Secrets | AES-256-GCM sealed envelopes; key from `PLOWERED_SECRETS_MASTER_KEY` |
| Email | Resend (production) or `LogSender` (dev — link prints to logs) |
| Frontend | Next.js 14 + TypeScript + Fluent UI v9 ("Loamy" theme) |
| Observability | Prometheus metrics at `/metrics`, OTel tracing hooks |
| AuthN | Argon2id passwords, server-side sessions (HttpOnly + Secure + SameSite=Lax), 14-day TTL, revocable from `/account`. OIDC / SSO adapter slot reserved. |
| Rate limiting | `golang.org/x/time/rate` token buckets; per-IP on auth, per-principal on the rest |
| HTTP hardening | `SecurityHeadersMW` — HSTS / CSP / X-Frame-Options / Referrer-Policy / Permissions-Policy / COOP / CORP |
| Deployment | Single binary, container, or three-process split (api / worker / mcp). |

## Binaries

| Binary | Purpose |
|---|---|
| `cmd/plowered` | HTTP + gRPC API process. Embeds the scheduler. |
| `cmd/plowered-worker` | Asynq consumer for pipeline runs, quality checks, crawls, classify, reindex |
| `cmd/plowered-mcp` | Model Context Protocol server |
| `cmd/ploweredctl` | CLI for migrations + admin chores |

## Repo layout

```
plowered/
├── cmd/
│   ├── plowered/             # API + scheduler binary
│   ├── plowered-worker/      # Async job consumer
│   ├── plowered-mcp/         # MCP server
│   └── ploweredctl/          # CLI
│
├── internal/
│   ├── core/                 # Domain packages (auth, catalog, glossary, jobs,
│   │                         #   aiprovider, classifier, search, policy, …)
│   ├── adapters/
│   │   ├── postgres_source/  # Postgres customer-DB adapter (Tester + Crawler)
│   │   ├── snowflake_source/ # Snowflake adapter (driver via blank-import)
│   │   └── bigquery_source/  # BigQuery scaffold (Driver interface + SetActive)
│   ├── api/http/             # REST handlers + middleware + OpenAPI spec
│   ├── storage/
│   │   ├── postgres/         # pgxpool stores + embedded migrations
│   │   └── memory/           # In-memory stores for dev/tests
│   ├── worker/               # Asynq + Sync enqueuers + handlers
│   └── server/               # Listener wiring (gRPC + HTTP) + middleware chain
│
├── pkg/
│   └── llm/                  # Provider-agnostic LLM interface; local + mock impls
│
├── web/                      # Next.js frontend
└── deploy/                   # Container + compose recipes
```

## Quickstart (local dev)

```bash
# 1. Tools
brew install go postgresql@16 redis node

# 2. Boot Postgres + Redis (optional — sync mode works without Redis)
brew services start postgresql@16
brew services start redis
createdb plowered

# 3. Configure
export PLOWERED_DATABASE_URL=postgres://localhost/plowered?sslmode=disable
export PLOWERED_REDIS_URL=redis://localhost:6379/0
export PLOWERED_SECRETS_MASTER_KEY=$(openssl rand -base64 32)
export PLOWERED_WEB_BASE_URL=http://localhost:3000
# Optional: email
# export RESEND_API_KEY=re_xxx
# export PLOWERED_FROM_ADDRESS="Plowered <noreply@yourdomain>"

# 4. Run
go mod tidy
go run ./cmd/plowered serve            # api on :8080
go run ./cmd/plowered-worker           # worker (only needed with Redis)

# 5. Web
cd web && pnpm install && pnpm dev      # http://localhost:3000
```

Without `PLOWERED_DATABASE_URL`, the API runs in **memory mode** — no
persistence, no async, but every endpoint responds. Useful for `pnpm dev`
flows that don't want a Postgres container.

## API exploration

- **Swagger UI** → http://localhost:8080/docs
- **Raw OpenAPI** → http://localhost:8080/openapi.yaml (use with `openapi-typescript`, `oapi-codegen`, Postman, Insomnia)
- **Metrics** → http://localhost:8080/metrics (Prometheus exposition)

## BYOM (bring your own model)

A tenant admin goes to **Management → AI providers**, picks a kind
(Anthropic / OpenAI / DeepSeek / openai-compatible), pastes an API key, and
clicks **Test**. The platform performs a `GET /v1/models` against the upstream
(burns no tokens) and unlocks Save on success. Keys are sealed in the AES vault
under `urn:plowered:aiprovider:<id>`.

Marking a config primary for `chat` or `embed` makes it the tenant default for
the corresponding feature (glossary auto-write, semantic search, etc.).

## Snowflake & BigQuery

Snowflake works out of the box with the gosnowflake driver. To enable, add a
blank import to your `cmd/plowered/main.go` and `cmd/plowered-worker/main.go`:

```go
import _ "github.com/snowflakedb/gosnowflake"
```

BigQuery ships as a scaffold. To activate, add `cloud.google.com/go/bigquery`
to your deployment + register a concrete driver in a wiring package:

```go
bigquery_source.SetActive(yourDriver)
```

## Teams & invites

Admins invite teammates from **Management → Team**. The invitee gets a 7-day
link that opens `/accept-invite?token=…`, where they pick a password and join.
Their email is treated as proof-of-ownership — no separate verification step.

Roles in v0: `viewer`, `editor`, `steward`, `admin` (plus `super_admin` for the
workspace creator). Role changes and removals are admin-only; self-removal is
blocked at the API level.

## Async jobs

Long-running things (classify a connection, reindex search, crawl a warehouse)
return `202 Accepted` + `{job_id}` from the HTTP layer. The frontend polls
`GET /v1/jobs/{id}` every 2 s for progress / status / result. The durable
ledger lives in the `jobs` Postgres table; the executor side runs on Asynq.

## Compliance posture

- **PII / PCI / PHI tagging**: sample-based classifier writes `class:*` tags
  onto columns; quality + policy engines act on them.
- **Legal holds**: any asset under hold cannot be soft-deleted or hard-deleted.
- **Admin DSR**: per-subject delete/export flows tracked with statutory clocks
  via `/v1/dsr/*` for regulator-driven requests.
- **Self-service GDPR**: `GET /v1/account/export` (Art. 15 / 20 JSON bundle)
  and `DELETE /v1/account?confirm=true` (Art. 17 pseudonymisation — PII
  blanked, `users.id` retained for audit history under the Art. 17(3)(b)
  legal-obligation exemption).
- **Audit**: every write is recorded in `audit_events`; retention is per-tenant.
- **Cookies + sessions**: HttpOnly, Secure, SameSite=Lax; server-side rows so
  any session can be revoked instantly; password change or reset revokes every
  active session for the user.
- **Rate-limit headers**: every limited endpoint emits `RateLimit-Limit`,
  `RateLimit-Remaining`, `RateLimit-Reset` per IETF draft RFC 9239.

See [`COMPLIANCE.md`](COMPLIANCE.md) for the full SOC 2 / GDPR / HIPAA
control matrix and the honest gap list.

## Tests

```bash
go test ./...                        # all Go tests
go test ./internal/api/http/...      # API tests (memory-backed)
cd web && pnpm typecheck             # web type-check
```

The consolidated e2e test (`internal/api/http/e2e_test.go`) exercises
signup → invite → accept → list members in-process with the MemoryRepo.

## Project status

Earlier seven feature tracks (foundations):

1. Real signup form with full validation + per-field strength meter
2. OpenAPI 3.1 spec + embedded Swagger UI
3. Async classify + reindex via Asynq + durable jobs table
4. BYOM (Anthropic / OpenAI / DeepSeek / openai-compatible) + Test button
5. Teams + invitations end-to-end
6. Snowflake adapter (live) + BigQuery scaffold
7. Consolidated in-process e2e test

Recent security + compliance pass (SOC 2 / GDPR / HIPAA prep):

1. Forgot-password / reset-password flow (BE + FE) with session revocation
2. Account self-service page — profile, password change, active sessions,
      "sign out everywhere"
3. Account lockout after 5 failed logins in a 15-min window
      (OWASP / NIST 800-63B)
4. Per-IP auth rate limit + per-principal API limit (120 read / 30 write
      per min) with RFC 9239 `RateLimit-*` headers on both tiers
5. Bookmarkable `/jobs/{id}` detail page with progress bar + status badge
6. `SecurityHeadersMW` — HSTS, strict CSP, X-Frame-Options DENY,
      Referrer-Policy, Permissions-Policy, COOP + CORP globally
7. GDPR self-service: `GET /v1/account/export` (Art. 15 / 20),
      `DELETE /v1/account?confirm=true` (Art. 17 pseudonymisation)
8. `COMPLIANCE.md` updated with the new controls and a real gap document
      covering remaining code + operational work (MFA, SSO, BAA, Type II
      audit window, pen test, Trust Center, etc.)

## Contact

Maintainer: [@Satyaamm](https://github.com/Satyaamm)
