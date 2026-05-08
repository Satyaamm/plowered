# Architecture

## Components

```
                     ┌──────────────────────────────────────────────┐
                     │                  Web UI                      │
                     │   Browse · Search · Lineage · Glossary       │
                     └───────────────────┬──────────────────────────┘
                                         │ gRPC-Web / REST
                                         ▼
   MCP clients ──────►  ┌────────────────────────────────────────┐
                        │           Plowered API Server          │
                        │  ┌──────────────────────────────────┐  │
                        │  │  gRPC services (proto-defined)   │  │
                        │  │  · CatalogService                │  │
                        │  │  · LineageService                │  │
                        │  │  · ContextService                │  │
                        │  │  · ConnectorService              │  │
                        │  │  · MCPService                    │  │
                        │  └──────────────────────────────────┘  │
                        │              │                         │
                        │              ▼                         │
                        │  ┌──────────────────────────────────┐  │
                        │  │           Core Engine            │  │
                        │  │  graph · lineage · search ·      │  │
                        │  │  context · auth · evals          │  │
                        │  └──────────────────────────────────┘  │
                        └───────┬────────────┬────────────┬──────┘
                                │            │            │
                                ▼            ▼            ▼
                        ┌────────────┐ ┌──────────┐ ┌──────────────┐
                        │ PostgreSQL │ │ Event    │ │ Object       │
                        │ + graph    │ │ bus      │ │ storage      │
                        │ + vector   │ │          │ │              │
                        └────────────┘ └──────────┘ └──────────────┘
                                ▲
                                │
                        ┌───────┴───────────────────────────────┐
                        │           Connector Workers          │
                        └──────────────────────────────────────┘
```

## Data model

### Nodes
- `Asset` — `database`, `schema`, `table`, `view`, `column`, `dashboard`, `report`, `transform_model`, `pipeline`, `ml_model`
- `GlossaryTerm`
- `User` / `Group`
- `Tag` / `Classification`

### Edges
- `LINEAGE` (asset → asset, directed)
- `OWNED_BY` (asset → user/group)
- `TAGGED_AS` (asset → tag)
- `DEFINES` (term → asset)
- `DEPENDS_ON` (asset → asset)

### Common envelope
`id (uuid)`, `qualified_name`, `tenant_id`, `type`, `created_at`, `updated_at`, `properties (jsonb)`, `embedding (vector)`.

Source of truth: `proto/plowered/v1/`.

## Storage

- **Production:** PostgreSQL — assets, edges, glossary_terms, tags, policies, evals, traces. Graph queries via recursive CTEs or a graph extension. Vector extension for embeddings. Trigram index for fuzzy fallback.
- **Dev:** SQLite, same schema.
- **Search:** embedded full-text index.
- **Blobs:** S3-compatible bucket.

## Context Pipeline

```
UNIFY ──► BOOTSTRAP ──► COLLABORATE ──► ACTIVATE
```

- UNIFY — `internal/connectors/*` + `internal/core/graph`
- BOOTSTRAP — `internal/core/context/agents/*`
- COLLABORATE — review queues, approval workflow, RBAC
- ACTIVATE — `cmd/plowered-mcp` + gRPC services + warehouse-side UDFs

## API surface

`proto/plowered/v1/`:
- `CatalogService`
- `LineageService`
- `ContextService`
- `ConnectorService`
- `MCPService`

REST is generated from `google.api.http` annotations.

## Connector framework

```go
type Connector interface {
    Info() ConnectorInfo
    Validate(ctx, Config) error
    Crawl(ctx, Config, Sink) error
    Lineage(ctx, Config, ...) error
}
```

## AI context layer

- DescriptionAgent
- MetricAgent
- GlossaryAgent
- QualityAgent
- EvalAgent

Provider abstraction: `pkg/llm`. Prompts versioned in `internal/core/context/prompts/`. Every generation logged in `evals`.

## MCP

Tools:
- `search_assets`
- `get_asset`
- `get_lineage`
- `get_glossary_term`
- `propose_query`

Transports: stdio (`plowered-mcp`) and HTTP/SSE (mounted on the API server).

## Multi-tenancy

- Row-level isolation: `tenant_id NOT NULL` on every table.
- Optional schema-per-tenant for regulated customers.
- RBAC verbs: `read`, `edit`, `propose`, `certify`, `delete`, `admin`.

## Performance budget

| Operation | Target |
|---|---|
| Asset lookup by qualified name | < 5ms p99 |
| Search (10 results, 1M assets) | < 100ms p99 |
| 3-hop lineage (100 nodes) | < 200ms p99 |
| Full crawl, 100K-asset warehouse | < 30 min |
| Cold-start single binary | < 2s |
| Memory at idle (10K assets, SQLite) | < 200MB |
