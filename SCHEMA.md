# Plowered — Schema reference

Every table in the platform, what it stores, and what it's there for.
Authoritative SQL lives in `internal/storage/postgres/migrations/` and the
migrations are applied in numeric order. Migrations 0001–0003 ship the
core domain; 0004 adds the production / enterprise tier.

| Migration | Theme |
|---|---|
| 0001 | catalog (assets / edges / tags) + audit log v1 |
| 0002 | orchestration (pipelines, runs, task_runs, checks, notify, policy) |
| 0003 | comprehensive audit (hash chain, http context) + recycle bin |
| 0004 | enterprise: tenants, users, sessions, API keys, connections, GDPR, leases, outbox, encryption keys, feature flags, glossary, classifications |

## Multi-tenancy is non-negotiable

Every domain table carries `tenant_id`. Every storage method extracts the
tenant from `ctx` via `storage.TenantFromContext`. Every compound index
leads with `tenant_id` so the query planner prunes early.
**A row without a `tenant_id` is a bug.**

## Append-only tables

Two tables are append-only by policy:

- `audit_events` — application role gets `INSERT` + `SELECT` only. UPDATE
  / DELETE require ops intervention. Hash chain (`prev_hash`,
  `row_hash`) breaks if anyone tampers anyway.
- `outbox` — events flow out, never get rewritten. `processed_at` flips
  once and stays.

Tombstones in `deleted_records` get `restored_at` and `purged_at` updates
because that's how restore + super-admin purge work; otherwise they're
historical.

---

## Catalog (M1)

| Table | Purpose |
|---|---|
| **assets** | Every catalogued resource: tables, columns, dashboards, models. Trust state, tags, owners, properties. |
| **edges** | Asset relationships: lineage, contains, references. |
| **tags** | Reusable color-coded tags for assets. |

## Audit (M1 → M3)

| Table | Purpose |
|---|---|
| **audit_events** | Append-only log of every authenticated request. Captures who/what/when/where, before/after JSON, outcome, policy reason, full HTTP context, service version, and a `prev_hash`/`row_hash` chain for tamper-evidence. |

## Orchestration (M2)

| Table | Purpose |
|---|---|
| **pipelines** | Pipeline definition (name, tasks JSONB, schedule, concurrency, fail_fast). |
| **pipeline_runs** | One execution instance with status, started/finished, scheduled_at, triggered_by, idempotency_key, last_heartbeat. |
| **task_runs** | Per-task attempt with status, attempt_count, error, output, dead_letter flag. |

## Quality (M2)

| Table | Purpose |
|---|---|
| **quality_checks** | Configurable assertions on assets: row_count, not_null, freshness, uniqueness, custom_sql. |
| **quality_check_runs** | Each evaluation: outcome (pass/fail/error), value, threshold, diagnostic, severity, started/finished, duration_ms. |

## Notification (M2)

| Table | Purpose |
|---|---|
| **notify_channels** | Per-tenant delivery destinations (log, webhook, …). |
| **notify_rules** | Match events → routes to a channel (with min_severity + event_types filter). |
| **notify_deliveries** | Each notification attempt with attempts, last_error, delivered_at; `(tenant_id, idempotency_key)` unique. |

## Policy (M2)

| Table | Purpose |
|---|---|
| **policy_rules** | Per-resource ABAC rules layered over workspace roles. Effect=deny overrides allow. |

## Recycle bin (M3)

| Table | Purpose |
|---|---|
| **deleted_records** | Tombstones for everything deleted. Stores full JSON payload + actor + reason + parent_tombstone_id (for cascade-delete chains). `restored_at` populated by restore endpoint; `purged_at` set only by `super_admin`-gated permanent delete. **No automatic expiry.** |

## Tenants & access (M4)

| Table | Purpose |
|---|---|
| **tenants** | Org boundary. slug, region, tier (`free|standard|enterprise|hipaa`), encryption_key_id, settings JSONB. |
| **users** | Person-level identity. email + lower-case index, password_hash (empty = OIDC-only), MFA enrollment, last_login, locked_at. |
| **tenant_memberships** | M:N user × tenant with roles + groups; `invited_at` / `accepted_at` track invite flow. |
| **sessions** | Active login sessions. `revoked_at` lets admins force logout; `last_seen_at` powers idle-timeout. |
| **api_keys** | Service-to-service tokens. Only the `key_hash` (bcrypt/argon2) is stored; `prefix` = first 8 chars for forensic lookups. `last_used_at` + `last_used_ip` show who's actually using it. `expires_at` + `revoked_at` for hygiene. |

## Connections (M4)

| Table | Purpose |
|---|---|
| **connections** | Customer datasources we connect to (Postgres, Snowflake, BigQuery, …). config JSONB + `secret_urn` pointing at the vault path. `health` column lets the UI show live status. |

## Workspaces (M4 — optional scope between tenant + resource)

