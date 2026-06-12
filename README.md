# Plowered

A **data context platform** — a catalog, governance, and lineage layer that
data teams (and the AI agents that act on their behalf) can trust. Go-native,
single-binary, BYO-LLM, multi-tenant from day one.

---

## What ships today

| Surface | What it does |
|---|---|
| **Catalog** | Assets, qualified-name lookups, lineage edges, glossary, classifications, owners, tags, two-phase classify (preview → approve → apply) |
| **Search** | Lexical + semantic. Vector backend is pluggable per tenant: pgvector (default), Pinecone, Weaviate, or Qdrant |
| **AI / BYOM** | OpenAI, Anthropic, DeepSeek, Gemini, Azure OpenAI, AWS Bedrock (Claude / Titan / Llama / Mistral / Cohere), GCP Vertex AI (Gemini / Claude / textembedding), Cohere, Voyage. Keys sealed in the AES-256-GCM vault; SSRF guard on every outbound URL |
| **Certifications** | Steward propose → admin approve / reject → revoke workflow with a pending queue at `/certifications` |
| **Data contracts** | Per-asset `expected_columns` / `freshness` / `null_thresholds`; periodic `contract.Runner` (5-min tick) emits dedup'd breach events |
| **Migrations** | Schema migration runner with full and **incremental** modes (S3-backed `ObjectStore` watermarks). Long migrations run async via Asynq with bookmarkable `/jobs/{id}` progress |
| **Async jobs** | Asynq (Redis Streams) or in-process sync mode. Per-job detail page at `/jobs/{id}` |
| **Connectors** | Postgres, Snowflake, Redshift, MongoDB, DynamoDB, Athena, BigQuery (service-account or workload-identity auth) |
| **Notify** | `events.Bus` → routing to Slack / Email (Resend) / Webhook / Log. Bounded async worker pool with inline fallback; `last_delivered_at` per rule |
| **Cost** | Per-model price book, per-feature roll-ups (`/v1/cost/summary?by_feature=1`), tenant cost budgets with warn / hard thresholds (`cost.Watcher` emits `CheckFailed` with 24h dedupe) |
| **Auth** | Email + Argon2id password, server-side sessions (HttpOnly + Secure cookies, revocable), forgot-password + reset, 5-fails-in-15min lockout, invite-based teammate onboarding |
| **RBAC** | Six roles: `viewer` / `editor` / `steward` / `admin` / `super_admin` / `platform_admin`. Verbs: `Read` / `Edit` / `Propose` / `Certify` / `Delete` / `Run` / `Admin` / `Purge` / `Platform` |
| **Feedback** | In-product feedback drawer (auto-captures URL + UA); cross-tenant triage queue at `/admin/feedback` gated by `platform_admin` |
| **MCP** | Native Model Context Protocol server (`cmd/plowered-mcp`) so any MCP-aware agent can read the catalog under your policy engine |
| **Rate limiting** | Per-IP token bucket on auth endpoints + per-principal limiter on the rest (120 read / 30 write / min). RFC 9239 `RateLimit-*` headers |
| **Security headers** | HSTS, strict CSP, `X-Frame-Options: DENY`, Referrer-Policy, Permissions-Policy, COOP + CORP — set globally by `SecurityHeadersMW` |
| **Compliance** | Legal holds, admin DSR + self-service GDPR (Art. 15/17/20), audit trail, recycle bin |
| **API** | REST + JSON over `/v1/*`. OpenAPI 3.1 spec at `/openapi.yaml`, Swagger UI at `/docs` |

---

## Tech stack

| Layer | Choice |
|---|---|
| Core | Go 1.26 |
| API | net/http + OpenAPI 3.1 (Swagger UI at `/docs`) |
| Metadata store | PostgreSQL 16 (pgx/v5), migrations through 0025. In-memory mode for dev/tests |
| Vector store | pgvector / Pinecone / Weaviate / Qdrant (per-tenant config) |
| Event bus | `events.Bus` (in-process) + NATS / JetStream outbox relay for cross-process delivery |
| Object storage | `blob.ObjectStore` with S3 + local-FS backends; mirrors profile + AI transcripts and DSR exports |
| Async jobs | Asynq (Redis Streams). Falls back to in-process sync when `PLOWERED_REDIS_URL` is unset |
| Secrets | AES-256-GCM sealed envelopes; key from `PLOWERED_SECRETS_MASTER_KEY` |
| Email | Resend (production) or `LogSender` (dev — link prints to logs) |
| Frontend | Next.js 15.5 + TypeScript + Fluent UI v9 ("Loamy" theme) + TanStack Query |
| AuthN | Argon2id passwords; server-side sessions (HttpOnly + Secure + SameSite=Lax), 14-day TTL, revocable |
| Edge | nginx with rate-limit zones + RFC 9239 headers + `X-Gateway-Auth` shared-secret check; Next.js BFF injects the secret server-side so browsers never see it |

---

## Binaries

