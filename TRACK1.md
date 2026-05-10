# Track 1 — what's pitchable today

This document captures the demo state at the end of Track 1. If a
prospective customer or investor asks "what does Plowered actually do",
walk them through this script.

## The pitch in one sentence

> Plowered is an open-source data context platform — catalog, governance,
> and AI-native lineage — with built-in tamper-evident audit, GDPR DSRs,
> and litigation-hold gates that fire at the runtime layer instead of as
> compliance theatre.

## What works end-to-end

| Capability                              | Where it lives                  | Demo state |
|-----------------------------------------|---------------------------------|------------|
| Workspace signup + email verification   | `/signup`, Resend, `users` + `tenants` + `email_verifications` | ✅ |
| Cookie-based session auth               | `plowered_session` HttpOnly cookie, `sessions` table | ✅ |
| Connection CRUD + secrets vault         | `/v1/connections`, AES-256-GCM, `secrets` table | ✅ |
| Test a connection                       | pgx handshake with 5s timeout, persists health | ✅ |
| Schema crawler (Postgres)               | Async via Asynq, walks `information_schema` | ✅ |
| Auto-PII tagging                        | 17 column-name rules, 6 tag classes | ✅ |
| Catalog browse                          | Fluent DataGrid with sort + filter + type tabs | ✅ |
| Asset detail with tabs                  | Overview · Schema · Lineage · Quality · Activity | ✅ |
| Tamper-evident audit chain              | SHA-256 prev_hash / row_hash on every authenticated request | ✅ |
| Recycle bin                             | Tombstones with restore + super_admin purge | ✅ |
| Legal holds                             | 409 at delete time with hold_id surfaced | ✅ |
| GDPR DSRs                               | 30-day SLA clock stamped at receipt | ✅ |
| Outbox-pattern event relay              | Polls Postgres, publishes to NATS / log | ✅ (no producers wired) |

## The 5-minute demo

1. Open <http://localhost:3000/signup>. Type workspace + email + password.
   Show the verification email arriving (or pull the token from the DB
   if you're on a test Resend key).
2. Sign in. Land on the home page — getting-started checklist shows
   "Workspace ✓ Email verified ✓ Connect a datasource ⃝ Crawl ⃝".
3. Connections → New connection. Wire up the bundled Postgres
   (`postgres` host, `plowered` db, password `plowered`).
4. Test connection → health goes green.
5. Crawl → 397 assets land in the catalog in ~150ms.
6. Catalog → drill into `users` table → Schema tab shows 16 columns,
   with `class:pii` / `class:secret` badges on email, full_name,
   password_hash, mfa_secret. Activity tab shows your own request
   history via the audit chain.
7. Issue a legal hold on `pipeline`. Try to delete a pipeline →
   HTTP 409 with the hold ID. Release the hold → delete now succeeds
   and the row appears in the recycle bin until a super_admin purges.
8. Compliance → DSR. File an erasure request. Show the 30-day clock.

## What an Atlan / Collibra prospect cares about

- **It's open source.** Self-hosted, no per-asset pricing.
- **Audit-by-design.** Every read and write is hash-chained from the
  application layer. Three sentences in HIPAA / SOC2 paperwork map
  directly to columns in `audit_events`.
- **Governance gates fire at runtime.** Legal holds aren't a
  spreadsheet — a delete on a held resource literally returns 409 with
  the hold ID in the response body. Auditors test this.
- **AI-native foundations.** The MCP server (Track 3) lets Claude / GPT
  query the catalog with the same tenant + policy context as a human.

## What's intentionally not here yet

These are scoped for Track 2 / Track 3:

- DAG-style pipeline editor + real ETL runners (Track 2.1, 2.2).
- Live log streaming on runs (Track 2.3).
- Quality check designer with threshold history (Track 2.4).
- Column-level lineage from dbt manifests / SQL parsing (Track 2.5).
- Glossary CRUD + asset linking (Track 3.1).
- Classification auto-detection from data samples (Track 3.2).
- "What can user X see" access viewer (Track 3.3).
- Live MCP server (Track 3.4).
- Conversational asset search (Track 3.5).

## What we owe to be production-ready

A small but real list:

- **Resend domain verification** — until done, signups outside the
  Resend account holder's email require a manual token pull. RUN.md
  documents the workaround.
- **DataGrid scale** — the catalog query returns up to 500 assets in
  one call. Server-side cursor pagination is a Track 2 task.
- **Outbox producers** — the relay is running and would publish to
  NATS, but no domain mutations write outbox rows yet.
- **Enterprise tenant table population** — every domain row carries
  `tenant_id`, but `tenant_memberships` is the *only* multi-row tenant
  surface today. Invite flow is Track 1 follow-up.
- **The `tin → SSN` PII false positive** — substring match, fixable
  with word boundaries.

## Backlog visible to operators

- `idempotency_keys`, `feature_flags`, `encryption_keys` tables exist
  but are not yet wired by domain code.
- Per-tenant Asynq queues are routed to `default` until a queue
  discovery mechanism lands.

These don't block the pitch — flag them so technical buyers know we're
honest about scope.
