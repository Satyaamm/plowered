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
- Authentication via email/password (Argon2id) today; OIDC + per-tenant
  JWT on the roadmap. Sessions are server-side rows behind HttpOnly,
  Secure, SameSite=Lax cookies; rotated on login; revocable from
  `/account` → Active sessions. 14-day TTL.
- **Account lockout**: 5 consecutive failed logins inside a 15-minute
  rolling window flips `users.status = 'locked'`. Cleared by a
  successful password reset (`internal/api/http/auth.go` +
  `password_reset.go`). Threshold + window follow OWASP / NIST 800-63B.
- **Per-IP rate limiting** on auth endpoints (login, signup, forgot,
  reset, verify): token bucket, configurable per env. Headers follow
  IETF draft-ietf-httpapi-ratelimit-headers (RFC 9239):
  `RateLimit-Limit`, `RateLimit-Remaining`, `RateLimit-Reset`.
- **Per-principal rate limiting** on authenticated routes: 120 reads/
  minute + 30 writes/minute by default. Keys by `principal_id` when
  authed, falls back to IP. Same RFC 9239 headers.
- **Security headers** (set by `SecurityHeadersMW`): HSTS
  (`max-age=31536000; includeSubDomains`), `X-Content-Type-Options:
  nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy:
  strict-origin-when-cross-origin`, `Permissions-Policy` lockdown,
  COOP + CORP, strict CSP (relaxed only on `/docs` for Swagger UI).
- Network: TLS 1.3 at the edge; mTLS service-to-service. (§9)
- Encryption at rest for connector credentials via AES-256-GCM sealed
  vault. Sealed keys never appear in logs, metrics, or audit `before/
  after` JSON.
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

Two surfaces serve these rights — **admin DSR** for "the regulator
asked us to delete this customer" and **self-service /v1/account** for
the user acting on themselves.

- **Right to access (Art. 15)**:
  - Self-service: `GET /v1/account/export` returns a JSON bundle with
    `_meta`, `profile`, `memberships`, and active `sessions` for the
    authenticated user. Sets `Content-Disposition: attachment` so the
    browser saves a file the user can `jq` over.
  - Admin: `GET /v1/dsr/...` returns every row across every table
    referencing the subject id (covers data subjects who are not
    Plowered users — e.g. people whose data is catalogued).
- **Right to rectification (Art. 16)**: `PATCH /v1/account/profile`
  for self-service updates. Standard UPDATE flow with audit trail for
  admin-driven changes.
- **Right to erasure (Art. 17)**:
  - Self-service: `DELETE /v1/account?confirm=true` pseudonymises the
    user (email → `deleted-<uuid>@deleted.invalid`, names/phone/avatar
    blanked, `status='deleted'`, `password_hash` cleared) and revokes
    every active session. The `users.id` is retained so audit history
    remains investigable under Art. 17(3)(b) (compliance with a legal
    obligation — SOC 2 + HIPAA audit retention).
  - Admin: `POST /v1/dsr/erase` writes a tombstone and runs a
    soft-delete pass; downstream connectors with `pii_columns`
    configured re-emit nullified rows on next sync.
- **Right to portability (Art. 20)**: both export surfaces emit
  structured, machine-readable JSON.
- **Right to object (Art. 21)**: telemetry opt-out via
  `PLOWERED_TELEMETRY_DISABLED=true` (deploy-time). Marketing email
  preferences live on the Account → Notifications tab.

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
| Account lockout (5 fails / 15 min) | CC6 | Art. 32 | §164.308(a)(5)(ii)(C) | `internal/api/http/auth.go` |
| Argon2id password hashing | CC6 | Art. 32 | §164.312(a)(2)(i) | `internal/core/identity/password.go` |
| Per-IP auth rate-limit + RFC 9239 headers | CC6 | Art. 32 | §164.308(a)(5)(ii)(C) | `ratelimit_auth.go` |
| Per-principal API rate-limit | CC6/CC7 | Art. 32 | §164.312(b) | `ratelimit_api.go` |
| Security headers (HSTS / CSP / X-Frame-Options) | CC6 | Art. 32 | §164.312(e)(1) | `security_headers.go` |
| Self-service data export (Art. 20) | — | Art. 15, 20 | — | `GET /v1/account/export` |
| Self-service erasure / pseudonymisation | CC9 | Art. 17 | — | `DELETE /v1/account` |
| Sessions list + revoke + sign-out-everywhere | CC6 | Art. 32 | §164.312(a)(2)(iii) | `account.go` |
| SSRF guard on outbound connectors | CC6/CC9 | Art. 32 | §164.312(e) | `internal/core/secrets/ssrf.go` |
| Session revocation on password change | CC6 | Art. 32 | §164.312(a) | `account.go` + `password_reset.go` |

## 8. What's still open

The product side is in good shape; what remains is mostly **operational
attestation work** that can only happen once the controls have been
running in production for the required observation window.

### Code / product gaps

- **MFA (TOTP)**. `users.mfa_enrolled` column exists; the enrollment +
  challenge flow does not. Required by HIPAA §164.312(d) for any
  workforce member accessing PHI, and is a frequent SOC 2 finding when
  missing. *Next slice.*
- **SSO via OIDC / SAML**. Customers in regulated industries want IdP-
  driven login (Okta, Entra, Auth0). Plan: `coreos/go-oidc` for
  Authorization Code + PKCE, lazy JIT provisioning into
  `tenant_memberships`. *Next slice after MFA.*
- **WebAuthn / passkey** as second factor or primary. Optional but
  increasingly expected.
- **ClassifierAgent live** (designed; PII/PHI tag proposals).
- **Customer-managed encryption keys (BYOK / HYOK)** for the secrets
  vault. AWS KMS / GCP KMS / Azure KeyVault adapters.
- **Tenant-scoped data residency**. Today every tenant runs in the
  region of the deployment; a multi-region SaaS deployment needs the
  region pin enforced at write-time and routing handled at the LB.
- **Privacy Policy / Terms of Service** pages in the marketing site
  (operational + legal, not engineering).

### Process / operational gaps

- **Independent SOC 2 Type II audit**. Requires 6+ months of evidence
  collection with the controls running. Prerequisite: pick an auditor
  (Vanta / Drata / Secureframe agent for the evidence collection, then
  a CPA firm for the attestation).
- **BAA legal template**. Required before signing any HIPAA-covered
  entity. Engage outside counsel; AWS / GCP / Azure each publish
  starter templates.
- **Sub-processor list + DPAs**. We use Resend (email), an LLM
  provider per workspace (BYOM), an S3-compatible object store, and
  Postgres. A maintained sub-processor list + signed DPAs are an Art.
  28 requirement.
- **Backup restore drill**. Document a runbook AND execute it
  quarterly. The audit asks for evidence of the last restore, not just
  the runbook.
- **Vulnerability scanning in CI**. `govulncheck` + `npm audit` +
  `trivy` on the Docker images, gating merges. Currently manual.
- **Pen test**. Most enterprise procurement teams ask for one within
  the last 12 months. Budget ~$15-30k for a focused web-app + API
  engagement.
- **Trust Center page**. A public "what controls do you have"
  landing page (e.g. trust.plowered.com) cuts security-review cycles
  in half. Vanta / Drata both ship one if you adopt their platform.
- **Incident response tabletop**. Run one once a quarter; record the
  minutes. The runbook at `docs/runbooks/data-breach.md` is the
  starting script.
- **Workforce security training**. Annual + at hire. Required by
  HIPAA §164.308(a)(5). Tools: KnowBe4, Hoxhunt, Vanta-bundled.
