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

- **API:** OIDC. JWT (RS256). Access ≤15 min; refresh 30 days with rotation. `aud=plowered`.
- **Service-to-service:** mTLS.
- **MCP HTTP:** OIDC bearer. Stdio MCP: filesystem permissions.
- **Connector workers:** signed tokens scoped to one connector instance.

## Authorization

- Workspace roles: `viewer`, `editor`, `steward`, `admin`.
- Per-asset policies (ABAC) overlay roles. JSON expression evaluated at query time.
- Verbs: `read`, `edit`, `propose`, `certify`, `delete`, `admin`.
- Enforced in storage layer, not handlers.

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
- Per-tenant token budgets.
- Every generation logged in `evals`.

## Network security

- TLS 1.3 only at edge. HTTPS redirect.
- HSTS preload. `SameSite=Lax` cookies.
- CSP: `default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; img-src 'self' data: https:;`
- gRPC TLS for non-localhost.
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
