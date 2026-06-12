# Roadmap

Status legend: **shipped** | partial | planned

## M0 — Repo bootstrap — **shipped**
- README, ARCHITECTURE, DESIGN, SECURITY, ROADMAP, CONTRIBUTING, SCHEMA, RUN
- Repo skeleton (`cmd/`, `internal/`, `web/`, `deploy/`)
- go.mod, Makefile, .gitignore, docker-compose stack (postgres / redis / nats / minio / api / worker / web)
- Domain types, validation, Store interface, in-memory + Postgres impls + tests

## M1 — Core graph — **shipped**
- PostgreSQL storage layer with migrations (now numbered through 0022)
- Asset / Edge / Tag CRUD via `CatalogService`
- Multi-tenancy (row-level on every table; `tenant_id` enforced at handler + storage)
- REST API + middleware chain
- `ploweredctl` CLI
- Auth + tenant + audit + rate-limit + security-headers middleware
- Exit hit: catalog scales to 100K assets, 3-hop lineage < 200ms

## M2 — First connector — **shipped**
- Connector framework (`internal/adapters/*`)
- Postgres source adapter (Tester + Crawler + Executor)
- Sync run history, scheduled syncs, error reporting
- Crawl auto-tags PII / PHI / PCI / secret columns by name

## M3 — Lineage parser — partial
- Asset-level edges (`edges` table) shipped
- Column-level lineage table (`column_lineage`) shipped — schema only; an
  extractor that mines `ai_query_executions` + pipeline SQL tasks into edges
  is the next concrete lineage build
- Persisted with `transformation_id` / `task_run_id`

## M4 — Search & web UI v0 — **shipped**
- Local deterministic embeddings; switchable to any BYOM provider
- Next.js 15.5 + Fluent UI v9 ("Loamy" theme) app:
  home, search, asset detail with Overview / Schema / Lineage / Activity /
  Contract panels, lineage graph, /ask, /catalog, /pipelines, /runs,
  /migrations, /checks, /contracts, /alerts, /certifications, /cost,
  /admin/audit, /admin/deleted, /legal-holds, /dsr, /connections, /team,
  /identity, /settings/ai, /account
- Auth: real signup + email verification + sessions + invites + RBAC
- Exit hit: non-engineer can find the right table by keyword

## M5 — MCP server — **shipped**
- `cmd/plowered-mcp` stdio binary
- Tools: `search_assets`, `get_asset`, `get_lineage`, `get_glossary_term`
- HTTP/SSE transport mounted on the main server

## M6 — Context Agents v0 — **shipped**
- `pkg/llm` provider abstraction (Anthropic / OpenAI / DeepSeek / openai-compatible)
- DescriptionAgent (column auto-describe with mirror-to-S3 transcripts)
- QualityAgent / AskerAgent / ClassifierAgent / Profiler
- Review queue UI (certification workflow + propose/approve/reject)
- Eval / audit table — every generation logged

## M7 — Warehouse + non-relational connectors — **shipped**
- Live drivers: Postgres, Snowflake, Redshift, MongoDB, DynamoDB, Athena, BigQuery
- All seven registered in `warehouse.MultiFactory` + the source adapter registry
- Incremental sync (last_modified watermark) for migrations
- Production validation against real clusters tracked under the hardening sprint

## M8 — Data quality + contracts — **shipped**
- Quality checks (row_count / not_null / freshness / uniqueness / custom_sql)
- Notify dispatcher with Slack / Email (Resend) / Webhook / Log channels
- Data contracts (`expected_columns` / `freshness` / `null_thresholds`)
- Periodic `contract.Runner` (5-min tick) emits dedup'd `CheckFailed` breach events
- Cost tracking — per-model price book, per-feature roll-ups, budgets with warn / hard thresholds

## M9 — Cloud preview — partial
- Multi-tenant: row-level isolation is in. Stress / fuzz validation is on
  the hardening sprint.
- SSO via OIDC: adapter slot reserved, not wired
- Billing: cost-tracking dashboard ships; vendor billing reconciliation is on
  the hardening sprint
- Public sign-up with free tier: signup + invite + verification ships;
  productisation (free tier + entitlements + checkout) is planned

