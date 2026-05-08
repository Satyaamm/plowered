# Contributing

## Before opening a PR

- Read `SECURITY.md` and `DESIGN.md`.
- Discuss non-trivial changes in an issue first.
- CI must pass: proto lint, `go vet`, `go test -race`, vulnerability scan.

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

- All public RPCs in `proto/plowered/v1/*.proto`.
- Additive changes only on `v1`. Breaking goes in `v2`.

## Security-sensitive PRs

Touching authn/authz, storage, secrets, audit, or the LLM pipeline → request `security-review` label, two reviewers.

## Commits

Conventional Commits: `feat(graph): add column-level lineage walker`. Squash on merge.
