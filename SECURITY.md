# Security

## Threat model

| Adversary | Vector | Control |
|---|---|---|
| External unauthenticated | Public API | TLS 1.3 + OIDC; deny by default |
| Tenant A vs tenant B | Cross-tenant read | Row-level isolation enforced at storage layer |
| Insider with read access | Search/lineage exfil | Per-asset RBAC + audit log |
| Compromised connector creds | Customer-side exfil | Vault-backed secrets, least-privilege roles, rotation |
| Prompt injection | Hijack Context Agents | Untrusted data wrapped as data; structured outputs |
| Supply chain | Backdoor in deps | `go mod verify`, pinned digests, vuln scan, distroless |
| Stolen JWT | Impersonation | ≤15 min access tokens, refresh rotation, revocation list |

## Authentication

- **Browser:** server-side sessions; `plowered_session` HttpOnly + Secure + SameSite=Lax cookie with a 14-day TTL, revocable from `/account`. Argon2id password hashing (`m=64MiB, t=3, p=2`). Email + password signup with verification; forgot / reset flow revokes every active session on success. Account lockout after 5 failed logins in a 15-min window.
- **API:** session cookie (browser-originated) or bearer JWT. HS256 default with `PLOWERED_JWT_HS256_SECRET`; RS256 optional via `PLOWERED_JWT_RS256_PRIVATE_KEY` / `_PUBLIC_KEY`. OIDC adapter slot reserved.
- **MFA / SSO:** OIDC + MFA + SAML + SCIM are on the production-hardening sprint.
- **MCP HTTP:** bearer token. Stdio MCP: filesystem permissions.
- **Connector workers:** signed tokens scoped to one connector instance.

## Authorization

Two-tier model — workspace **roles** + per-resource **ABAC rules**.

### Roles + verb matrix

Canonical source: `internal/core/policy/policy.go`. Mirrored in the frontend at `web/src/lib/hooks/use-role.ts`.

| Role | Verbs |
|---|---|
| `viewer` | `read` |
| `editor` | `read · edit · propose · run` |
| `steward` | `read · edit · propose · certify · run` |
| `admin` | `read · edit · propose · certify · delete · run · admin` |
| `super_admin` | every verb above plus `purge` (permanently delete from the recycle bin) |

### How enforcement happens

- **HTTP layer (the gate that matters).** Every gated handler calls `gate(w, r, authz, verb, resourceType)` from `internal/api/http/authz.go` right after `mustTenant`. The helper writes 403 with the engine's reason string on deny. Unauthenticated → 401.
- **Policy engine.** `policy.NewEngine(d.Policies)` is initialized once in `NewMux`. It applies role grants first, then consults the `policy_rules` table for per-resource overrides. **Deny rules override allow.**
- **ABAC conditions** today: `principal.role`, `principal.group`, `resource.tag`, `resource.owner=self`.
- **Frontend.** `useRole()` mirrors the same role/verb table so destructive buttons (Approve cert, Delete connection, New connection, …) hide for users who would 403 anyway. Cosmetic — security still happens on the backend.

### Endpoint coverage

Every `/v1/*` endpoint in `internal/api/http/` routes through `gate()` — read endpoints to `VerbRead` (or stricter where the data is sensitive), mutating endpoints to `VerbEdit / VerbRun / VerbDelete / VerbAdmin / VerbCertify / VerbPropose / VerbPurge` per the table below. There is no longer a `requireAdmin()` parallel path or an inline `policy.HasRole()` check — every authorization decision flows through `policy.Engine.Allow()` and respects per-resource ABAC rules.

End-to-end deny tests live in `internal/api/http/rbac_test.go` (every role × representative endpoint per family — catalog, stats, policies, audit, deleted bin, legal holds, DSR, team, notify, contracts, certifications, cost, pipelines, checks).

