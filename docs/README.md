# Plowered docs

This directory is the canonical home for human-facing documentation. The
OpenAPI spec lives next to the handlers it documents at
`internal/api/http/openapi.yaml` (it's embedded into the API binary via
`go:embed`); edit it there.

## Browse the API

When the API server is running:

- **`/docs`** — Swagger UI. Try-it-out works for logged-in sessions (the
  page sends cookies).
- **`/openapi.yaml`** — the raw spec; feed it to `openapi-typescript`,
  `oapi-codegen`, Postman, Insomnia, etc.

## Endpoint surface (v0)

Grouped by tag — full request/response shapes live in the OpenAPI spec.

### auth
- `POST /v1/auth/signup` — create tenant + admin user, emails a verify link
- `GET  /v1/auth/verify?token=…` — flip `email_verified_at`
- `POST /v1/auth/resend-verification`
- `POST /v1/auth/login` — session cookie
- `POST /v1/auth/logout`
- `GET  /v1/auth/me`
- `GET  /v1/auth/invite-info?token=…` — public preview of an invite
- `POST /v1/auth/accept-invite` — public; creates teammate + membership

### team
- `GET    /v1/members`
- `PATCH  /v1/members/{user_id}`
- `DELETE /v1/members/{user_id}`
- `GET    /v1/invites[?include=all]`
- `POST   /v1/invites`
- `DELETE /v1/invites/{id}`

### catalog
- `GET /v1/assets`, `POST /v1/assets`, `GET /v1/assets/{id}`,
  `PATCH /v1/assets/{id}`, `DELETE /v1/assets/{id}`
- `GET /v1/assets:byQualifiedName`
- `POST /v1/assets:search`
- `GET /v1/assets/{id}/lineage`, `GET /v1/assets/{id}/column-lineage`
- `GET /v1/assets/{id}/classifications`

### connections
- `GET|POST|PATCH|DELETE /v1/connections[/{id}]`
- `POST /v1/connections/{id}/test`
- `POST /v1/connections/{id}/crawl` — 202 + Asynq job
- `POST /v1/connections/{id}/classify` — 202 + jobs ledger row

### orchestration
- Pipelines: `GET|POST|PATCH|DELETE /v1/pipelines[/{id}]`,
  `POST /v1/pipelines/{id}:trigger`
- Runs: `GET /v1/runs`, `GET /v1/runs/{id}`, `GET /v1/runs/{id}/logs`,
  `GET /v1/runs/{id}/logs:tail` (SSE)
- Jobs ledger: `GET /v1/jobs[?limit=]`, `GET /v1/jobs/{id}`

### quality
- `GET|POST|PATCH|DELETE /v1/checks[/{id}]`
- `POST /v1/checks/{id}:run`
- `GET /v1/checks/{id}/runs`

### governance
- Glossary terms: `GET|POST|PATCH|DELETE /v1/glossary/terms[/{id}]`
- Policies, access, audit log, recycle bin, legal holds, DSR.

### ai
- `POST /v1/search:semantic`, `POST /v1/search:reindex` (202 + jobs)
- BYOM: `GET|POST|PATCH|DELETE /v1/ai/providers[/{id}]`,
  `POST /v1/ai/providers/{id}/test`,
  `POST /v1/ai/providers/{id}/primary`,
  `POST /v1/ai/providers:test` (pre-save probe)

## Architecture pointers

| Topic | Where the code lives |
|---|---|
| HTTP middleware chain | `internal/server/server.go` (auth + tenant + audit + rate-limit + recovery) |
| Auth + tenancy context | `internal/core/auth` (`auth.Principal`, `auth.WithPrincipal`) |
| Storage stores | `internal/storage/postgres/*.go` (Postgres) + `internal/storage/memory/*.go` (in-memory) |
| Migrations | `internal/storage/postgres/migrations/*.sql` (embedded; numbered; up/down pairs) |
| Async work | `internal/worker/{asynq,sync,handlers,types}.go`; jobs ledger in `internal/core/jobs` |
| Secrets | `internal/core/secrets` (AES-256-GCM sealed envelopes, Memory + Postgres backends) |
| RBAC | `internal/core/policy` (5 roles + tag-based ABAC); HTTP gates use `principal.Roles` directly for now |
| BYOM | `internal/core/aiprovider` + Postgres store + per-kind HTTP adapters |
| Customer-DB adapters | `internal/adapters/{postgres,snowflake,bigquery}_source` |

