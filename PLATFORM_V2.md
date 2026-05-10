# Plowered Platform — v2 Architecture

This document is the single reference for the v2 platform: the upgraded
architecture, the canonical tech stack, the edge cases each subsystem must
survive, the use cases the product solves, and the SOC 2 / GDPR / HIPAA
controls baked into the design.

It supersedes overlapping sections in `ARCHITECTURE.md`,
`ORCHESTRATION.md`, and `COMPLIANCE.md`. Where those go deeper into a
single subsystem they remain authoritative; where they conflict, this
document wins.

---

## 1. What Plowered does

Plowered is an **open data context platform**. It plugs into a customer's
warehouses, lakes, queues, and transform tools, then continuously builds:

1. A **catalog** of every asset (tables, columns, dashboards, models, ML
   features, ML models, files, topics).
2. A **lineage graph** linking the assets via parsed SQL, transform DAGs,
   and runtime traces.
3. A **quality layer** that runs configurable checks (row count, freshness,
   nulls, uniqueness, custom SQL) against each asset on a schedule or in
   reaction to upstream events.
4. An **orchestration layer** that runs ETL/ELT pipelines, retries
   intelligently, and reacts to failures in real time.
5. A **policy layer** that enforces who can read, edit, certify, run, or
   delete each asset, and produces an immutable audit trail.
6. An **agent / API surface** that exposes all of the above to humans
   (web UI), to LLMs (MCP server), and to other services (gRPC + REST).

### 1.1 Use cases this unlocks

| Persona               | Use case                                                                                |
| --------------------- | --------------------------------------------------------------------------------------- |
| Data engineer         | Find every downstream table that breaks if I drop this column                           |
| Analytics engineer    | Get notified within 60s when a freshness check fails on a critical mart                 |
| Platform engineer     | Run ETL/ELT pipelines with retries, fan-out, and dead-letter queues across regions      |
| Data steward          | Certify a table, attach a definition, and require approval before edits                 |
| Compliance officer    | Prove every PII column is tagged, accessed only by authorized roles, with an audit log  |
| Security engineer     | Enforce tenant isolation + RBAC + row-level filters with an append-only audit feed      |
| ML engineer           | Trace a model's prediction back to the exact training table, column, and pipeline run   |
| Product manager       | Read a Slack alert on a failed pipeline run with the root-cause task and stack trace    |
| LLM agent             | Ask "which tables describe revenue?" via MCP and get governed answers with provenance   |
| FinOps / leadership   | See unused / stale assets, top-cost transforms, broken pipelines per team               |

The same core handles all of these because every concept is exposed as a
**typed API** with **uniform RBAC, lineage, audit, and quality**.

---

## 2. Canonical tech stack (chosen for scale + perf)

We deliberately pick boring, battle-tested infrastructure with proven
horizontal-scale stories. Every choice has a fallback documented under §10.

### 2.1 Backend

| Concern               | Choice                                  | Why                                                                         |
| --------------------- | --------------------------------------- | --------------------------------------------------------------------------- |
| Language              | **Go 1.23**                             | Static binaries, low GC, mature stdlib, best concurrency story for an API + worker tier |
| RPC                   | **gRPC + Connect** (`connect-go`)       | Single proto contract serves gRPC, gRPC-Web, and JSON; streaming first-class |
| HTTP edge             | **Echo v4** (`labstack/echo`)           | Fast routing, middleware ecosystem, zero-alloc binding                      |
| DB driver             | **pgx/v5** + **sqlc**                   | Native protocol, prepared statements, type-safe generated queries           |
| Migrations            | **golang-migrate**                      | Versioned, down-migration safe, runs in-process for embedded mode           |
| Queue / async jobs    | **Asynq** (Redis Streams)               | Go-native, at-least-once, retries, scheduled tasks, priorities, dashboards  |
| Long-running orch.    | **Temporal** (optional, large tenants)  | Durable execution for >1h pipelines without re-implementing replay          |
| Event bus (in-proc)   | `internal/core/events`                  | Synchronous fan-out for embedded mode                                       |
| Event bus (cluster)   | **NATS JetStream**                      | Lightweight, K8s-native, exactly-once on a subject; Kafka if customer requires |
| Time-series / metrics | **ClickHouse**                          | Cheap columnar store for run logs, check history, lineage events            |
| Cache                 | **Redis 7** (cluster)                   | TTL'd query results, idempotency keys, rate-limit counters, queue backplane |
| Object storage        | **S3-compatible** (MinIO local, S3 prod) | Audit log archives (object lock), task-run logs, large check results        |
| Search                | **PostgreSQL FTS** → **OpenSearch**     | Start with `tsvector`/trigram; switch when corpus > 50M assets              |
| Vector search         | **pgvector** → **Qdrant**               | Embed for catalog search; keep in Postgres until > 5M vectors               |
| Observability         | **OpenTelemetry → Prometheus + Grafana + Loki + Tempo** | Single SDK feeds metrics/logs/traces; vendor-neutral                        |

