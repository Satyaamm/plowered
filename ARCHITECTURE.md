# Architecture

## Components

```
                     ┌──────────────────────────────────────────────┐
                     │              Web UI (Next.js 15.5)           │
                     │   Catalog · Lineage · Pipelines · Contracts  │
                     │   Cost · Certifications · Alerts · Ask · DSR │
                     └───────────────────┬──────────────────────────┘
                                         │ REST + cookies
                                         ▼
   MCP clients ──────►  ┌────────────────────────────────────────┐
                        │       Plowered API + scheduler         │
                        │  ┌──────────────────────────────────┐  │
                        │  │  REST handlers (/v1/*)            │  │
                        │  │  · catalog · lineage · glossary  │  │
                        │  │  · classify · ask · jobs         │  │
                        │  │  · certifications · contracts    │  │
                        │  │  · cost · notify · migrations    │  │
                        │  │  · pipelines · runs · checks     │  │
                        │  │  · legal holds · DSR · audit     │  │
                        │  │  · accounts · sessions · team    │  │
                        │  │  · MCP (HTTP/SSE)                │  │
                        │  └──────────────────────────────────┘  │
                        │              │                         │
                        │              ▼                         │
                        │  ┌──────────────────────────────────┐  │
                        │  │           Core Engine            │  │
                        │  │  catalog · contract · cost ·     │  │
                        │  │  certification · classifier ·    │  │
                        │  │  search · auth · policy · audit  │  │
                        │  │  migration · profile · asker     │  │
                        │  └──────────────────────────────────┘  │
                        └───┬─────────┬────────┬─────────┬───────┘
                            │         │        │         │
                            ▼         ▼        ▼         ▼
                  ┌────────────┐ ┌───────┐ ┌──────────┐ ┌──────────────┐
                  │ PostgreSQL │ │ Redis │ │ events.  │ │ ObjectStore  │
                  │  (pgx/v5)  │ │ Asynq │ │  Bus +   │ │   (S3 / FS)  │
                  │ + outbox   │ │       │ │  NATS    │ │              │
                  │ + vector   │ │       │ │  outbox  │ │              │
                  └────────────┘ └───┬───┘ └──────┬───┘ └──────────────┘
                                     │            │
                            ┌────────▼────────┐   │
                            │ plowered-worker │   │
                            │  pipeline runs  │   │
                            │  quality checks │   │
                            │  crawl / class. │   │
                            │  async migration│   │
                            │  contract eval  │   │
                            │  notify dispatch│◄──┘
                            └─────────────────┘
```

## Data model

### Nodes
- `Asset` — `database`, `schema`, `table`, `view`, `column`, `dashboard`,
  `report`, `transform_model`, `pipeline`, `ml_model`
- `GlossaryTerm`
- `User` / `Group`
- `Tag` / `Classification`
- `Certification` (proposed → approved | rejected → revoked)
- `Contract` (per-asset expectations: columns, freshness, null thresholds)

### Edges
- `LINEAGE` (asset → asset, directed)
- `OWNED_BY` (asset → user/group)
- `TAGGED_AS` (asset → tag)
- `DEFINES` (term → asset; also schema → column for catalog walks)
- `DEPENDS_ON` (asset → asset)

### Common envelope
`id (uuid)`, `qualified_name`, `tenant_id`, `type`, `created_at`,
`updated_at`, `properties (jsonb)`, `embedding (vector)`.

## Storage

- **Production:** PostgreSQL 16 — assets, edges, glossary_terms, tags,
  policies, audit_events, sessions, jobs, certifications, contracts,
  cost_records, cost_budgets, notify_*, plus the outbox table for
  cross-process events. Graph queries via recursive CTEs.
  Vector / embedding column on `asset_embeddings`.
- **Dev / tests:** in-memory MemoryRepo, identical interface.
- **Object storage:** `blob.ObjectStore` (Put / Get / Delete / SignedURL)
  with S3 + local-FS backends. Mirrors profile output, AI-request /
  response transcripts, DSR exports, and migration watermarks.
- **Search:** local deterministic embeddings out of the box; switchable
  to any BYOM provider via the AI providers page.
- **Cache / queue:** Redis (Asynq queue + idempotency cache +
  rate-limit counters).

## Async + eventing

- **events.Bus** — in-process pub/sub. Every domain change publishes (eg
  `AssetCreated`, `CheckFailed`, `ContractBreach`). The notify dispatcher,
  contract runner, cost watcher, and outbox relay all subscribe.
- **outbox** — every state change writes a row in the same Postgres TX. A
  background relay reads `processed_at IS NULL` and forwards to NATS /
  JetStream. At-least-once delivery without distributed TX.
- **Asynq** (Redis Streams) — pipeline runs, quality checks, crawls,
  classify, reindex, long migrations.
