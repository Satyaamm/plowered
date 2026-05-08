# AI Capabilities

Plowered uses AI on two sides: **inbound** (agents that fill gaps in the metadata graph) and **outbound** (surfaces that feed external AI agents).

## Inbound — Context Agents

All agents run queue-driven, never blocking user requests. Every generation is logged to the `evals` table.

### DescriptionAgent
| | |
|---|---|
| Input | asset + lineage neighborhood + sample query history |
| Output | 1–3 sentence business description (proposed, not authoritative) |
| Trigger | new asset created · existing asset description empty · stale beyond N days |

### MetricAgent
| | |
|---|---|
| Input | repeated SQL patterns across query history |
| Output | metric definition (name, formula, dimensions, owner candidate) |
| Trigger | scheduled scan of query history |

### GlossaryAgent
| | |
|---|---|
| Input | asset descriptions + column names + query patterns |
| Output | candidate business terms grouped by domain |
| Trigger | scheduled |

### QualityAgent
| | |
|---|---|
| Input | asset metadata + freshness + ownership presence + downstream usage |
| Output | 0–100 trust score + rationale + per-component breakdown |
| Trigger | on every asset update · scheduled refresh |

### ClassifierAgent
| | |
|---|---|
| Input | column names + sample values (PII-safe) + lineage |
| Output | classification proposals: PII, GDPR, HIPAA, custom |
| Trigger | on connector crawl · scheduled rescan |

### EvalAgent
| | |
|---|---|
| Input | external agent's question + answer + cited assets |
| Output | groundedness score + trace |
| Trigger | `ContextService.Evaluate` RPC |

## Outbound — Activation surfaces

How AI agents consume Plowered's context.

| Surface | Use case |
|---|---|
| **MCP server** (stdio + HTTP/SSE) | Local and remote MCP-compliant agents |
| **gRPC + REST** | Programmatic access with full RBAC |
| **SQL UDFs** | Warehouse-side functions that inline descriptions, tags, owners with query results |
| **Generated SDKs** | Python / TypeScript / Go from proto contracts |

### MCP tool list (M5)

- `search_assets(query, filters)` → ranked asset list
- `get_asset(qualified_name)` → full asset rendered as markdown
- `get_lineage(asset, direction, depth)` → subgraph
- `get_glossary_term(term)` → definition + linked assets
- `propose_query(business_question)` → SQL grounded in the catalog

## Provider abstraction

`pkg/llm` interface:
```go
type Provider interface {
    Generate(ctx, GenerateRequest) (GenerateResponse, error)
    Embed(ctx, EmbedRequest) (EmbedResponse, error)
    Stream(ctx, GenerateRequest) (Stream, error)
}
```

Implementations are hot-swappable per tenant.

### Routing
- Per-tenant default model
- Per-agent override
- Failover on provider error
- Cheap-model triage → escalate to frontier when confidence low

## Prompt management

- Versioned files under `internal/core/context/prompts/<agent>/<version>.md`
- Every prompt change ships through PR review
- Production tenants pinned to a prompt version
- A/B testing: split tenants across versions, compare eval scores

## Evaluation framework

`evals` table:
```
eval_id, tenant_id, agent, model, prompt_version, input_hash,
output, reviewer_id, reviewer_disposition, groundedness_score,
latency_ms, tokens_in, tokens_out, cost_estimate, created_at
```

Reviewer disposition: `approved`, `edited`, `rejected`, `expired`.

Tracked metrics:
- Approval rate per agent / model / prompt version
- Time-to-approval
- Reviewer edit distance (Levenshtein over output)
- Groundedness score over time
- Cost per approved description

## Safety & guardrails

- Untrusted data wrapped in `<asset_metadata>` tags; system prompt instructs the model to treat tag contents as data only.
- Structured outputs (JSON schema / tool use). Free text only for the description slot, escaped before storage.
- No tool execution from inside the agent in v0. Agents propose; humans (or a deterministic backend) execute.
- Per-tenant monthly token budget — refuses requests once exceeded.
- PII redaction before any LLM call. Column samples are never sent.
- No customer data leaves the customer's perimeter when running in air-gapped mode (offline LLM required).

## Cost management

- Prompt caching for the per-tenant system prompt (repeated across thousands of generations per sync).
- Cheap-model triage; escalate only when the small model flags uncertainty.
- Batch processing for bulk crawls.
- Per-tenant monthly token budget.
- `context_generations_total{model,outcome}` and `llm_tokens_total{tenant,model}` exposed for cost dashboards.

## Embeddings

- Stored on `assets.embedding` via the vector extension.
- Refresh: on description change, on schema change, scheduled monthly.
- Used for: semantic search, similar-asset suggestion, glossary clustering.
- Embedding model is provider-pluggable; default is the cheapest model that scores acceptably on internal eval set.

## AI-specific roadmap

### M6 — v0 agents
- DescriptionAgent
- QualityAgent
- Eval table populated
- Review queue UI
- Provider abstraction with one default + one fallback

### M10 — agent expansion
- MetricAgent
- GlossaryAgent
- ClassifierAgent
- A/B prompt testing infrastructure
- Embedding-based semantic search in the web UI

### M12 — closed-loop
- EvalAgent grounded in external agent traces
- Auto-tagging proposals (PII, sensitivity) with reviewer queue
- Auto-link suggestions for cross-system glossary unification
- Cost dashboard

### M14+ — advanced
- Multi-step agents that propose schema changes / refactors
- Retrieval-augmented MCP responses (auto-attach lineage to `get_asset`)
- Per-tenant fine-tuned reranker
- Offline / air-gapped LLM mode