### 2.2 Frontend

| Concern               | Choice                                                              |
| --------------------- | ------------------------------------------------------------------- |
| Framework             | **Next.js 15 App Router**, React 19, Server Components first         |
| UI primitives         | **Fluent UI v9** with the Loamy brand tokens                         |
| Data fetching         | **TanStack Query v5** (`@tanstack/react-query`) for caching + SWR    |
| Forms                 | **react-hook-form** + **Zod** schemas shared with the Go server      |
| Tables                | **TanStack Table v8** + **TanStack Virtual** for 100k+ rows          |
| Graph viz             | **React Flow v12** for lineage; **Recharts** for time-series         |
| State (client only)   | **Zustand** for cross-page UI state; React Context for theme         |
| Realtime              | **Server-Sent Events** for run/check status; WebSocket fallback      |
| Auth client           | **next-auth** with OIDC providers; HttpOnly cookies                  |
| Build / runtime       | **Turbopack** dev, **SWC** prod, **Bun** for scripts                 |
| Type-safe RPC         | Generated **connect-web** clients from the same proto                |
| Testing               | **Vitest** + **Testing Library**, **Playwright** for e2e             |

### 2.3 Platform / infra

| Concern               | Choice                                                              |
| --------------------- | ------------------------------------------------------------------- |
| Container             | Distroless `gcr.io/distroless/static-debian12` base                  |
| Orchestration         | **Kubernetes** + **Helm** chart (provided)                          |
| Service mesh (opt.)   | **Linkerd** for mTLS; lighter than Istio                             |
| Secrets               | **External Secrets Operator** + AWS Secrets Manager / GCP SM / Vault |
| CI / CD               | **GitHub Actions**, **Trivy** (CVE), **Syft** (SBOM), **Cosign** (sign) |
| Migrations            | Pre-deploy Job (Helm hook) running `migrate up`                      |
| Backup                | `pgBackRest` to S3, point-in-time recovery, 35-day retention         |
| DR                    | Cross-region read replica + warm standby; quarterly failover drill   |

---

## 3. Service topology

```
                   ┌──────────────────────────────────────────────┐
   browser ──TLS──▶│  next.js edge (vercel/k8s)                   │
                   │   ⤷ React Server Components + connect-web    │
                   └──────────────────────────────────────────────┘
                               │  HTTPS / SSE
                               ▼
                   ┌──────────────────────────────────────────────┐
   CLI / SDK ─────▶│  api gateway (echo + connect-go)             │
   MCP clients     │   ⤷ auth, rate-limit, tenant ctx, audit hook │
                   └────────────────────┬─────────────────────────┘
                                        │ gRPC
              ┌─────────────────┬───────┴────────┬─────────────────┐
              ▼                 ▼                ▼                 ▼
       ┌────────────┐    ┌────────────┐   ┌────────────┐    ┌────────────┐
       │ catalog    │    │ pipelines  │   │ quality    │    │ policy &   │
       │ service    │    │ runner     │   │ runner     │    │ audit svc  │
       └─────┬──────┘    └─────┬──────┘   └─────┬──────┘    └─────┬──────┘
             │                 │                │                 │
             ▼                 ▼                ▼                 ▼
       ┌─────────────────────────────────────────────────────────────┐
       │ Postgres (catalog + meta) │ ClickHouse (runs/checks history)│
       │ Redis (queues + cache)    │ S3 (logs + audit archive)       │
       │ NATS JetStream (events)   │ pgvector / Qdrant (search)      │
       └─────────────────────────────────────────────────────────────┘
                                        ▲
                                        │
                            ┌───────────┴────────────┐
                            ▼                        ▼
                     ┌────────────┐          ┌────────────────┐
                     │ workers    │          │ connectors     │
                     │ (asynq)    │          │ (warehouse,    │
                     │            │          │  transform,    │
                     │            │          │  bi)           │
                     └────────────┘          └────────────────┘
```

