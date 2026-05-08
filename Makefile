.PHONY: help proto build test lint clean dev migrate docker-up docker-down web-dev fmt

GO         ?= go
BUF        ?= buf
BIN_DIR    ?= bin
MODULE     := github.com/Satyaamm/plowered

help:
	@echo "Plowered dev targets:"
	@echo "  make proto       Generate Go code from .proto files (requires buf)"
	@echo "  make build       Build all binaries into ./bin"
	@echo "  make test        Run unit tests"
	@echo "  make lint        Lint Go + proto"
	@echo "  make fmt         Format Go + proto"
	@echo "  make dev         Run plowered server with reload (requires air)"
	@echo "  make docker-up   Start local dev stack (postgres, nats, meilisearch)"
	@echo "  make docker-down Stop local dev stack"
	@echo "  make migrate         Apply DB migrations"
	@echo "  make test-postgres   Run integration tests against local PostgreSQL"
	@echo "  make web-dev     Start Next.js frontend"
	@echo "  make clean       Remove build artifacts"

proto:
	$(BUF) lint
	$(BUF) generate

build:
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/plowered ./cmd/plowered
	$(GO) build -o $(BIN_DIR)/plowered-mcp ./cmd/plowered-mcp
	$(GO) build -o $(BIN_DIR)/ploweredctl ./cmd/ploweredctl

test:
	$(GO) test ./... -race -count=1

lint:
	$(GO) vet ./...
	$(BUF) lint

fmt:
	$(GO) fmt ./...
	$(BUF) format -w

dev:
	air -c .air.toml

docker-up:
	docker compose up -d

docker-down:
	docker compose down

migrate:
	$(GO) run ./cmd/ploweredctl migrate up --db "$${PLOWERED_DATABASE_URL:-postgres://plowered:plowered@localhost:5432/plowered?sslmode=disable}"

test-postgres:
	PLOWERED_TEST_DATABASE_URL="$${PLOWERED_TEST_DATABASE_URL:-postgres://plowered:plowered@localhost:5432/plowered?sslmode=disable}" \
		$(GO) test -race -count=1 ./internal/storage/postgres/...

web-dev:
	cd web && npm run dev

clean:
	rm -rf $(BIN_DIR) proto/gen
