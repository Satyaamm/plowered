# Plowered — production deploy on a single VPS

This is the runbook for the configuration the repo ships:

- frontend on **plowered.s2datasystems.in**
- backend on **ploweredapi.s2datasystems.in**
- everything in Docker on a single VPS
- nginx terminates TLS via Let's Encrypt (certbot)
- Postgres + Redis + NATS + MinIO live in the same compose stack

Replace the two domains with yours if different — they're only
referenced in `.env.production`, `deploy/nginx/conf.d/*.conf`, and
`deploy/init-certs.sh`.

---

## 1. What the stack looks like

```
                                  internet
                                     |
                                     v
                              ┌──────────────┐
                              │ nginx :80/443│   (TLS termination, rate
                              │  (certbot)   │    limit, gateway gate,
                              └─┬───────────┬┘    security headers)
                                │           │
              plowered.*        │           │  ploweredapi.*
                                v           v
                       ┌─────────────┐  ┌─────────────┐
                       │ plowered-web│  │ plowered-api│  (gateway-auth MW,
                       │  (Next.js   │  │  (Go API)   │   RBAC, session)
                       │   middleware│  └─┬─────┬─────┘
                       │  injects    │    │     │
                       │  gateway hdr│    │     │
                       └─────────────┘    │     │
                              │           │     │
                              └───────────┘     │
                                                │
              ┌─────────────────────┬───────────┼──────────────────┐
              v                     v           v                  v
        ┌──────────┐         ┌─────────┐    ┌───────┐         ┌────────┐
        │ postgres │         │  redis  │    │ nats  │         │ minio  │
        └──────────┘         └─────────┘    └───────┘         └────────┘
                                                                      ^
                                       ┌──────────────────┐           │
                                       │ plowered-worker  │───────────┘
                                       │  (Asynq consumer)│
                                       └──────────────────┘
```

Browsers only ever touch the two `*.s2datasystems.in` names. Every
data store is on the `private_net` docker network with `internal:true`
— no egress, no exposed port. Only nginx straddles `public_net` and
`private_net`.

The **gateway-auth** is a second wall around the API. Even if a
scanner figures out where ploweredapi is, every request without the
`X-Gateway-Auth: <secret>` header gets a 401 before the auth handler
runs. The frontend's Next.js middleware (`web/src/middleware.ts`)
injects this header server-side, so browsers never see the secret.

---

## 2. Prerequisites

On the VPS:

- Ubuntu 22.04+ or Debian 12+ (any modern Linux with systemd + docker
  works)
- Docker Engine 25+ with the compose plugin (`docker compose ...`)
- A public IPv4 (an IPv6 too is nice but not required)
- 4 vCPU / 8 GB RAM / 80 GB SSD is the floor for the bundled MinIO
- Open ports: 80, 443 inbound (everything else stays internal)

DNS — point both subdomains at the VPS:

```
plowered.s2datasystems.in      A    <vps-ip>
ploweredapi.s2datasystems.in   A    <vps-ip>
```

Wait for both to resolve before running step 5 (certbot uses DNS to
prove ownership):

```sh
dig +short plowered.s2datasystems.in
dig +short ploweredapi.s2datasystems.in
```

---

## 3. First-time deploy

```sh
# 1. Clone + check out the release tag you want to deploy
git clone https://github.com/Satyaamm/plowered.git /opt/plowered
cd /opt/plowered

# 2. Generate the secrets file. The script mints fresh cryptographic
#    values for the master key, JWT secret, gateway secret, plus DB +
#    Redis + MinIO passwords.
./deploy/secrets-gen.sh

# 3. Edit .env.production — fill in:
#    - PLOWERED_RESEND_API_KEY (optional but needed for verification
#      emails to leave the building)
#    - PLOWERED_EMAIL_FROM (only when Resend is wired)
$EDITOR .env.production

# 4. Bring up just the data containers so the volumes are created
docker compose --env-file .env.production -f docker-compose.production.yml up -d postgres redis nats minio

# 5. Issue Let's Encrypt certificates for BOTH subdomains. This
#    bootstraps a temporary HTTP-only nginx, runs certbot, then
#    tears it down. ~30 seconds total.
./deploy/init-certs.sh you@s2datasystems.in

# 6. Bring the full stack up.
docker compose --env-file .env.production -f docker-compose.production.yml up -d

# 7. Tail the logs while the first set of migrations apply.
docker compose -f docker-compose.production.yml logs -f plowered-api
#    Look for: "applied migrations through 0025"
```

Browser checks:

- https://plowered.s2datasystems.in       → signup page
- https://ploweredapi.s2datasystems.in/healthz → 200 ok
- https://ploweredapi.s2datasystems.in/docs    → Swagger UI
- https://ploweredapi.s2datasystems.in/v1/auth/me → 401 (gateway gate
  works — direct hits without the header are rejected)