| Binary | Purpose |
|---|---|
| `cmd/plowered` | HTTP + gRPC API process. Embeds the scheduler |
| `cmd/plowered-worker` | Asynq consumer for pipeline runs, quality checks, crawls, classify, reindex |
| `cmd/plowered-mcp` | Model Context Protocol server |
| `cmd/ploweredctl` | CLI for migrations + admin chores |

---

## Quickstart — local dev

The fastest path. `run.sh` brings up Postgres + Redis + NATS + MinIO in
Docker, then starts the API, worker, and Next.js dev server on the host
with hot reload.

```bash
# Prereqs: Docker Desktop, Go ≥ 1.24, Node ≥ 20.
git clone https://github.com/Satyaamm/plowered.git
cd plowered

cp .env.example .env                 # then edit secrets as needed
./run.sh                             # starts everything; Ctrl-C to stop
```

Open http://localhost:3000. Sign up, check your inbox for the verification
link (or grab the token from the API logs if Resend isn't configured), log
in — the first user in a workspace gets `super_admin` + `admin`.

To stop everything (local processes + infra containers):

```bash
./run.sh --stop
```

---

## Production deploy (single VPS)

A self-contained stack: Postgres 16 + Redis 7 + NATS (JetStream) + MinIO +
the API + worker + web + nginx + certbot. Two networks: a public one for
nginx, a private internal-only one for everything else so the database
never gets a public address.

```bash
# 1. Mint production secrets (chmod 600 .env.production).
./deploy/secrets-gen.sh

# 2. Bootstrap Let's Encrypt for both subdomains. Run once.
./deploy/init-certs.sh you@yourdomain.com

# 3. Bring the stack up.
docker compose --env-file .env.production -f docker-compose.production.yml up -d
```

Topology: browsers hit `plowered.s2datasystems.in/api/*`, the Next.js Edge
middleware injects `X-Gateway-Auth`, then rewrites to the API over the
internal Docker network. Direct API access via `ploweredapi.s2datasystems.in`
requires callers to send the gateway header themselves.

---

## API exploration

- **Swagger UI** → http://localhost:8080/docs
- **Raw OpenAPI** → http://localhost:8080/openapi.yaml
- **Metrics** → http://localhost:8080/metrics (Prometheus exposition)

---

## BYOM (bring your own model)

`Management → AI providers` lists every supported kind. Pick one, paste
credentials, click **Test** — the platform performs a credential-check
call against the upstream (burns no tokens, just validates auth) and
unlocks **Save** on success. Keys are sealed under
`urn:plowered:aiprovider:<id>` in the AES-256-GCM vault.

| Provider kind | Auth |
|---|---|
| `anthropic`, `openai`, `deepseek`, `openai_compatible` | API key |
| `gemini` | Google AI Studio key |
| `azure_openai` | API key OR Azure AD (managed identity / client-secret) |
| `bedrock` | AWS access key OR workload identity (IRSA / EC2 role) |
| `vertex` | GCP service-account JSON OR workload identity |
| `cohere`, `voyage` | API key |

Mark a config primary for `chat` or `embed` and it becomes the tenant
default for the corresponding feature (glossary auto-write, semantic
search, `/ask`, asset describer, etc.).

---

## Vector stores

Each tenant configures one vector store at `Management → Vector stores`.
Backends:

| Kind | Notes |
|---|---|
| `pgvector` | Bundled with Postgres; default. In-process cosine scan over `asset_embeddings` |
| `pinecone` | Managed; API key + index name |
| `weaviate` | Managed or self-hosted; URL + API key |
| `qdrant` | Self-hosted or cloud; URL + API key |

Switching backends is a per-tenant operation; the resolver picks the
configured store on each search call.

---

## Tests

```bash
go test ./...                        # all Go tests
go test ./internal/api/http/...      # API tests (memory-backed)
cd web && npx tsc --noEmit           # web type-check
```

The consolidated e2e test (`internal/api/http/e2e_test.go`) exercises
signup → invite → accept → list members in-process with the MemoryRepo.

---

## Repo layout

```
plowered/
├── cmd/                       # plowered, plowered-worker, plowered-mcp, ploweredctl
├── internal/
│   ├── core/                  # Domain packages — auth, catalog, glossary, jobs,
│   │                          #   aiprovider, classifier, search, policy, feedback,
│   │                          #   vectorstore, …
│   ├── adapters/              # Source + warehouse + vectorstore drivers
│   ├── api/http/              # REST handlers + middleware + OpenAPI spec
│   ├── storage/
│   │   ├── postgres/          # pgxpool stores + embedded migrations 0001-0025
│   │   └── memory/            # In-memory stores for dev/tests
│   ├── worker/                # Asynq + sync enqueuers + handlers
│   └── server/                # Listener wiring + middleware chain
├── pkg/llm/                   # Provider-agnostic LLM interface
├── web/                       # Next.js frontend
└── deploy/                    # Docker + nginx + Let's Encrypt recipes
```

---

## Contact

Maintainer: [@Satyaamm](https://github.com/Satyaamm)

License: proprietary — contact the maintainer for evaluation terms.
