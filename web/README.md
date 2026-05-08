# Web

Frontend for Plowered.

## Stack

- TypeScript (strict)
- A modern React framework
- gRPC-Web client (generated from `proto/plowered/v1/*.proto`)
- A graph-visualization library for lineage

## Pages (M4 target)

- `/` — recent + top assets, quick search
- `/search?q=` — global search
- `/asset/[qualifiedName]` — asset detail, lineage, columns, quality
- `/glossary` — business glossary
- `/connectors` — connector instance management
- `/admin` — users, policies, audit log
