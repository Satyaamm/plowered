# Commands

Every command you might need, grouped by task. Copy-paste ready.

---

## Table of contents

- [First-time setup](#first-time-setup)
- [Local dev](#local-dev)
- [Clean slate](#clean-slate-stop--prune--rebuild)
- [Production deploy](#production-deploy)
- [Database](#database)
- [Auth / users](#auth--users)
- [Tests](#tests)
- [Build](#build)
- [Smoke tests](#smoke-tests)
- [Logs](#logs)
- [Git](#git)
- [Troubleshooting](#troubleshooting)

---

## First-time setup

```bash
git clone https://github.com/Satyaamm/plowered.git
cd plowered

cp .env.example .env                            # edit any secrets you want to change
cd web && npm install && cd ..                  # install web deps
```

Prereqs on the host: Docker Desktop running, Go ≥ 1.24, Node ≥ 20.

---

## Local dev

`run.sh` brings up Postgres + Redis + NATS + MinIO in Docker, then starts
the API, worker, and Next.js dev server on the host with hot reload. All
three stream to one terminal with coloured `[api]` / `[worker]` / `[web]`
prefixes.

```bash
./run.sh                # start everything; Ctrl-C to stop the local procs
./run.sh --stop         # stop local procs AND the infra containers
./run.sh --infra        # only bring up the infra containers
```

After `./run.sh` is up:

- Web → http://localhost:3000
- API → http://localhost:8080
- Swagger UI → http://localhost:8080/docs
- MinIO console → http://localhost:9001 (user `minioadmin`, pw `minioadmin`)
- Prometheus metrics → http://localhost:8080/metrics

---

## Clean slate (stop · prune · rebuild)

When you want to start fresh — DB wiped, volumes gone, build caches cleared.

```bash
# 1. Stop local processes + infra.
./run.sh --stop

# 2. Nuke Docker volumes (DB / Redis / MinIO / NATS data all gone).
docker compose down -v
docker compose -f docker-compose.production.yml down -v 2>/dev/null

# 3. Reclaim disk: ALL unused containers / networks / images / volumes / build cache.
docker system prune -af --volumes
docker builder prune -af

# 4. Clean web build cache + reinstall.
rm -rf web/.next web/node_modules web/.turbo
cd web && npm install && cd ..

# 5. Clean Go build cache.
go clean -cache -testcache
# Heavier — only run if you suspect a corrupt module cache:
# go clean -modcache

# 6. Bring it back up.
./run.sh
```

---

## Production deploy

```bash
# 1. Mint production secrets (writes .env.production, chmod 600).
./deploy/secrets-gen.sh
./deploy/secrets-gen.sh --force                 # rotate every secret (bounces all containers)

# 2. Bootstrap Let's Encrypt certs for both subdomains. Idempotent — first run only.
./deploy/init-certs.sh you@yourdomain.com

# 3. Build all three images from scratch.
docker compose --env-file .env.production -f docker-compose.production.yml build --no-cache --pull

# 4. Bring the stack up.
docker compose --env-file .env.production -f docker-compose.production.yml up -d

# 5. Tail logs until you see "listening on :8080".
docker compose -f docker-compose.production.yml logs -f plowered-api plowered-web

# 6. Take it down.
docker compose --env-file .env.production -f docker-compose.production.yml down

# 7. Restart one service after a code change (rebuilds that image).
docker compose --env-file .env.production -f docker-compose.production.yml up -d --build plowered-api
```

---

## Database

```bash
# Open a psql shell.
docker exec -it plowered-postgres psql -U plowered -d plowered

# One-shot query.
docker exec plowered-postgres psql -U plowered -d plowered -c "SELECT count(*) FROM users;"

# Apply migrations.
go run ./cmd/ploweredctl migrate up
go run ./cmd/ploweredctl migrate down       # roll back one step
go run ./cmd/ploweredctl migrate status     # show current migration version

# Quick backup (local dev — NOT production-grade).
docker exec plowered-postgres pg_dump -U plowered plowered > backup-$(date +%Y%m%d).sql

# Quick restore.
cat backup.sql | docker exec -i plowered-postgres psql -U plowered -d plowered
```

---

## Auth / users

```bash
# List every user with workspace + roles + verified flag.
docker exec plowered-postgres psql -U plowered -d plowered -c "
SELECT u.email,
       NULLIF(u.full_name, '') AS name,
       t.name AS workspace,
       tm.roles::text AS roles,
       u.email_verified_at IS NOT NULL AS verified,
       u.created_at::timestamp(0) AS created
FROM users u
LEFT JOIN tenant_memberships tm ON tm.user_id = u.id
LEFT JOIN tenants t ON t.id = tm.tenant_id
ORDER BY u.created_at;
"

# Sign up a new user (admin of a new workspace).
curl -s -X POST http://localhost:8080/v1/auth/signup \
  -H 'Content-Type: application/json' \
  -d '{
    "email":"you@yourdomain.com",
    "password":"YourPasswordHere!",
    "first_name":"Your",
    "last_name":"Name",
    "workspace_name":"Your Workspace",
    "accept_terms":true
  }'

# Mark a user email-verified without round-tripping email.
docker exec plowered-postgres psql -U plowered -d plowered -c "
UPDATE users SET email_verified_at = now() WHERE email = 'you@yourdomain.com';
"

# Find the verification token in the API logs (when Resend isn't configured).
grep -E "verify|token=" /tmp/plowered-run.log | tail -5

# Login (returns a Set-Cookie session).
curl -s -X POST http://localhost:8080/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@yourdomain.com","password":"YourPasswordHere!"}' -i

# Grant platform_admin (cross-tenant feedback queue access) to an existing user.
docker exec plowered-postgres psql -U plowered -d plowered -c "
UPDATE tenant_memberships
SET roles = roles || '[\"platform_admin\"]'::jsonb
WHERE user_id = (SELECT id FROM users WHERE email = 'you@yourdomain.com');
"
```

---

## Tests

```bash
go test ./...                                  # all Go tests
go test -race ./...                            # with the race detector
go test -run TestXYZ ./internal/api/http/      # single test by name
go test -v ./internal/core/policy/             # one package, verbose
go test -cover ./...                           # with coverage

cd web && npx tsc --noEmit                     # web type-check (no emit)
cd web && npm run lint                         # eslint
```

---

## Build

```bash
make build              # builds plowered + plowered-worker + plowered-mcp + ploweredctl into ./bin
make proto              # regenerates Go from .proto via buf
make lint               # go vet + buf lint
make fmt                # go fmt + buf format

# Build a single production image manually (no compose).
docker build -f deploy/docker/Dockerfile.api    -t plowered-api    .
docker build -f deploy/docker/Dockerfile.worker -t plowered-worker .
docker build -f deploy/docker/Dockerfile.web    -t plowered-web    web

# Inspect image size.
docker images | grep plowered
```

---

## Smoke tests

```bash
# API health.
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8080/healthz
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8080/readyz

# Web responds.
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:3000/

# Metrics (Prometheus exposition).
curl -s http://localhost:8080/metrics | head -20

# OpenAPI spec.
curl -s http://localhost:8080/openapi.yaml | head -30

# Container health from inside Docker.
docker exec plowered-api    wget -qO- http://localhost:8080/healthz
docker exec plowered-web    wget -qO- http://localhost:3000/
docker exec plowered-redis  redis-cli ping
docker exec plowered-nats   wget -qO- http://localhost:8222/healthz
```

---

## Logs

```bash
# Local dev (run.sh streams everything into one file).
tail -f /tmp/plowered-run.log
grep -E "ERROR|WARN" /tmp/plowered-run.log

# Production compose.
docker compose -f docker-compose.production.yml logs -f                  # all services
docker compose -f docker-compose.production.yml logs -f plowered-api     # one service
docker compose -f docker-compose.production.yml logs --tail=200 nginx    # last 200 lines
```

---

## Git

```bash
git status
git diff
git log --oneline -20

# Commit + push (the project keeps small themed commits).
git add <files>
git commit -m "..."
git push origin main

# What did the latest push include?
git log origin/main..HEAD --stat
```

---

## Troubleshooting

```bash
# Container stuck in restart loop? Show why.
docker inspect plowered-postgres --format '{{ .State.Health }}'
docker compose -f docker-compose.production.yml logs --tail=50 plowered-postgres

# Port 5432 / 3000 / 8080 already in use? Find the offender.
lsof -i :5432
lsof -i :3000
lsof -i :8080

# Docker out of disk space.
docker system df
docker system prune -af --volumes      # see "Clean slate" above

# pgvector extension missing (search index broken).
docker exec plowered-postgres psql -U plowered -d plowered -c "CREATE EXTENSION IF NOT EXISTS vector;"

# Let's Encrypt cert renewal stuck.
docker compose -f docker-compose.production.yml logs certbot

# Reset a user's password from the DB directly (Argon2id hash via a one-liner).
# Replace EMAIL and NEWPASSWORD.
docker exec -it plowered-api ploweredctl users set-password \
  --email EMAIL --password 'NEWPASSWORD'
```