- **Notify dispatcher** — bounded async worker pool
  (`PLOWERED_NOTIFY_WORKERS` / `_QUEUE_SIZE`) with inline fallback on
  full-queue (logged warn). Channels: Slack, Email (Resend), Webhook
  (SSRF-guarded), Log.

## Periodic workers (in the API process under `leases.scheduler`)

| Worker | Cadence | Purpose |
|---|---|---|
| `contract.Runner` | 5 min (configurable) | Evaluate contracts, emit dedup'd breach events |
| `cost.Watcher` | 5 min | Roll up cost records, fire warn/hard threshold events (24h dedupe) |
| Outbox relay | continuous | Drain `outbox` → NATS |
| Scheduler | continuous | Fire pipeline schedules; lease-protected so only the leader ticks |
| Reaper | 30s | Reap orphaned `pipeline_runs` / `task_runs` past their heartbeat |

## Context Pipeline

```
UNIFY ──► BOOTSTRAP ──► COLLABORATE ──► ACTIVATE
```

- **UNIFY** — `internal/adapters/*` (Postgres, Snowflake, Redshift,
  Mongo, Dynamo, Athena, BigQuery) + `internal/core/catalog`
- **BOOTSTRAP** — `internal/core/{classifier,describer,profile,asker}`
- **COLLABORATE** — `internal/core/certification` (propose / approve /
  reject / revoke) + review queues + audit chain
- **ACTIVATE** — `cmd/plowered-mcp` + REST APIs + warehouse-side
  pushdown (cost, contract checks)

## API surface

REST over `/v1/*` with OpenAPI 3.1 at `/openapi.yaml`, Swagger UI at
`/docs`. Cookie-authenticated browser flow, bearer-authenticated
service flow.

Endpoint families:
- `auth`, `account`, `sessions`, `team`, `identity`
- `catalog`, `assets`, `glossary`, `classifications`, `tags`
- `pipelines`, `runs`, `task-runs`, `jobs`, `migrations`
- `checks`, `quality-runs`, `contracts`, `notify`, `alerts`
- `certifications`, `cost` (with `?by_feature=1`)
- `legal-holds`, `dsr`, `audit`, `recycle-bin`
- `connections`, `aiproviders`, `ask` (with history)
- `mcp` (HTTP/SSE)

## Connector framework

```go
type Tester interface { Test(ctx, Config) error }
type Crawler interface { Crawl(ctx, Config, Sink) error }
type Executor interface { Execute(ctx, Plan) (Result, error) }
```

Each adapter package implements the interfaces it supports. Tester is
called by the connection-form's Test button before a connection is
persisted (silent — wizard renders inline result UI). Crawler is the
async worker that walks `information_schema` (or Mongo collections /
Dynamo tables / Athena workgroups / BigQuery datasets). Executor runs
migration plans + warehouse-side checks.

## AI context layer

- DescriptionAgent — column auto-describe; mirrors transcripts to S3
- ProfileAgent — per-asset chart-ready snapshots
- AskerAgent — natural-language Q&A over the catalog; history persisted
- ClassifierAgent — sample-based PII / PHI / PCI / secret tagging
- (planned) MetricAgent, GlossaryAgent, EvalAgent

Provider abstraction: `pkg/llm`. Prompts versioned. Every generation
logged to `ai_query_executions` with token counts + latency, picked up
by `cost.Recorder`.

## MCP

Tools:
- `search_assets`
- `get_asset`
- `get_lineage`
- `get_glossary_term`
- `propose_query`

Transports: stdio (`plowered-mcp`) and HTTP/SSE (mounted on the API).

## Multi-tenancy

- Row-level isolation: `tenant_id NOT NULL` on every domain table.
  Every handler goes through `mustTenant`; every storage method extracts
  via `storage.TenantFromContext`. Every compound index leads with
  `tenant_id`.
- Schema-per-tenant available for regulated customers via tenant
  `tier='hipaa'`.
- RBAC verbs: `read`, `edit`, `propose`, `certify`, `delete`, `admin`.
  Roles: `viewer`, `editor`, `steward`, `admin`, `super_admin`.

## Cost tracking

`cost.Recorder` writes a `cost_records` row for every billable call
(LLM tokens, warehouse seconds, S3 bytes). `cost.Watcher` rolls them
up against per-tenant `cost_budgets` and fires `CheckFailed` events at
warn + hard thresholds. The UI's `/cost` page reads
`GET /v1/cost/summary?by_feature=1`.

## Performance budget

| Operation | Target |
|---|---|
| Asset lookup by qualified name | < 5ms p99 |
| Search (10 results, 1M assets) | < 100ms p99 |
| 3-hop lineage (100 nodes) | < 200ms p99 |
| Full crawl, 100K-asset warehouse | < 30 min |
| Cold-start single binary | < 2s |
| Memory at idle (10K assets, in-memory) | < 200MB |
| Contract runner tick (per tenant) | < 1s |
| Notify dispatch (per event, p99) | < 500ms |