Every service is **horizontally scalable** and **stateless**; durability
lives in Postgres / Redis / S3 / NATS / ClickHouse. The API gateway and
runners scale on CPU; workers scale on queue depth.

---

## 4. Design patterns we follow

A canonical list — every new module is reviewed against this checklist.

1. **Hexagonal / ports-and-adapters.** `internal/core/<domain>` defines
   interfaces (`Store`, `Channel`, `Executor`, `Bus`, `Writer`). Adapters
   live in `internal/adapters/<impl>` (postgres, memory, redis, http).
   Tests use the memory adapter; prod uses the real one.
2. **Dependency injection via constructors.** No service locators, no
   globals except `slog.Default()`. Everything wired in `cmd/server/main.go`.
3. **Repository pattern** for storage; `sqlc`-generated queries are private
   to the repo.
4. **Outbox pattern** for events that must survive crashes — pipeline runs
   write a `pipeline_events` row in the same TX as the state change; a
   relay job fans out to NATS / webhook.
5. **Saga / compensating actions** for multi-step pipelines: each task
   declares an optional `Rollback` so a partial failure can unwind.
6. **Circuit breaker + bulkhead** on every outbound call (warehouse query,
   webhook delivery, LLM completion). `sony/gobreaker` + per-tenant
   semaphore.
7. **Backpressure everywhere.** Queues bound their depth; workers cap
   concurrency per tenant; HTTP applies token-bucket rate limits per
   `(tenant, route)`.
8. **Idempotency keys** on every write endpoint (and webhook delivery)
   so retries are safe.
9. **Context-first.** `context.Context` is the first arg of every public
   function; cancellation propagates from HTTP timeout → gRPC deadline →
   pgx query → external HTTP call.
10. **Functional options** for constructors with > 3 optional knobs
    (e.g. `NewRunner(store, opts...)`).
11. **Errors as values, wrapped.** `fmt.Errorf("...: %w", err)`; sentinel
    errors via `errors.Is`. No panics outside `init()`.
12. **No premature abstraction.** Three call sites before extracting an
    interface; one consumer for now is fine.

---

## 5. Edge cases the platform must survive

This is the deliberate "what could go wrong" list — every item maps to a
concrete mitigation already designed in.

### 5.1 Quality checks against huge client warehouses

The motivating case: a `not_null` check on a 5B-row Snowflake table.

| Risk                                  | Mitigation                                                               |
| ------------------------------------- | ------------------------------------------------------------------------ |
| Query takes 30 minutes, blocks API    | Checks run in **Asynq workers**, never inline. API returns a `CheckRun.Queued` immediately. |
| Query never finishes / hangs          | Per-check `Timeout` (default 5m, max 60m) wired into `context.WithTimeout`; warehouse driver cancels the running query on context cancel. |
| Customer DB charges per-byte scanned  | Default to **server-side sampling**: `TABLESAMPLE BERNOULLI(1)` on Postgres, `SAMPLE(1)` on Snowflake, `TABLESAMPLE SYSTEM` on BigQuery. Sample size is a per-check setting; full scan requires explicit opt-in. |
| Worker dies mid-run                   | Asynq's at-least-once redelivery + idempotency on `(check_id, scheduled_for)` — duplicate runs collapse on insert. |
| Result set too big to hold in memory  | We never materialize rows; checks return scalars. `custom_sql` enforces `LIMIT 1` on the projection. |
| Customer DB rate-limits us            | Per-(tenant, datasource) **token-bucket semaphore** in Redis — at most N concurrent queries per source. Excess waits in queue. |
| Stale check result confuses operators | `CheckRun.expires_at = scheduled_for + 2 × interval`; UI greys out expired results. |
| Cascading failures across 10k checks  | Workers respect a per-tenant **circuit breaker** — once 5 consecutive datasource errors fire, that source is paused for 60s. |