## Production-hardening sprint (gates before the first paying customer)

1. Production-load behaviour (100-col profile, 1M-row migrations, 10K events/sec notify)
2. New driver validation against real Mongo / Dynamo / Athena / BigQuery
3. Tenant-isolation fuzz + `migration/builders.go` SQL safety review
4. Notify against real Resend / Slack workspaces
5. Fault injection (Redis death mid-migration, S3 503, LLM rate-limit)
6. Playwright click-through on every new page
7. OWASP top-10 review (SSRF on WebhookChannel, CSRF on new POSTs, per-tenant /v1/ask limit)
8. Vendor cost reconciliation against real billing exports
9. Backups + restore drill for Postgres + S3 mirror
10. OTel + Sentry + SLOs (API 99.9% / p95 < 500ms, worker 99% / p95 < 5s)

## What's wired but not yet validated against real infra

The platform ships these surfaces functionally; they compile, register,
and have unit-level seam tests. They have **not** been driven against
live external services in this repo. Set the listed env var to `1` to
opt in to the integration tests once you have credentials.

| Surface | Build artefact | Integration env var | What it validates |
|---|---|---|---|
| AWS Bedrock LLM | `internal/adapters/bedrock_provider` | `PLOWERED_TEST_BEDROCK=1` + standard AWS creds | InvokeModel against a real region; per-model wire-format dispatch |
| GCP Vertex AI LLM | `internal/adapters/vertex_provider` | `PLOWERED_TEST_VERTEX=1` + ADC | OAuth2 token flow, Gemini + Claude-on-Vertex routing |
| Pinecone vector DB | `internal/adapters/pinecone_vectorstore` | `PLOWERED_TEST_PINECONE=1` + `PLOWERED_PINECONE_*` env | upsert + query + delete; namespace isolation |
| Weaviate vector DB | `internal/adapters/weaviate_vectorstore` | `PLOWERED_TEST_WEAVIATE=1` | batch-objects + nearVector + per-id delete |
| Qdrant vector DB | `internal/adapters/qdrant_vectorstore` | `PLOWERED_TEST_QDRANT=1` | upsert + search + delete |
| Mongo / Dynamo / Athena | the corresponding `*_source` packages | `PLOWERED_TEST_<X>=1` | Tester + Crawler against a real cluster |
| Notify Slack channel | `internal/core/notify` | `PLOWERED_TEST_SLACK=1` + webhook URL | live POST against Slack |
| Notify Resend channel | `internal/core/notify` | `PLOWERED_TEST_RESEND=1` + verified domain key | live send via Resend |

These belong on the production-hardening sprint above. Honest framing:
the seams are correct; the failure modes a real call would surface
(403 from a misconfigured IAM role, 404 from a missing Bedrock model
opt-in, a Pinecone index dimension mismatch, etc.) need a real account
to find.

## Resolver swap (semantic search → vectorstore.Build)

The `vectorstore` package's CRUD + seam + Pinecone / Weaviate / Qdrant /
pgvector adapters are wired. A tenant can configure a vector store and
mark it primary. **What's not yet done:** `search.Indexer`,
`describer`, and `asker` still call the pgx-backed `EmbeddingStore`
directly. They need a thin `vectorstore.Resolver` that looks up the
tenant's primary config, calls `vectorstore.Build`, and memoises the
resulting `Store`. When no config is set the fall-back is pgvector
(which is now itself a registered backend, so the fallback is one line
of code rather than a parallel branch). Estimated effort: ~2 hours
spanning search.go, asker.go, describer.go.

## Beyond — planned

- **Column-level lineage extractor** — mine `ai_query_executions` + pipeline SQL into `column_lineage`; unlocks impact analysis
- **Workflow approvals** — generalise the certification propose/approve pattern for schema changes / contract changes / asset deletes
- **Marketplace / data products** — asset bundles → publishable products with descriptions + certification + contracts + owners
- **Anomaly detection** — profile snapshots persist across runs; flag week-over-week row-count drops or null-fraction creep
- **MFA / SAML / SCIM** — beyond the OIDC slot
- **Helm chart + IaC modules**
- **Eval / trace dashboards** — OTel + LLM eval pipeline
- **Audit log export to SIEM**
