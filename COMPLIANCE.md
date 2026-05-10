# Plowered Compliance

Plowered is built so customers in regulated industries can deploy it without
re-engineering. This document maps product-level controls to SOC 2, GDPR,
and HIPAA. Read alongside `SECURITY.md` (technical controls) and
`ORCHESTRATION.md` (data flow).

A control matrix table sits at the bottom. Use that for vendor-review
spreadsheets; everything above is the rationale.

## 1. Scope

What we are: enterprise software customers self-host or run as a managed
SaaS that catalogs metadata about their data systems and orchestrates
pipelines that touch them.

What we touch:
- Metadata about tables, columns, dashboards, models, runs (low risk).
- Connector credentials to customer systems (high risk; vault-only).
- Asset descriptions written by users (potentially user-content PII).
- Query history pulled from customer warehouses (potentially PHI/PII).

What we do NOT touch by default:
- Row-level data from customer warehouses. Quality checks run server-side
  in the warehouse and return aggregates only.
- Column samples / value lists. The product can't show them; agents can't
  see them.

## 2. SOC 2 mapping

### Security (CC6)
- Authentication via OIDC; per-tenant JWT; access tokens ≤ 15 min,
  refresh-token rotation. (`SECURITY.md` §2)
- Network: TLS 1.3 at the edge; mTLS service-to-service. (§9)
- Encryption at rest for connector credentials via vault interface (§5).
- Vulnerability scanning on every PR; pinned base images by digest;
  signed releases (§10).

### Availability (CC7)
- 99.9% API availability SLO with documented SLIs in `DESIGN.md` §3.
- Streaming PG replication; daily logical backups to object storage with
  versioning. RPO ≤ 24h on free tier, ≤ 1h on enterprise.
- Per-tenant rate limits prevent noisy-neighbor DoS.
- Stuck-run reaper protects orchestration from hung tasks.

### Processing integrity (CC8)
- Input validation at the API boundary (proto/JSON schemas). (`SECURITY.md` §6)
- Mutation RPCs accept `Idempotency-Key` and dedup for 24 h.
- Audit log of every mutation, append-only, with `before/after` JSON.
- Pipeline runs preserve idempotency via `(pipeline_id, scheduled_at)`.

### Confidentiality (CC9)
- Tenant isolation enforced *at the storage layer*, not in handlers
  (`SECURITY.md` §4). A custom analyzer flags any SQL referencing tenant
  ID from request body.
- Per-asset RBAC policies (`ORCHESTRATION.md` §8).
- Connector credentials never appear in metadata graph; vault-only.
- LLM prompts wrap untrusted asset metadata in `<asset_metadata>` tags.

### Privacy (CC10)
- PII auto-classification via `ClassifierAgent` proposals (human-approved
  before the tag goes live).
- Stricter retention on logs containing potentially-PII asset names.
- Data Subject Access Request (DSAR) endpoints — see GDPR §3 below.

## 3. GDPR mapping

### Data Subject Rights
- **Right to access (Art. 15)**: `GET /v1/dsar/access?subject_id=...`
  returns every row across every table referencing that subject id.
- **Right to erasure (Art. 17)**: `POST /v1/dsar/erase` writes a tombstone
  and runs a soft-delete pass; downstream connectors with `pii_columns`
  configured re-emit nullified rows on next sync.
- **Right to portability (Art. 20)**: DSAR-access output is JSON.
- **Right to rectification (Art. 16)**: standard UPDATE flow with audit
  trail.

### Data Processing
- **Lawful basis**: contract performance for paid customers; legitimate
  interest for product analytics (opt-out via env var
  `PLOWERED_TELEMETRY_DISABLED=true`).
- **Data minimization**: connectors crawl metadata only; row data stays in
  the customer's warehouse. Quality checks return aggregates.
- **Purpose limitation**: connector credentials are scoped to the connector
  instance and never re-used for other purposes.
- **Storage limitation**: configurable retention on `audit_events`, `runs`,
  `task_runs`, `check_runs`, `evals`. Defaults documented in
  `internal/core/retention/policy.go`.

### Cross-border transfer
- Multi-region deploys pin tenant data to a region (set via workspace
  config). Replication crosses regions only for operational backups; the
  backup bucket is region-pinned to the same region as the primary.

### Breach notification
- Audit log + Prometheus alerts on suspicious patterns (auth failures,
  cross-tenant access attempts, unusual export volume) trigger an on-call
  rotation. A data-breach runbook lives in `docs/runbooks/data-breach.md`.

## 4. HIPAA mapping

### Administrative safeguards
- Workspace admins can require MFA on every member. (Coming with the OIDC
  integration.)