### 5.2 Pipeline failures in real time

| Risk                                     | Mitigation                                                                 |
| ---------------------------------------- | -------------------------------------------------------------------------- |
| Task fails transiently                   | `RetryPolicy` with exponential backoff + jitter; default 3 attempts.       |
| Task fails permanently                   | DLQ row in `task_runs` with `dead_letter=true`; surfaces in `/runs?dlq=1`. |
| Downstream stays "running" after upstream fail | `runner.go` skips downstream tasks (`TaskSkipped`) when a `DependsOn` fails and `FailFast` is true. |
| Run gets stuck (worker died)             | `RunReaper` cron sweeps `runs` where `status=running AND last_heartbeat < now()-5m` → marks `Failed` with reason `stuck`. |
| Notification storm on outage             | Notification dispatcher **deduplicates** by `idempotencyKey = sha256(rule_id ‖ event_id)`; same rule + event is delivered once even if event re-emits. |
| Webhook receiver is down                 | Per-channel retry queue, exponential backoff up to 24h; after that a `notification.failed` event lights up the alerts page. |
| Large fan-out (1 event → 500 deliveries) | Dispatcher enqueues each delivery into Asynq; never delivers in the request path. |

### 5.3 Multi-tenancy and noisy neighbors

| Risk                               | Mitigation                                                          |
| ---------------------------------- | ------------------------------------------------------------------- |
| One tenant exhausts all workers    | Per-tenant queue priority + max in-flight cap (Asynq queue per tier).|
| Cross-tenant data leak via bug     | Every storage query takes `tenantID`; integration test asserts a query without it returns `ErrMissingTenant`. Policy engine denies cross-tenant access even for admins. |
| Long-running query blocks shared DB pool | Reads use a separate read-only pool with statement timeout = 30s.   |

### 5.4 Auth, secrets, and crypto

| Risk                                  | Mitigation                                                         |
| ------------------------------------- | ------------------------------------------------------------------ |
| JWT signing key leaks                 | Keys live in External Secrets Operator → Vault/KMS; rotated on a 90-day schedule with overlapping `kid`s. |
| OIDC provider down                    | Sessions are independent of the IdP after login; refresh token errors degrade to read-only mode rather than logging users out abruptly. |
| Webhook secret stolen                 | Signed payload (`X-Plowered-Signature: t=…,v1=…`); receivers verify before processing; 5-minute timestamp window blocks replay. |
| PII in logs                           | `internal/obs/redact` strips fields tagged `pii:true` from structured logs; lint rule blocks `slog.Any` on raw structs. |

### 5.5 Storage / migration / capacity

| Risk                                 | Mitigation                                                          |
| ------------------------------------ | ------------------------------------------------------------------- |
| Postgres bloat from audit table      | Audit rows older than 90 days are moved nightly to S3 (object-locked). |
| Catalog grows past Postgres comfort  | Hot tables are **partitioned by tenant_id + month**; lineage edges are sharded by `tenant_id`. |
| Migration locks a 100M-row table     | All `ALTER` migrations wrapped in `pg_repack`-style online recipes; long migrations gated behind the `migrations.allow_long` env. |
| Disk full on a worker node           | Workers stream task logs directly to S3 with a 100 MB local ring buffer; node disk is ephemeral. |

### 5.6 Operational

