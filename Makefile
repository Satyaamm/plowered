.PHONY: help env proto build test lint clean dev migrate docker-up docker-down web-dev web-install fmt

GO        ?= go
BUF       ?= buf
NPM       ?= npm
BIN_DIR   ?= bin
MODULE    := github.com/Satyaamm/plowered
ENV_FILE  ?= .env

# Load .env into make's environment when it exists, so targets see the same
# values as the running binary.
-include $(ENV_FILE)
export

help:
	@echo "Plowered dev targets:"
	@echo "  make env             Bootstrap .env from .env.example if missing"
	@echo "  make proto           Generate Go code from .proto files (requires buf)"
	@echo "  make build           Build all binaries into ./bin"
	@echo "  make test            Run all unit tests"
	@echo "  make test-postgres   Run integration tests against local PostgreSQL"
	@echo "  make lint            Lint Go + proto"
	@echo "  make fmt             Format Go + proto"
	@echo "  make dev             Run plowered server with reload (requires air)"
	@echo "  make docker-up       Start local dev stack"
	@echo "  make docker-down     Stop local dev stack"
	@echo "  make migrate         Apply DB migrations"
	@echo "  make web-install     Install web dependencies"
	@echo "  make web-dev         Start the Next.js frontend"
	@echo "  make clean           Remove build artifacts"

env:
	@if [ ! -f .env ]; then cp .env.example .env && echo "→ created .env from .env.example"; else echo "→ .env exists; skipping"; fi
	@if [ ! -f web/.env.local ]; then cp web/.env.example web/.env.local && echo "→ created web/.env.local from web/.env.example"; else echo "→ web/.env.local exists; skipping"; fi

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

test-postgres:
	$(GO) test -race -count=1 ./internal/storage/postgres/...

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
	$(GO) run ./cmd/ploweredctl migrate up

web-install:
	cd web && $(NPM) install

web-dev:
	cd web && $(NPM) run dev

clean:
	rm -rf $(BIN_DIR) proto/gen
