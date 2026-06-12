# VPS deploy runbook

Fully self-contained stack: Postgres, Redis, NATS, MinIO, API, worker,
web, nginx, certbot — all containers in one compose file. nginx is the
only thing the internet can reach.

## Before first deploy

1. **DNS** — two A records pointing at the VPS:
   - `plowered.s2datasystems.com` (web)
   - `ploweredapi.s2datasystems.com` (direct API)
   Change them in `.env.production` (`WEB_DOMAIN` / `API_DOMAIN` /
   `PLOWERED_SESSION_COOKIE_DOMAIN`) if you use different hosts.
2. **Firewall** — open 80 + 443 only. Do NOT open 5432/6379/6543/7479;
   the data planes have no published host ports.
3. **Docker** — Docker Engine + the compose plugin on the VPS.

Note on ports: the bundled Postgres listens on **6543** and Redis on
**7479** *inside the cluster* because the VPS already runs services on
5432/6379. Neither container binds any host port, so there is no
host-level conflict regardless — the custom ports just remove all
ambiguity.

## First deploy

```bash
git clone https://github.com/Satyaamm/plowered.git && cd plowered

# 1. Mint .env.production — fills every credential with a fresh
#    cryptographically-random value (chmod 600). Save the printed
#    gateway secret somewhere safe; CLI/SDK callers need it.
./deploy/secrets-gen.sh

# 2. Issue Let's Encrypt certs for both domains (reads the domains
#    from .env.production). One-shot; renewals are automatic after.
./deploy/init-certs.sh admin@s2datasystems.com

# 3. Build + start everything.
docker compose --env-file .env.production -f docker-compose.production.yml up -d --build

# 4. Watch until the API logs "plowered listening".
docker compose -f docker-compose.production.yml logs -f plowered-api
```

Then open https://plowered.s2datasystems.com, sign up (first user in a
workspace becomes its admin), and grab the verification link from the
API logs while `PLOWERED_EMAIL_PROVIDER=log`:

```bash
docker compose -f docker-compose.production.yml logs plowered-api | grep verify
```

Switch to real email later by setting `PLOWERED_EMAIL_PROVIDER=resend`
(+ API key) or `ses` (+ region, AWS creds) and restarting the API.

## Scaling

nginx resolves upstreams per request through Docker DNS, so:

```bash
docker compose --env-file .env.production -f docker-compose.production.yml up -d --scale plowered-api=3
```

round-robins across replicas with no config change.

## Upgrades

```bash
git pull
docker compose --env-file .env.production -f docker-compose.production.yml up -d --build
```

Schema migrations run automatically at API boot.

## Secret rotation

```bash
./deploy/secrets-gen.sh --force    # re-mints EVERYTHING
docker compose --env-file .env.production -f docker-compose.production.yml up -d --force-recreate
```

Caveats: rotating `PLOWERED_SECRETS_MASTER_KEY` orphans secrets already
sealed in the vault (re-enter AI provider keys after), and rotating the
JWT secret signs everyone out. Postgres/Redis/MinIO passwords change in
the env but the data volumes keep the OLD passwords — for those, either
wipe the volumes (data loss) or change the passwords inside the
running services first.

## Backups

Not automated. Minimum viable:

```bash
docker compose -f docker-compose.production.yml exec postgres \
  pg_dump -p 6543 -U plowered plowered | gzip > plowered-$(date +%F).sql.gz
```

Cron that daily + ship it off the VPS.

## Edge protection summary (nginx)

- TLS 1.2/1.3 only, Let's Encrypt certs, OCSP stapling
- Per-IP rate limits: 5 r/s on `/v1/auth/*` (brute-force surface), 30 r/s general
- Bad-bot user-agent filter (403, unlogged)
- `X-Gateway-Auth` required on the direct API host — random scanners
  never reach the app
- Security headers (HSTS, CSP, COOP/CORP, …) on every response
- 25 MB edge body cap; the app enforces its own 2 MiB JSON cap +
  application/json-only content gate (BodyGuardMW)
- `/metrics` allow-listed to docker bridge CIDRs