| Risk                            | Mitigation                                                      |
| ------------------------------- | --------------------------------------------------------------- |
| Bad deploy bricks the API       | Helm `maxSurge=1, maxUnavailable=0` + readiness probe gating new traffic; image rollback in <60s. |
| Schema migration fails halfway  | Migrations are reversible; a failed `up` triggers an automatic `down`; deploy aborts before the new pods serve traffic. |
| Observability blind spot        | Every public RPC emits a span + a histogram; SLOs published in Grafana; alerting rules in Git, not the UI. |

---

## 6. Backend scalability playbook

Concrete knobs we tune as load grows.

1. **Stateless services, sticky data.** All app pods can be replaced any
   time; data sticks to its primary store. Horizontal Pod Autoscaler on
   `request_p95` and `queue_depth`.
2. **Read/write split.** Catalog reads target a Postgres replica;
   writes go to primary. Replica lag is published as a metric and gated
   on critical reads.
3. **Connection pooling.** PgBouncer in front of Postgres in transaction
   mode; pgx pool per pod sized to `min(num_cores * 2, 20)`.
4. **Caching layers.**
   - Redis caches: tenant config, policy rules, channel configs (TTL 60s,
     event-driven invalidation).
   - In-process LRU caches: parsed SQL ASTs, compiled policy decisions,
     LLM embedding results.
5. **Hot-path budgets.** Each request has an explicit budget:
   - Catalog read: < 100ms p95
   - Lineage neighborhood (1-hop): < 200ms p95
   - Pipeline list: < 150ms p95
   The CI suite runs a benchmark that fails the build on a 10% regression.
6. **Bulk operations** ship as gRPC streams or batched endpoints; never
   loop a unary call.
7. **Async by default for anything > 250ms.** The web client renders an
   optimistic state, then resolves on SSE.

---

## 7. Frontend UX & performance

Goal: feel instant on every page, never jank when listing 100k assets, and
read like a polished enterprise product end to end.

### 7.1 Performance targets

| Metric                       | Budget         |
| ---------------------------- | -------------- |
| LCP (catalog page)           | < 2.0s p75     |
| INP (any interaction)        | < 200ms p75    |
| CLS                          | < 0.05         |
| Catalog list (10k rows)      | 60fps scroll   |
| Lineage graph (1k nodes)     | < 1s render    |

### 7.2 Patterns

- **Server Components by default**, client components only where needed
  (`"use client"` is a smell to be reviewed).
- **Streaming SSR + Suspense** for above-the-fold content; below-the-fold
  hydrates lazily.
- **Route-level data hooks** in `web/src/hooks/`:
  - `useAssets`, `useAsset`, `useLineageNeighborhood`
  - `usePipelines`, `useRun`, `useRunStream` (SSE)
  - `useChecks`, `useCheckHistory`
  - `useNotifications`, `useDeliveries`
  - `usePolicies`, `useAuditFeed`
  - `usePrincipal`, `useTenant`, `useFeatureFlag`
  All wrap TanStack Query, share retry/staleTime config, expose typed
  errors. Mutations use optimistic updates with rollback.
- **Forms**: `useZodForm()` wraps `useForm` with Zod-resolver and our
  shared schemas; fields render via Fluent UI primitives.
- **Tables**: `<DataGrid>` wraps TanStack Table + Virtual; supports
  column resize, server pagination, faceted filters, saved views.
- **Realtime**: `useEventStream(topic)` opens a single SSE connection
  multiplexed across components via a Zustand store; reconnect with
  exponential backoff.
- **Error boundaries** at the route segment level; each renders a
  `<RouteError>` Fluent banner with a "Retry" button that invalidates
  affected queries.
- **A11y**: Fluent v9 is WCAG 2.2 AA out of the box; we add `aria-live`
  on toasts, full keyboard nav on the lineage graph, and `prefers-reduced-motion` opt-outs on every animation.
- **i18n**: `next-intl`, all strings centralised; layout supports RTL.
- **Loamy theme tokens** unchanged: terracotta `#B8521B` on cream `#FAF6F0`;
  status tokens (success/failed/running/queued/skipped/warning) referenced
  from a single map (`web/src/theme/status.ts`).