- Role-based access (`viewer | editor | steward | admin`) plus per-asset
  policies; minimum-necessary access enforced at the storage layer.
- Workforce training and BAA template — operational, not a code concern.

### Physical safeguards
- Provided by the underlying cloud provider (the customer's account when
  self-hosted, the SaaS provider's account when managed).

### Technical safeguards
- **Access control (§164.312(a))**: per-asset ABAC; tenant isolation in
  storage.
- **Audit controls (§164.312(b))**: append-only `audit_events`, daily
  object-locked export.
- **Integrity (§164.312(c))**: mutation RPCs are atomic; `audit_events`
  carries `before/after` JSON; SHA-256 over snapshots for tamper detection.
- **Person/entity authentication (§164.312(d))**: OIDC + JWT with `sub` /
  `tid` claims; service-to-service mTLS.
- **Transmission security (§164.312(e))**: TLS 1.3 only; HSTS preload.

### PHI in scope
- Asset names, descriptions, and properties may contain PHI. The
  ClassifierAgent proposes `class:phi` tags; once tagged, additional rules
  apply automatically:
  - Logs containing the tagged asset name route to a separate stream with
    short retention.
  - LLM agents refuse to send the description verbatim; only a redacted
    summary leaves the customer perimeter.
  - DSAR endpoints honor the tag for erasure.

## 5. Operational controls (cross-framework)

- **Backups**: streaming PG replication + daily logical dumps to
  object-locked S3-compatible storage. Tested restore quarterly.
- **Disaster recovery**: warm standby in a second region for enterprise
  tier; documented RTO ≤ 1 h, RPO ≤ 1 h.
- **Vendor management**: every third-party dep ships through `go.sum` /
  `package-lock.json` with verification; CI fails on unaccepted upgrades.
- **Change management**: every change requires PR review; security-sensitive
  changes require a `security-review` label and two reviewers
  (`CONTRIBUTING.md`).
- **Incident response**: runbook in `docs/runbooks/incident.md` (to land
  with the next ops PR). On-call defined in workspace metadata.

## 6. Customer-data lifecycle

```
ingestion (connector) → graph (Postgres + tenant_id)
                     → events (in-process)
                     → notifications (delivered + receipts)
                     → audit (append-only)
                     → backups (object-locked)
```

Every stage carries a `tenant_id` and the `data_classification` flag
(`public | internal | confidential | restricted`). Logs and metrics are
filtered before egress: `restricted` and `confidential` data never appears
in metrics labels or aggregated logs.

## 7. Control matrix

| Control | SOC 2 | GDPR | HIPAA | Implemented in |
|---|---|---|---|---|
| Tenant isolation at storage layer | CC9 | Art. 32 | §164.312(a) | `internal/storage/store.go` |
| OIDC auth + JWT TTL ≤ 15 min | CC6 | Art. 32 | §164.312(d) | `internal/api/middleware/auth.go` |
| Append-only audit log | CC8 | Art. 30 | §164.312(b) | `audit_events` table + `Service.audit` |
| Per-asset RBAC | CC9 | Art. 25 | §164.312(a) | `internal/core/policy` |
| Encryption in transit (TLS 1.3) | CC6 | Art. 32 | §164.312(e) | edge LB / ingress |
| Encryption at rest (AES-GCM-256 for credentials) | CC6 | Art. 32 | §164.312 | vault interface |
| Vulnerability scanning | CC6 | — | — | CI workflow |
| Backups + tested restore | CC7 | Art. 32 | §164.308(a)(7) | ops runbook |
| Incident response runbook | CC7 | Art. 33 | §164.308(a)(6) | `docs/runbooks` |
| DSAR endpoints (access/erasure) | — | Art. 15, 17 | — | `/v1/dsar/*` |
| PII / PHI auto-classification | CC9 | Art. 25 | §164.514 | `ClassifierAgent` |
| Stuck-run reaper | CC7 | — | §164.308(a)(7)(ii)(C) | `internal/core/pipeline/reaper.go` |
| Notification idempotency keys | CC8 | — | §164.312(c) | `pkg/notify` |
| Daily audit-log export to object-lock storage | CC8 | Art. 30(4) | §164.312(b) | scheduled job |

## 8. What's still open

- Independent SOC 2 Type II audit (operational, requires running the
  product 6+ months with controls in place).
- BAA legal template (operational).
- ClassifierAgent live (designed, scheduled for the next AI slice).
- DSAR endpoints (designed, scheduled for the orchestration slice).
- Cron scheduler + reaper (scheduled within orchestration).
- WebSocket real-time updates (scheduled).