CLI check (you'll need the gateway secret from `.env.production`):

```sh
GW=$(grep ^PLOWERED_GATEWAY_SECRET .env.production | cut -d= -f2-)
curl -i -H "X-Gateway-Auth: $GW" https://ploweredapi.s2datasystems.in/v1/auth/me
# → 401 with auth_required (auth gate works, gateway gate passed)
```

---

## 4. First signup

1. https://plowered.s2datasystems.in/signup
2. Workspace name, email, password (≥ 8 chars, mixed-case, digit,
   symbol).
3. If `PLOWERED_RESEND_API_KEY` is unset or the recipient is not your
   Resend verified address, the verification email lands in the
   API container logs. Pull the token:
   ```sh
   docker exec -it plowered-postgres-1 psql -U plowered -d plowered -c \
     "SELECT token FROM email_verifications ORDER BY created_at DESC LIMIT 1;"
   ```
   Then open `https://plowered.s2datasystems.in/verify?token=<token>`.
4. Sign in.
5. The first user gets `super_admin` on the new tenant. Promote them
   to `platform_admin` too if they should triage cross-tenant
   feedback:
   ```sh
   docker exec -it plowered-postgres-1 psql -U plowered -d plowered -c \
     "UPDATE tenant_memberships SET roles = array_append(roles, 'platform_admin') \
        WHERE user_id = (SELECT id FROM users WHERE email='you@s2datasystems.in');"
   ```

---

## 5. Day-to-day operations

### Updating

```sh
git pull
docker compose --env-file .env.production -f docker-compose.production.yml build
docker compose --env-file .env.production -f docker-compose.production.yml up -d
```

Migrations run on API boot — keep an eye on the logs for the first
30 seconds.

### Rotating the gateway secret

```sh
./deploy/secrets-gen.sh --force
docker compose --env-file .env.production -f docker-compose.production.yml up -d
```

Every active session gets invalidated (JWT secret rotates with it).
Browser users see a re-login prompt; CLI consumers need the new
secret.

### Backups

The compose stack does NOT take backups. You must:

1. Add a cron on the host:
   ```sh
   0 3 * * * docker exec plowered-postgres-1 \
     pg_dump -U plowered plowered | gzip > /backup/plowered-$(date +\%F).sql.gz
   ```
2. Ship `/backup/` to S3 with object-lock for tamper-evidence.
3. Run a quarterly restore drill — there is no backup until you've
   restored it once.

### Reading logs

```sh
docker compose -f docker-compose.production.yml logs -f --tail=100 plowered-api
docker compose -f docker-compose.production.yml logs -f --tail=100 nginx
```

JSON-format access log on nginx — `jq` away.

### Cert renewal

Automatic via the `certbot` container in the stack — it loops every
12h and only re-issues when ≤ 30 days from expiry. To force a renewal:

```sh
docker compose -f docker-compose.production.yml exec certbot \
  certbot renew --force-renewal
docker compose -f docker-compose.production.yml exec nginx nginx -s reload
```

---

## 6. Security posture summary

What this deploy already does:

- TLS 1.2/1.3 only at edge, with HSTS + Mozilla intermediate ciphers.
- HSTS + CSP + X-Frame-Options + COOP + CORP set at both nginx + the
  app (defense in depth).
- Per-IP rate limit at nginx (5/s on /v1/auth, 30/s on the rest) +
  per-principal rate limit in the app.
- AES-256-GCM secrets vault for connector + AI-provider credentials.
- Argon2id password hashing.
- Session cookies HttpOnly + Secure + SameSite=Lax + parent-domain
  scope; the JWT secret signs them.
- Account lockout after 5 failed logins in 15 min.
- Gateway-auth shared secret on every API call (defense in depth
  above the session/bearer auth).
- Bad-bot UA blocklist (nmap, sqlmap, nikto, masscan, zgrab, censys,
  empty UA).
- Metrics endpoint allow-listed to the private docker bridge.
- Distroless non-root containers; read-only filesystem.
- `internal: true` private network so app containers can't reach the
  host's cloud-metadata endpoint.

What's still on you to do:

- Backups (see above).
- Off-VPS monitoring + uptime probe (a probe from your laptop is
  not monitoring).
- Cloudflare or another DDoS layer if you're hosting publicly —
  nginx's rate limit handles small floods but a real attack will
  fill the link.
- Per-tenant cost budgets via /cost so a runaway agent can't bill
  your AI provider account.
- A real disaster-recovery rehearsal: restore the most recent
  backup into a sibling VPS, confirm signup + login + crawl work,
  destroy it.

---

## 7. Tearing down (or migrating to a different host)

```sh
# Stop containers but keep volumes (data preserved)
docker compose -f docker-compose.production.yml stop

# Stop + remove containers AND volumes (destructive)
docker compose -f docker-compose.production.yml down -v

# Just the data volumes — useful when moving to a different VPS:
#   docker volume save plowered_pgdata plowered_miniodata plowered_certbotetc plowered_certbotwww \
#     | ssh new-vps "docker volume load"
# (requires docker buildx + scp; see ROADMAP § migration tooling.)
```