### 7.3 Pages added in v2

| Route                       | Purpose                                                             |
| --------------------------- | ------------------------------------------------------------------- |
| `/pipelines`                | List + filter pipelines, run status sparklines, last-run latency    |
| `/pipelines/[id]`           | DAG view (React Flow), task config, schedule, run history           |
| `/runs`                     | Recent runs across pipelines, DLQ filter, replay action             |
| `/runs/[id]`                | Per-run timeline, per-task logs (streamed from S3), retry button    |
| `/checks`                   | Quality checks list, last outcome, freshness, sampled %             |
| `/checks/[id]`              | History chart, threshold editor, manual run                         |
| `/alerts`                   | Notification deliveries, status, retry, channel health              |
| `/admin/policies`           | RBAC rules editor with conditions builder + JSON view               |
| `/admin/audit`              | Searchable, filterable audit feed, export to CSV                    |

Each page has loading skeletons, empty states, error states, and a
Storybook story.

---

## 8. RBAC, audit, and tenant isolation

Three layers, each independently sufficient against most attack classes.

1. **Tenant column on every row.** Every storage method takes `tenantID`
   from the request context; queries `WHERE tenant_id = $1`. Cross-tenant
   reads are impossible at the storage layer.
2. **Policy engine.** `internal/core/policy.Engine` decides allow/deny on
   `(principal, verb, resource)`. Two-tier model:
   - **Workspace roles** (`admin`, `steward`, `editor`, `viewer`) for
     coarse access.
   - **Per-resource ABAC rules** with conditions on principal group,
     resource tag (`class:pii`), or owner.
   - **Deny rules override allow** — a `class:pii` deny rule beats admin.
3. **Append-only audit.** `internal/core/audit.Writer` is invoked from
   every mutation. The Postgres adapter writes to a partitioned
   `audit_events` table with a `WORM` policy (no UPDATE/DELETE roles
   exist outside the daily archive job).

---

## 9. Compliance mapping (SOC 2 Type 1 + 2, GDPR, HIPAA)

`COMPLIANCE.md` has the per-control matrix. The summary:

### 9.1 SOC 2 (Type 1 design + Type 2 evidence)

| Trust criterion          | Implementation                                                                |
| ------------------------ | ----------------------------------------------------------------------------- |
| **CC1 control env.**     | Code owners enforced in `.github/CODEOWNERS`; security review tag on PRs      |
| **CC2 communication**    | Public roadmap + changelog; incident comms templates in `docs/incidents`      |
| **CC3 risk assessment**  | Quarterly threat model review checked into `SECURITY.md`                      |
| **CC4 monitoring**       | Centralised obs (OTel → Prometheus + Loki + Tempo), SLOs per service          |
| **CC5 control activities** | RBAC + audit + change-mgmt via PR + signed releases (Cosign)                |
| **CC6 logical access**   | OIDC + MFA enforced; session TTL; IP allowlists per tenant; scoped API keys    |
| **CC6.6 boundary**       | mTLS (Linkerd) inside the cluster; TLS 1.3 at edge; HSTS + CSP                |
| **CC7 system ops**       | Helm hook migrations; canary deploy; auto-rollback; on-call runbooks          |
| **CC8 change mgmt**      | Branch protection, mandatory review, CI must pass, signed commits             |
| **CC9 risk mitigation**  | Dependency scanning (Trivy), SBOM (Syft), container signing (Cosign)          |
| **A (availability)**     | HPA, multi-AZ, cross-region read replica, PITR backups, DR drill quarterly    |
| **C (confidentiality)**  | Field-level encryption for `secret_urn`s; AES-GCM with KMS keys; least-priv DB users |
| **PI (processing integrity)** | Pipeline runs idempotent; checks deterministic; data quality gates       |
| **P (privacy)**          | DSR endpoints (export/delete); consent registry per tenant                    |

### 9.2 GDPR