| Resource | Read | Write / create / edit | Delete | Run / approve |
|---|---|---|---|---|
| asset (catalog + lineage + column lineage + profile) | `read` | `edit` | `delete` | propose cert: `propose`; refresh profile: `run`; describe via AI: `edit`; reindex search: `admin` |
| pipeline (incl. runs + run logs) | `read` | `edit` | `delete` | trigger: `run` |
| check | `read` | `edit` | `delete` | run: `run` |
| contract | `read` | `edit` | `delete` | evaluate: `run` |
| certification | `read` | propose: `propose` | n/a | approve / reject / revoke: `certify` |
| classification | `read` | apply: `certify` | — | preview/scope: `admin` (samples warehouse) |
| glossary_term | `read` | `edit` | `delete` | — |
| notify_channel / notify_rule | `read` | `admin` | `admin` | — |
| cost_budget | `read` | `admin` | `admin` | — |
| connection | `read` | `admin` | `delete` | test / crawl / classify: `admin` |
| ai_provider | `read` | `admin` | `admin` | test / primary: `admin` |
| migration | `read` | `edit` | `delete` | run: `admin` (touches production warehouse) |
| ai_query (Ask / Text-to-SQL) | history: `read` | n/a | n/a | draft + run: `run` (burns BYOM tokens) |
| job | `read` | n/a | n/a | — |
| policy_rule (meta-RBAC + access preview) | `admin` | `admin` | `admin` | — |
| team_member | `read` | role change: `admin` | remove: `admin` | — |
| invite | `admin` | `admin` | revoke: `admin` | — |
| audit_event | `admin` | append-only (writer is the platform itself) | n/a | — |
| recycle_bin_entry | `admin` | restore: `admin` | purge: `purge` (super_admin only) | — |
| dsr_request / legal_hold | `admin` | `admin` | `admin` | — |
| mcp transport | `read` (on asset; per-tool authz inside) | n/a | n/a | — |

## Multi-tenancy

- `tenant_id NOT NULL` on every row.
- `context.Context` carries `tenant_id`. Storage methods derive from `ctx`, never from request payload.
- Custom analyzer flags any SQL referencing tenant ID from a request body or query param.
- Optional schema-per-tenant for regulated customers.

## Secret management

- Never in metadata graph; `properties` jsonb is treated as untrusted user content.
- `Vault` interface for connector credentials. File-based dev impl refuses to start in production.
- URN references only: `vault://warehouse/prod/password`.
- `.env` gitignored; CI fails on commit.
- DB connection strings, JWT signing keys, LLM keys all via vault.

## Input validation

- Every gRPC request runs `Validate()`.
- `properties`: 1 MiB size cap, depth 8.
- Parameterized SQL only. String concatenation = build failure.
- Search input escaped before backend.
- React default escaping in UI; no `dangerouslySetInnerHTML` on user content.

## Audit log

Append-only `audit_events` table:
```
event_id, tenant_id, actor_id, actor_kind, action, resource_type,
resource_id, before_json, after_json, ip, user_agent, request_id, created_at
```

- Mutation RPCs emit events in same DB transaction.
- `INSERT`-only via role grants.
- Daily export to object storage with object-lock.

## LLM safety

- Untrusted data wrapped in `<asset_metadata>` tags; system prompt treats as data.
- Structured outputs via JSON schema / tool use.
- No tool execution from inside the agent in v0.
- Per-tenant cost budgets in `cost_budgets` with warn + hard thresholds (24h dedupe). `/v1/ask` per-tenant rate limit is on the production-hardening sprint.
- Every generation logged in `ai_query_executions`; prompt + completion mirrored to S3 with deterministic keys; cost rolled up by `cost.Recorder`.

## Network security

- TLS 1.3 only at edge. HTTPS redirect.
- HSTS preload. `SameSite=Lax` cookies.
- Strict CSP set globally by `SecurityHeadersMW`; `X-Frame-Options: DENY`, Referrer-Policy, Permissions-Policy lockdown, COOP + CORP.
- Rate limiting — per-IP token bucket on auth endpoints, per-principal limiter on the rest (120 read / 30 write per min). RFC 9239 `RateLimit-Limit / Remaining / Reset` headers everywhere.
- `WebhookChannel` (notify dispatcher) — SSRF guard rejects RFC 1918 / link-local / `0.0.0.0` / metadata endpoints before any outbound request. A full SSRF audit is on the production-hardening sprint.
- No CORS wildcards.

## Supply chain

- `go.sum` committed; `go mod verify` in CI.
- Vulnerability scan on every PR.
- Container base: distroless, non-root, pinned by digest.
- CI actions pinned by SHA.
- SBOM per release.

## Crypto

- Stdlib `crypto/tls`.
- Passwords: `argon2id` (`m=64MiB, t=3, p=2`).
- Connector secrets at rest: AES-GCM-256, keys from vault.
- No homegrown crypto.

## Disclosure

`security@plowered.dev` (placeholder). Triage within 72h. Fix or mitigation within 30 days for High/Critical.
