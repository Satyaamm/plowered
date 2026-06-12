# Plowered Orchestration

The orchestration layer turns Plowered from a passive metadata catalog into an
active platform that runs ETL/ELT pipelines, asserts data quality, reacts to
failures in real time, and writes lineage back into the same graph that
powers search, agents, and MCP.

## 1. Why this exists

Catalogs that don't run pipelines drift out of sync with the warehouses they
describe. Orchestrators that don't own the catalog produce orphan runs no
one can find. Plowered owns both — every pipeline run emits lineage edges
into the same graph, every quality check writes back to the asset it tested,
and every failure flows through one notification system.

## 2. Domain model

```
Workspace ─┬─ Pipeline ─── Task ─┐
           │       │             │
           │       └── Schedule  │
           │                     │
           ├─ Run ─── TaskRun ───┘
           │       │
           │       └─ emits Event(s)
           │
           ├─ Check ─── CheckRun
           │
           └─ Notification ─── NotificationDelivery
```

**Pipeline** — a named, versioned DAG of `Task`s with a `Schedule` and an
owner. Pipelines are tenant-scoped.

**Task** — one node in the DAG. Has a `Type` (`sql`, `connector_sync`,
`quality_check`, `transform_run`, `webhook`), a `Config` map, and a
`DependsOn` list. Tasks declare their *output assets* by qualified name so
the runner can attach lineage.

**Schedule** — cron-style trigger. The scheduler enqueues a `Run` whenever
the cron matches, idempotent per `(pipeline_id, scheduled_at)`.

**Run** — one execution instance. State machine: `queued → running →
(succeeded | failed | cancelled)`.

**TaskRun** — one execution of one task within a run. State machine:
`queued → running → (succeeded | failed | skipped | retrying)` with
attempt counter and last-error string.

**Check** — a data-quality assertion bound to one or more assets. Types:
`row_count`, `not_null`, `freshness`, `uniqueness`, `custom_sql`.

**CheckRun** — one execution of a check. Outcome: `pass | fail | error`
with a measured value, a threshold, and human-readable diagnostics.

**Event** — typed message published on the in-process bus. Examples:
`run.started`, `task.failed`, `check.failed`, `pipeline.scheduled`.

**Notification** — a configured channel (`email`, `webhook`, `slack-style`,
`pagerduty-style`) plus a filter rule selecting which events trigger it.

**NotificationDelivery** — one attempt to deliver a notification, with
status, attempt count, and external receipt id (for de-dup at the receiver).

## 3. Pipeline runner

The runner is event-driven. One worker per Run; tasks within a run execute
according to topological order over `DependsOn`. Independent tasks at the
same depth run concurrently up to `Pipeline.Concurrency`.

### Failure handling
- **Per-task retry policy**: `max_attempts`, `initial_backoff`, `multiplier`,
  `max_backoff`. The TaskRun is updated in place with attempt counters.
- **Pipeline-level fail-fast vs continue**: configurable. Default fail-fast
  for production pipelines, continue for backfills.
- **Dead-letter store**: `task_runs.dead_letter = true` after final attempt
  fails. The notification system fires on transition into this state.
- **Stuck-run reaper**: a background sweeper marks runs with no progress
  for > `Pipeline.HeartbeatTimeout` as `failed`.

### ETL vs ELT
Both modes share the same runner; the difference is task ordering:

- **ELT** (default for warehouse-native): `connector_sync` extracts
  metadata, then `transform_run` (or `sql`) runs in the warehouse, then a
  bundle of `quality_check` tasks asserts the result.
- **ETL**: pre-load transformation tasks run before `connector_sync`.

Both are valid DAGs; users are not asked to pick a mode upfront.

## 4. Real-time failure handling

```
TaskRun fails →
  Event "task.failed" published →
    NotificationRouter matches subscribers →
      Channel-specific delivery worker enqueues retry on failure →
        DeliveryReceipt persisted with idempotency key
```

Three guarantees:

1. **At-least-once delivery** of notifications, with dedup keys so the
   external system can drop duplicates.