| Article                                  | Implementation                                                       |
| ---------------------------------------- | -------------------------------------------------------------------- |
| Art. 5 — lawfulness, minimisation        | Catalog only stores metadata; payload PII never enters Plowered DB    |
| Art. 15 — right of access                | `GET /v1/dsr/export?subject=…` job streams subject's records to S3    |
| Art. 16 — rectification                  | `PATCH /v1/users/{id}` + audit                                       |
| Art. 17 — erasure                        | `DELETE /v1/dsr/erase?subject=…` runs scrub job; tombstones retained for legal hold |
| Art. 20 — portability                    | DSR export uses JSON + CSV                                           |
| Art. 25 — privacy by design              | Default minimum role is `viewer`; PII tag forces extra approval       |
| Art. 28 — processor obligations          | DPA template in `legal/dpa.md`; sub-processor list published         |
| Art. 30 — records of processing          | `audit_events` is the ROPA source of truth                           |
| Art. 32 — security                       | TLS, encryption at rest, RBAC, MFA, vulnerability mgmt               |
| Art. 33 — breach notification            | 72h runbook + automated PagerDuty escalation on `security.incident`   |
| Art. 44 — int'l transfers                | Region pinning per tenant (`tenant.region` in JWT); SCCs in DPA       |

### 9.3 HIPAA (when handling PHI)

| Safeguard                              | Implementation                                                       |
| -------------------------------------- | -------------------------------------------------------------------- |
| §164.308 admin safeguards              | Workforce training tracked, access reviews quarterly                 |
| §164.310 physical safeguards           | Cloud provider attestation (AWS/GCP HIPAA-eligible services only)    |
| §164.312(a) access control             | Unique user IDs, automatic logoff (15m default), encryption          |
| §164.312(b) audit controls             | `audit_events`, immutable, tamper-evident hash chain                 |
| §164.312(c) integrity                  | SHA-256 chain over audit rows; anomaly detection job                 |
| §164.312(d) person/entity auth         | OIDC + MFA + per-API-key scopes                                      |
| §164.312(e) transmission security      | TLS 1.3 only, HSTS, mTLS in cluster                                  |
| §164.314 BAA                           | BAA template in `legal/baa.md`; required for HIPAA tier              |
| §164.316 documentation                 | Policies in `docs/security/policies`, retention 6 years              |

---

## 10. Tech-stack fallbacks (if a primary choice fails the bake-off)

| Primary           | Fallback              | Trigger                                                  |
| ----------------- | --------------------- | -------------------------------------------------------- |
| Asynq             | River (Go + Postgres) | Customers refuse to run Redis                            |
| NATS JetStream    | Apache Kafka / Redpanda | Throughput > 500k msg/s sustained                       |
| ClickHouse        | TimescaleDB           | Existing Postgres-only ops policy                        |
| pgvector          | Qdrant                | Vector count > 5M or recall on 1000-NN < 0.95            |
| Linkerd           | Istio                 | Customer requires advanced traffic policy                |
| Echo              | Connect-only HTTP     | If we drop REST in favor of pure gRPC-Web                |
| TanStack Query    | RTK Query             | Already in the customer's stack                          |

---

## 11. Edge-case test plan

We don't ship a feature without a test for each row in §5 that's relevant.
The matrix is enforced by `internal/qa/coverage.go` which prints the
mapping and fails CI if a `// edge:` comment names an unrun test.

---

## 12. What's next (sequenced)

1. Wire `Asynq` worker for `quality.Runner.Run` so checks run async.
2. Add `redis` and `nats` to `docker-compose.yml`; pull images locally.
3. Implement `internal/adapters/postgres/notify` + `…/policy` + `…/audit`
   so the embedded mode and prod mode share the same code path.
4. Add HTTP endpoints (`/v1/pipelines`, `/v1/runs`, `/v1/checks`,
   `/v1/notifications`, `/v1/policies`) wired through the policy engine.
5. Build the FE pages listed in §7.3 with the hooks listed there.
6. Add the SOC 2 / GDPR / HIPAA evidence collectors to
   `internal/compliance/evidence.go` so audits run themselves.
