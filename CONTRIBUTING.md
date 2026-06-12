# Contributing

## Before opening a PR

- Read `SECURITY.md` and `DESIGN.md`.
- Discuss non-trivial changes in an issue first.
- CI must pass: `go vet`, `go test -race ./...`, `cd web && npx tsc --noEmit`, vulnerability scan.

## Coding norms

See `DESIGN.md` §coding-norms. Highlights:
- Errors are values; wrap with `%w`. No `panic` outside `main`.
- `context.Context` first arg on every I/O function.
- No globals except metrics. Constructor-injected configuration.
- Interfaces declared at the consumer side.
- Tests required (≥70% on `internal/core`, `internal/storage`).
- One file = one concept; ~500 lines max.
- Public names get a doc comment starting with the identifier.

## API changes

- All public endpoints defined in `internal/api/http/openapi.yaml` (embedded via `go:embed`).
- Additive changes only on `/v1/*`. Breaking goes in `/v2/*`.
- Every new handler ships with the matching `useX` hook in `web/src/lib/hooks/` and a docs entry under `docs/README.md`.

## Security-sensitive PRs

Touching authn/authz, storage, secrets, audit, notify dispatcher (SSRF surface), or the LLM pipeline → request `security-review` label, two reviewers.

## Commits

Conventional Commits with a multi-section body grouping backend / frontend / tests / known limitations. Example:

```
feat(contract): periodic runner with dedup'd breach detection

backend: contract.Runner ticks every 5m; emits ContractBreach events
frontend: inline editor on asset Overview tab
tests: …
limitations: …
```

**No emojis** anywhere — commits, docs, code, chat. Squash on merge.
