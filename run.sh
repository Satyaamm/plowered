#!/usr/bin/env bash
#
# run.sh — single-command local dev for Plowered.
#
# What this does:
#   1. Brings up the infrastructure containers (postgres, redis, nats, minio)
#      via docker compose — leaves them running across restarts.
#   2. Makes sure the master.key from the secrets-init volume is mirrored
#      into ./.secrets/master.key so the locally-run Go services can read it.
#   3. Starts the Go API, Go worker, and Next.js dev server on the host
#      with hot-reload-friendly env vars. All three log to the same terminal
#      with coloured [api] / [worker] / [web] prefixes.
#   4. Ctrl-C cleanly kills every child process; the infra containers stay up.
#
# Usage:
#   ./run.sh           # start everything
#   ./run.sh --stop    # stop the local processes (you started another way)
#                        AND the infra containers
#   ./run.sh --infra   # only bring up the infra containers and exit
#
# Prereqs: Docker Desktop running, Go ≥ 1.24 on PATH, Node ≥ 20 on PATH,
# web/node_modules already installed (`cd web && npm install`).

set -euo pipefail

# ---------------------------------------------------------------------------
# Locate repo root + colours
# ---------------------------------------------------------------------------
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$REPO_ROOT"

C_API='\033[1;36m'    # cyan
C_WORK='\033[1;35m'   # magenta
C_WEB='\033[1;33m'    # yellow
C_INFO='\033[1;32m'   # green
C_WARN='\033[1;31m'   # red
C_RESET='\033[0m'

info() { printf "${C_INFO}[run]${C_RESET} %s\n" "$*"; }
warn() { printf "${C_WARN}[run]${C_RESET} %s\n" "$*" >&2; }

# ---------------------------------------------------------------------------
# Optional subcommands
# ---------------------------------------------------------------------------
case "${1:-}" in
  --stop)
    info "stopping infra containers …"
    docker compose stop postgres redis nats minio plowered plowered-worker web 2>/dev/null || true
    info "done."
    exit 0
    ;;
  --infra)
    info "bringing up infra containers only …"
    docker compose up -d postgres redis nats minio
    info "infra up. exit code 0."
    exit 0
    ;;
esac

# ---------------------------------------------------------------------------
# Step 1 — make sure the dockerised services we DON'T want running are down,
# and the ones we DO need are up.
# ---------------------------------------------------------------------------
info "stopping dockerised api/worker/web (we'll run them on the host) …"
docker compose stop plowered plowered-worker web >/dev/null 2>&1 || true

info "bringing up postgres, redis, nats, minio …"
docker compose up -d postgres redis nats minio >/dev/null

info "waiting for postgres health …"
for _ in $(seq 1 30); do
  if docker exec plowered-postgres pg_isready -U plowered >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

# ---------------------------------------------------------------------------
# Step 2 — make sure the master.key file is available locally for both Go
# binaries. We copy it once from the secrets-init docker volume.
# ---------------------------------------------------------------------------
mkdir -p .secrets
if [[ ! -s .secrets/master.key ]]; then
  info "extracting master.key from the docker volume …"
  docker run --rm \
    -v plowered_secretsdata:/s \
    -v "$REPO_ROOT/.secrets:/out" \
    busybox sh -c "cp /s/master.key /out/master.key && chmod 600 /out/master.key" >/dev/null
fi

# ---------------------------------------------------------------------------
# Step 3 — env for both the API and the worker.
#
# Plowered's config loader reads process env first, then .env. Exporting
# here pre-empts the broken `.env` defaults that ship with inline
# comments like `PLOWERED_JWT_RS256_PUB_KEY=  # PEM contents …` — those
# comments survive as the value and break the API at boot.
# ---------------------------------------------------------------------------
export PLOWERED_ENV=dev
export PLOWERED_HTTP_ADDR=:8080
export PLOWERED_GRPC_ADDR=:9090
export PLOWERED_DATABASE_URL='postgres://plowered:plowered@localhost:5432/plowered?sslmode=disable'
export PLOWERED_REDIS_URL='redis://localhost:6379/0'
export PLOWERED_NATS_URL='nats://localhost:4222'
export PLOWERED_CORS_ALLOWED_ORIGINS='*'
export PLOWERED_WEB_BASE_URL='http://localhost:3000'
export PLOWERED_WORKER_CONCURRENCY=10
export PLOWERED_SECRETS_MASTER_KEY_FILE="$REPO_ROOT/.secrets/master.key"

# JWT: use HS256 for local dev. The secret has to match between the
# token issuer (login handler) and verifier (auth middleware), so we
# pin both via env. Explicit empty-string for RS256 prevents Plowered's
# .env from injecting a stray comment as the public key.
export PLOWERED_JWT_HS256_SECRET="${PLOWERED_JWT_HS256_SECRET:-dev-only-jwt-secret-do-not-use-in-prod-this-is-local-only}"
export PLOWERED_JWT_RS256_PUB_KEY=""
export PLOWERED_JWT_ISSUER="${PLOWERED_JWT_ISSUER:-plowered}"
export PLOWERED_JWT_AUDIENCE="${PLOWERED_JWT_AUDIENCE:-plowered}"

# ---------------------------------------------------------------------------
# Step 4 — kick off the three host processes, prefix each line of output.
# ---------------------------------------------------------------------------
PIDS=()
cleanup() {
  warn "shutting down host processes …"
  for pid in "${PIDS[@]:-}"; do
    if kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
    fi
  done
  # Give them a beat to exit cleanly, then force-kill any stragglers.
  sleep 1
  for pid in "${PIDS[@]:-}"; do
    if kill -0 "$pid" 2>/dev/null; then
      kill -9 "$pid" 2>/dev/null || true
    fi
  done
  info "done. infra containers are still running — use './run.sh --stop' to take them down."
}
trap cleanup INT TERM EXIT

# helper: stream a child's combined stdout/stderr with a coloured prefix.
prefix() {
  local color="$1" tag="$2"
  awk -v p="$(printf "%s[%s]%s " "$color" "$tag" "$C_RESET")" '{ print p $0; fflush(); }'
}

info "starting API on :8080 …"
( go run ./cmd/plowered 2>&1 | prefix "$C_API" "api" ) &
PIDS+=($!)

info "starting worker …"
( go run ./cmd/plowered-worker 2>&1 | prefix "$C_WORK" "worker" ) &
PIDS+=($!)

# Web: only fire if web/node_modules is present.
if [[ -d web/node_modules ]]; then
  info "starting Next.js on :3000 …"
  (
    cd web
    PLOWERED_API_BASE=http://localhost:8080 \
    NEXT_PUBLIC_APP_NAME=plowered \
      npm run dev 2>&1 | prefix "$C_WEB" "web"
  ) &
  PIDS+=($!)
else
  warn "web/node_modules missing — skipping the frontend. Run 'cd web && npm install' once, then re-run ./run.sh."
fi

info "all three started. Open http://localhost:3000. Ctrl-C to stop everything."

# Portable watchdog: poll every 2s and bail when any child PID has
# died. macOS ships Bash 3.2 which doesn't support `wait -n`, so we
# can't lean on it here.
while :; do
  sleep 2
  for pid in "${PIDS[@]}"; do
    if ! kill -0 "$pid" 2>/dev/null; then
      warn "process $pid exited; tearing down the rest …"
      exit 1
    fi
  done
done