## Operating modes

- **In-memory** (no `PLOWERED_DATABASE_URL`): every endpoint works, nothing
  persists, sync enqueuer runs all jobs inline. Useful for `pnpm dev` flows.
- **Postgres-only**: real persistence, sync enqueuer for jobs.
- **Postgres + Redis**: production. Pipelines / quality / crawl / classify /
  reindex run on `plowered-worker`. API stays fast.

## Environment variables

| Var | Purpose | Required |
|---|---|---|
| `PLOWERED_DATABASE_URL` | Postgres DSN | Yes for persistence |
| `PLOWERED_REDIS_URL` | Asynq broker | Optional |
| `PLOWERED_SECRETS_MASTER_KEY` | AES key for the vault (base64-encoded 32 bytes) | Yes in prod |
| `PLOWERED_WEB_BASE_URL` | Used in email links (verify, invite) | Recommended |
| `PLOWERED_FROM_ADDRESS` | Outbound email From | Recommended |
| `RESEND_API_KEY` | Resend transactional email | Optional (LogSender fallback) |
| `PLOWERED_ENV` | `production` enables fail-closed secrets | Optional |
| `PLOWERED_SCHEDULER_DISABLED` | `1` disables in-process scheduler | Optional |
| `PLOWERED_WORKER_CONCURRENCY` | Asynq worker concurrency | Optional |

## Signup validation rules

Both the web form and `validateSignup()` in `internal/api/http/auth.go`
enforce these in lockstep. If they drift, the server wins.

| Field | Rule |
|---|---|
| `email` | Trimmed + lowercased server-side. Must contain `@` and a `.`. Max 256 chars. |
| `password` | 8+ chars, max 256. At least 3 of: lowercase, uppercase, digit, symbol. |
| `confirm_password` | Optional; if supplied must equal `password`. |
| `first_name` | Letters / marks / spaces / `' . - ,` only. Max 64. No digits. |
| `last_name` | Same rules as `first_name`. Max 64. |
| `phone` | Optional. 6–15 digits after stripping spaces and dashes. |
| `phone_country` | Required when `phone` is set. Pattern `^\+\d{1,4}$`. |
| `workspace_name` | 2–64 chars after trim. |
| `workspace_slug` | Optional; auto-generated from name when blank. |

## Adding a new endpoint — checklist

1. Implement the handler in `internal/api/http/<feature>.go`.
2. Register on the mux in the right `<feature>Handlers(mux, …)` function.
3. Add it to `NewMux` in `internal/api/http/server.go` if it needs new deps.
4. Document it in `internal/api/http/openapi.yaml` — request, response,
   error shapes. Run the API and open `/docs` to sanity-check.
5. Add the matching `useX` hook in `web/src/lib/hooks/`.

## Adding a new migration

1. `internal/storage/postgres/migrations/00NN_<name>.up.sql` + `.down.sql`.
2. The file is auto-picked up by the embedded loader. Migrations are
   applied in lexical order on every boot; applied rows are tracked in
   `schema_migrations` so reruns are no-ops.

## Adding a new BYOM provider

1. Append a `Kind` constant in `internal/core/aiprovider/aiprovider.go`.
2. Implement an adapter in `aiprovider/adapters.go` (or split into a new file
   if it grows). Match the `llm.Provider` interface.
3. Wire it into the `Build` + `Test` switches and into `AllKinds`.
4. Surface it in the web `KIND_LABELS` + `SUGGESTED_MODELS` maps.

## Adding a new connector adapter

1. New `internal/adapters/<source>_source/` package with `Tester` + `Crawler`.
2. Register both in `cmd/plowered/main.go` (Tester) and
   `cmd/plowered-worker/main.go` (Crawler).
3. Enable the type in the web connection wizard and add a config form
   section for it.
