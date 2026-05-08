# Design

## Scale targets (per tenant)

| Dimension | Year 1 | Year 2 | Year 3 |
|---|---|---|---|
| Assets | 1M | 10M | 100M |
| Edges | 5M | 50M | 500M |
| Active users | 100 | 1k | 10k |
| Search QPS (peak) | 10 | 100 | 1k |
| Connector syncs / day | 100 | 1k | 10k |
| MCP tool calls / day | 10k | 1M | 100M |

## Scaling strategy

- Vertical first; read replicas next; tenant-sharded last.
- Indexes: `(tenant_id, qualified_name)`, `(tenant_id, type)`, `(tenant_id, updated_at)`.
- Search index rebuildable from the graph; never authoritative.
- Lineage traversal bounded: depth (default 3, max 10), node count (default 500, max 5K).
- Connector workers consume from event bus, horizontally scalable. Per-connector concurrency caps.
- Context Agents: queue-driven. Prompt caching on system prompt. Cheap-model triage; escalate uncertain.

## SLOs (v0)

| SLI | SLO |
|---|---|
| API availability | 99.9% |
| API latency p99 (search) | < 300ms |
| API latency p99 (asset CRUD) | < 100ms |
| Connector sync success rate | > 95% |

## Failure isolation

- Connector failures cannot take down the API.
- LLM provider outages cannot take down the catalog.
- Search backend down → fall back to `ILIKE` (degraded, logged).

## Data durability

- Streaming replication.
- Daily logical backups to object storage with versioning.
- RPO 24h free / 1h enterprise. RTO 1h enterprise.

## Idempotency

- `Idempotency-Key` header on every mutation. 24h uniqueness.
- Connector crawl key: `(connector_instance_id, run_id)`.

## Consistency

- Strong read-after-write per tenant on primary.
- Eventual: graph → search index (target lag < 1s).
- Eventual: trust scores.
- Strong: lineage edges with their endpoint assets (same DB transaction).

## Observability

### Logs
- Structured JSON via `slog`.
- Required fields: `ts`, `level`, `msg`, `request_id`, `tenant_id`, `user_id`, `service`, `version`.

### Metrics
- `/metrics` endpoint.
- RED per gRPC method.
- USE for storage and search.
- Business: `assets_total{tenant}`, `lineage_edges_total{tenant}`, `context_generations_total{model,outcome}`.

### Traces
- OTLP export. Trace IDs propagated through event-bus messages.

### Alerts
- p99 latency breaches SLO for 10 min
- Error rate > 1% for 5 min
- Connector sync failure > 10% for 1 hour
- LLM provider error rate > 5% for 15 min
- Audit log write failures (any)

## Deployment topologies

- **Single binary:** embedded SQLite + embedded search. Up to ~100K assets.
- **Compose:** PostgreSQL + event bus + plowered. Up to ~1M assets.
- **Cluster:** API + MCP + worker deployments + managed PostgreSQL + event-bus operator + search StatefulSet.
- **Air-gapped:** distroless binary + PostgreSQL + offline LLM. No external network.

## Coding norms (enforced)

1. Errors are values; wrap with `%w`. No `panic` outside `main`.
2. `context.Context` first arg on every I/O function. Always propagate.
3. No globals except metrics. Configuration is constructor-injected.
4. Interfaces declared at the consumer side.
5. No `interface{}` / `any` without justification; prefer generics.
6. Test coverage ≥ 70% on `internal/core`, `internal/storage`.
7. One file = one concept; ~500 lines max.
8. Public names get a doc comment starting with the identifier.
9. No struct embedding for code reuse (only for interface satisfaction).
10. Locks travel with the data they protect.

## API design rules

1. Public surface defined in `proto/plowered/v1/` first.
2. Breaking changes require a new package version (`v2`).
3. Resource names follow AIP-122: `tenants/{tenant}/assets/{asset}`.
4. gRPC canonical error codes only.
5. Pagination follows AIP-158 (`page_token`).
6. Filtering follows AIP-160 (CEL).
7. Long-running ops follow AIP-151.

## Failure modes

| Failure mode | Detection | Mitigation |
|---|---|---|
| Single-tenant lineage explosion | Pre-write edge count check | Reject parse, surface as connector error |
| Pathological search query | Per-tenant rate limit, per-query CPU budget | 429 + retry-after |
| LLM provider timeouts | Circuit breaker on `pkg/llm` | Switch to fallback; queue depth alert |
| Connection storm during sync | Bounded pool, per-tenant cap | Workers throttle, event-bus replay |
| 500MB blob in `properties` | 1 MiB cap at Validate | Reject `InvalidArgument` |
| Compromised connector worker | Worker token scoped to one instance | Revoke token; audit shows blast radius |
