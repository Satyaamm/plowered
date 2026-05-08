# Plowered

## Tech stack

| Layer | Choice |
|---|---|
| Core engine | Go 1.23+ |
| API contracts | Protocol Buffers + gRPC + REST gateway |
| Metadata store (prod) | PostgreSQL with graph & vector extensions |
| Metadata store (dev) | SQLite |
| Search | Embedded full-text index |
| Event bus | Lightweight messaging system |
| LLM | Provider-agnostic via `pkg/llm` interface |
| MCP | Native Go Model Context Protocol implementation |
| Frontend | TypeScript + a modern web framework |
| Auth | OIDC |
| Object storage | S3-compatible |
| Deployment | Container image + single-binary mode |
| Observability | Standard metrics + tracing |

## Repo layout

```
plowered/
├── README.md ARCHITECTURE.md DESIGN.md SECURITY.md AI.md ROADMAP.md CONTRIBUTING.md
├── Makefile go.mod buf.yaml buf.gen.yaml docker-compose.yml .gitignore
│
├── proto/plowered/v1/
│   ├── common.proto
│   ├── catalog.proto
│   ├── lineage.proto
│   ├── context.proto
│   ├── connector.proto
│   └── mcp.proto
│
├── cmd/
│   ├── plowered/
│   ├── plowered-mcp/
│   └── ploweredctl/
│
├── internal/
│   ├── core/
│   │   ├── auth/
│   │   ├── graph/
│   │   ├── lineage/
│   │   ├── search/
│   │   └── context/
│   ├── connectors/shared/
│   ├── api/
│   ├── storage/
│   └── server/
│
├── pkg/
├── web/
└── deploy/
```

## Quickstart

```bash
brew install go protobuf bufbuild/buf/buf

git clone https://github.com/Satyaamm/plowered.git
cd plowered
go mod tidy
make proto
make build

docker compose up -d
make migrate
./bin/plowered serve
```

## Contact

Maintainer: [@Satyaamm](https://github.com/Satyaamm)
