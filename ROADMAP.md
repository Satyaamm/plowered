# Roadmap

## M0 — Repo bootstrap
- README, ARCHITECTURE, DESIGN, SECURITY, ROADMAP, CONTRIBUTING
- Repo skeleton (proto/, cmd/, internal/, web/, deploy/)
- go.mod, Makefile, .gitignore, docker-compose
- First proto files (catalog, lineage, context, connector, mcp)
- Domain types, validation, Store interface, in-memory impl + tests

## M1 — Core graph
- PostgreSQL storage layer with migrations
- Asset / Edge / Tag CRUD via `CatalogService`
- Multi-tenancy (row-level)
- gRPC server bootstrap + REST gateway
- `ploweredctl` CLI
- Auth + tenant + audit + rate-limit interceptors
- Exit: 100K assets, 3-hop lineage < 200ms

## M2 — First connector
- Connector framework (`internal/connectors/shared`)
- Relational-database connector
- Sync run history, scheduled syncs, error reporting
- Exit: catalog points at its own DB and lists every table

## M3 — Lineage parser
- SQL AST parsing (single dialect first)
- Source→target column-level lineage from `SELECT … INSERT INTO …`, CTAS, views
- Persist edges with `transformation_id`
- LineageService gRPC: upstream / downstream / impact
- Exit: transformation graph for a 50-model project

## M4 — Search & web UI v0
- Embedded search index, kept in sync via event bus
- Web app: home, search, asset detail, lineage graph
- Auth: dev-only magic-link, then OIDC
- Exit: non-engineer can find the right table by keyword

## M5 — MCP server
- `plowered-mcp` stdio binary
- Tools: `search_assets`, `get_asset`, `get_lineage`, `get_glossary_term`
- HTTP/SSE transport mounted on main server
- Exit: an MCP-compliant agent answers questions against a Plowered instance

## M6 — Context Agents v0
- `pkg/llm` provider abstraction
- DescriptionAgent
- QualityAgent
- Review queue UI
- Eval table
- Exit: 80%+ of generated descriptions approved by a steward in <2 clicks

## M7 — Warehouse connector
- Metadata + query history → assets + lineage
- Incremental sync (last_modified watermark)
- Exit: first design partner runs Plowered against their warehouse

## M8 — Transformation-tool connector
- Parse model + manifest artifacts; refs become lineage
- Run results → freshness signals
- Exit: end-to-end lineage from raw → mart → BI

## M9 — Cloud preview
- Multi-tenant SaaS deploy
- SSO via third-party identity provider
- Billing
- Public sign-up with free tier
- Exit: outside developer can sign up, connect a warehouse, see their catalog

## Beyond
- Lakehouse / OLAP / BI / orchestrator connectors
- MetricAgent + GlossaryAgent
- Open table-format context store
- SAML / SCIM
- Audit log export to SIEM
- Helm chart + IaC modules
- Eval / trace dashboards