| Table | Purpose |
|---|---|
| **workspaces** | Logical sub-grouping inside a tenant (env, team). |
| **workspace_memberships** | M:N membership with workspace-scoped roles. |

## Governance (M4)

| Table | Purpose |
|---|---|
| **glossary_terms** | Business definitions. parent_id → hierarchy. Status workflow `draft → reviewed → approved → deprecated`. |
| **term_assignments** | Term ↔ asset link. |
| **data_classifications** | Per-asset labels: `public|internal|confidential|pii|phi|pci`. Policy engine reads these for conditional rules. |

## GDPR (M4)

| Table | Purpose |
|---|---|
| **consent_records** | Every consent grant / revocation. legal_basis, purpose, granted_at / revoked_at, source ("app" / "api" / "import"), proof JSONB (IP, UA, click-through). |
| **dsr_requests** | Data Subject Requests under GDPR Art. 15-20. type (`access | portability | rectification | erasure | restriction`), status, due_at (received + 30 days), artifact_urn for the export bundle on S3. |

## Legal (M4)

| Table | Purpose |
|---|---|
| **legal_holds** | Litigation holds. While active, deletion is blocked for the rows in `scope`. issued_by + issued_at + released_at for the chain of custody. |

## Reliability (M4)

| Table | Purpose |
|---|---|
| **idempotency_keys** | Cache of completed write requests for safe retries. PK is (tenant_id, key); `request_hash` rejects same-key-different-body. `expires_at` lets old entries fall off. |
| **outbox** | Outbox-pattern table — every state change writes a row in the same TX. Background relay reads `processed_at IS NULL` and forwards to NATS. Guarantees at-least-once delivery without distributed transactions. |
| **leases** | Distributed lock for the cron leader. Only the holder of `name="scheduler"` runs schedule firing; other replicas idle. `expires_at` is bumped each tick. |

## Crypto (M4)

| Table | Purpose |
|---|---|
| **encryption_keys** | Audit log of envelope-encryption rotation. We store **only** KMS pointers (`kms_arn`) and a public-component fingerprint — never key material. `tenants.encryption_key_id` FK pins each tenant to its current key. |

## Operations (M4)

| Table | Purpose |
|---|---|
| **feature_flags** | Per-flag, per-tenant or per-user override. `rollout_pct` for gradual rollouts. CHECK constraint enforces (tenant_id, user_id) are mutually exclusive. |

## Search & lineage extras (M4)

| Table | Purpose |
|---|---|
| **asset_embeddings** | Vector embeddings for semantic search. `model` column lets multiple embedding models coexist. JSONB today; switch to `vector(N)` when pgvector lands. |
| **column_lineage** | Column-level lineage edges (vs. asset-level in `edges`). transform = `identity | expression | aggregation`; `task_run_id` links the edge to the run that created it. |

---

## Compliance map

The schema's purpose-built around the audit + retention story compliance teams ask about.

| Concern | Schema source of truth |
|---|---|
| **HIPAA §164.312(b)** — audit controls | `audit_events` capturing every authenticated request |
| **HIPAA §164.312(c)** — integrity | `prev_hash` / `row_hash` chain on `audit_events` + role grants forbidding UPDATE/DELETE |
| **HIPAA §164.316(b)(2)(i)** — 6-year retention | retention is operator-configured (no auto-purge); ops archives daily to object-locked S3 |
| **GDPR Art. 5(1)(e)** — storage limitation | `legal_holds` + `dsr_requests` track deletion gates; tombstones expire only on operator command |
| **GDPR Art. 7** — consent records | `consent_records` |
| **GDPR Art. 15-20** — data subject rights | `dsr_requests` |
| **GDPR Art. 30** — records of processing | `audit_events` is the ROPA |
| **GDPR Art. 32** — security of processing | `sessions`, `api_keys`, `encryption_keys`, password hash + MFA on `users` |
| **GDPR Art. 33** — breach notification | `audit_events` `outcome='denied'` + alerts page powers detection |
| **SOC 2 CC4** — monitoring | every authenticated call is captured; `outbox` ensures alert events survive restarts |
| **SOC 2 CC5** — control activities | `policy_rules` + `audit_events.policy_reason` |
| **SOC 2 CC6** — logical access | `users`, `sessions`, `api_keys`, MFA |
| **SOC 2 CC7** — system operations | `pipelines`, `pipeline_runs`, `task_runs`, the scheduler reaper |
| **SOC 2 CC8** — change management | `audit_events` + `deleted_records` capture both forward and undo paths |

## Schema upgrades — what we'd add next

- `pgvector` extension swap on `asset_embeddings.embedding` (currently JSONB)
- TimescaleDB or partitioning on `audit_events` (volume tier)
- `dlq_events` for failed outbox messages so they don't burn redrive cycles
- `oauth_clients` if we ship a public OAuth API later
- `service_accounts` — distinct from `users` / `api_keys`, used for first-party integrations