2. **Bounded latency** — events are dispatched on a buffered channel; if a
   subscriber is slow, the bus drops oldest *for that subscriber* (slow
   consumers don't back-pressure faster ones).
3. **Fan-out** — one event can trigger N notifications across M channels.

Channels are pluggable behind an interface. Built-ins today:
`channel:webhook` (SSRF-guarded), `channel:log`, `channel:slack`,
`channel:email` (Resend). All four route through a bounded async worker
pool (`PLOWERED_NOTIFY_WORKERS` / `_QUEUE_SIZE`) with an inline fallback
on full-queue. `last_delivered_at` is materialised per rule on every
successful send.

## 5. Lineage integration

Every `sql` and `transform_run` task records lineage automatically:

1. The runner parses the executed SQL with `internal/core/lineage`.
2. For each parsed statement, it looks up source/target asset IDs via the
   resolver.
3. It writes a `LINEAGE` edge with
   `properties = { transformation_id: task_run_id, op, executed_at, run_id }`.

The lineage page on an asset (`/asset/[qn]`) automatically shows pipeline
runs that produced it because every edge knows which `task_run_id` made it.

## 6. Quality framework

Five built-in check types, each implementing the `quality.Check` interface:

| Type | Question it answers | Failure example |
|---|---|---|
| `row_count` | Did the table get any rows? | "0 rows in mart.daily_orders, expected ≥ 100" |
| `not_null` | Are required columns populated? | "27 nulls in customer_id" |
| `freshness` | Was the table updated recently? | "Last update 36h ago, threshold 24h" |
| `uniqueness` | Are key columns unique? | "184 duplicate (order_id) rows" |
| `custom_sql` | Anything you can write in SQL | User-defined |

Custom checks are SQL of the form:

```sql
SELECT 1 AS pass WHERE (your assertion)
-- or
SELECT failing_row_id, reason FROM your_query
```

Each `CheckRun` records:
- `value` — what the check measured (row count, null count, etc.)
- `threshold` — the asserted bound
- `outcome` — `pass | fail | error`
- `affected_asset_qn` — the asset the check ran against
- `severity` — `info | warning | error | critical`

`CheckRun` rows are queryable on the asset detail page so users see "did
this asset pass the latest checks?" alongside its lineage.

## 7. Notifications

```
NotificationRule
  filter: events.type IN ('task.failed', 'check.failed') AND severity >= 'error'
  channel_id: ch_xyz
  template_id: tpl_abc
```

Rules are evaluated for every event. A matching rule schedules a delivery on
its channel's queue. Templates produce the channel-specific payload from a
typed `EventContext`.

## 8. RBAC

Two-tier model — workspace **roles** + per-resource **ABAC rules**.

1. **Workspace roles**: `viewer`, `editor`, `steward`, `admin`,
   `super_admin`. Assigned via tenant membership. The role → verb table
   lives in `internal/core/policy/policy.go` and is mirrored on the
   frontend in `web/src/lib/hooks/use-role.ts`.
2. **Per-resource rules**: rows in `policy_rules` consulted by the
   engine after role grants. Conditions supported today:
   - `principal.role`
   - `principal.group`
   - `resource.tag`
   - `resource.owner=self`

   Effect can be `allow` or `deny`. **Deny wins.**

Verbs: `read | edit | propose | certify | delete | run | admin | purge`
(`purge` is super_admin-only and permanently removes a recycle-bin
tombstone).

Enforcement happens at the **HTTP handler** layer. Every gated handler
calls `gate(w, r, authz, verb, resourceType)` from
`internal/api/http/authz.go` immediately after `mustTenant`; on deny the
helper writes `403` with the engine's reason string. The policy engine
is built once in `NewMux` from the registered `policy.RuleRepo` and
threaded into every handler family. Coverage is verified by
`internal/api/http/rbac_test.go`, which asserts the expected deny / allow
for every role across one representative mutating endpoint per family.

The full endpoint coverage table lives in `SECURITY.md` § Authorization.

## 9. Audit log

Append-only `audit_events` table (already migrated in M1). Every
mutation RPC, every pipeline run, every check run, every policy change, and
every notification delivery emits an event with:

```
event_id, tenant_id, actor_id, actor_kind, action, resource_type,
resource_id, before_json, after_json, ip, user_agent, request_id, created_at
```

Daily export to S3-compatible object storage with object-lock for tamper
evidence.

## 10. Real-time UX

The web UI receives status updates via short-poll (3s for live runs, 30s
for everything else) in v0. WebSockets are a follow-up — they buy <1s
latency at the cost of a stateful proxy hop.

Status colors come from a single token set in `web/src/theme/status.ts`:

| Status | Token | Hex (light) | Use |
|---|---|---|---|
| Success | `statusSuccess` | `#3F7A4E` | passed checks, succeeded runs |
| Failed | `statusFailed` | `#B83330` | failed runs, failed checks |
| Running | `statusRunning` | `#D4A341` | in-progress runs |
| Queued | `statusQueued` | `#7E8896` | waiting tasks |
| Skipped | `statusSkipped` | `#C9B79A` | dependency-skipped tasks |
| Warning | `statusWarning` | `#D17A1F` | degraded but non-critical |

These complement the Loamy brand palette and ship as Fluent UI tokens.

## 11. Performance targets

| Operation | Target |
|---|---|
| Schedule trigger → Run queued | < 1s |
| Task dispatch → TaskRun running | < 200ms |
| Quality check (1M-row asset, single column) | < 5s |
| Notification dispatch (event → channel) | < 500ms p95 |
| Lineage write (per task) | < 50ms p99 |
| /v1/runs?status=running list | < 100ms p99 |

## 12. Data contracts (M8)

Beyond per-asset quality checks, contracts express the producer's
*promise* about an asset's shape and freshness — and the consumer's
right to be alerted the moment it's violated.

A `contracts` row carries:

- `expected_columns` — `[{name, type}]`. A missing column or type drift
  is a breach.
- `freshness_seconds` — the asset's most-recent timestamp must be no
  older than this when `contract.Runner` checks.
- `null_thresholds` — `{column: max_fraction}`. The latest profile
  snapshot drives the comparison.

`contract.Runner` ticks every `PLOWERED_CONTRACT_TICK` (default 5m),
walks every asset with a contract, and emits `ContractBreach` events
keyed by `hash = sha256(kind, observed)`. The dedupe table
(`contract_breaches`) ensures "still broken" doesn't refire — the breach
only re-emits when the observed fingerprint changes.

The inline contract editor lives on the asset Overview tab; the
admin-wide queue is at `/contracts`. Every breach flows through the
notify dispatcher.

## 13. Cost tracking (M8)

`cost.Recorder` writes a `cost_records` row for every billable call (LLM
tokens, warehouse seconds, S3 bytes). `cost.Watcher` rolls them up
against per-tenant `cost_budgets` and fires `CheckFailed` events at warn
+ hard thresholds with 24h dedupe.

The `/cost` page calls `GET /v1/cost/summary?by_feature=1` and renders
ai / warehouse / storage breakdowns alongside any budget alerts.

## 14. Certifications (M8)

A separate workflow from data contracts: certifications express
*human-verified trust* in an asset. Stewards `propose` certification,
admins `approve` or `reject`, and any role with the `certify` verb can
`revoke`.

A `requireActiveProposal` guard prevents double-resolution (the
proposal must still be the asset's `latest` row when the admin acts).
The pending queue lives at `/certifications`.

## 15. Roadmap inside this layer

Shipped:
| Slice | Scope |
|---|---|
| O1 | Pipeline + Task + Run domain types, in-memory stores, runner |
| O2 | Quality checks (5 built-ins), CheckRun persistence |
| O3 | Event bus + Notification dispatch (webhook / log / slack / email) |
| O4 | RBAC enforcement layer + audit log writer |
| O5 | HTTP endpoints + UI pages (`/pipelines`, `/runs`, `/checks`, …) |
| O6 | Cron scheduler + retries + dead-letter handling |
| O7 | Postgres persistence + migrations |
| O8 | Async migrations (Asynq + incremental watermarks in S3) |
| O9 | Contracts + `contract.Runner` |
| O10 | Cost tracking + `cost.Watcher` + budgets |
| O11 | Certifications workflow |

Next:
| Slice | Scope |
|---|---|
| O12 | Column-level lineage extractor mining `ai_query_executions` + pipeline SQL |
| O13 | WebSocket real-time updates (replace 3s/30s poll) |
| O14 | Workflow approvals generalised beyond certifications |
| O15 | Anomaly detection over `profile_snapshots` |
