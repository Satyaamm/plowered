#!/usr/bin/env bash
# =============================================================================
# Plowered — one-command PROD deploy to the VPS.
#
# Usage:
#     bash deploy.sh
#
# The VPS is a SHARED box: host nginx already owns ports 80/443 (other
# apps live there) and 5432/6379 are taken by other services. This
# script therefore deploys the HOST-EDGE shape:
#
#   internet → host nginx (TLS, rate limits, gateway check)
#                ├─→ 127.0.0.1:3005 → plowered-web   container
#                └─→ 127.0.0.1:8085 → plowered-api   container
#              plowered-worker / postgres(:6543) / redis(:7479) /
#              nats / minio — internal docker networks only
#
# The compose overlay deploy/compose.host-edge.yml parks the bundled
# nginx+certbot containers and publishes the two loopback ports; the
# bundled data planes run untouched (no host ports at all).
#
# What the script does:
#   1. Confirms PROD (type 'prod').
#   2. Opens ONE ssh master connection (password asked once).
#   3. Pushes ./.env.production → VPS (chmod 600). Local file is the
#      source of truth — secrets were minted here by secrets-gen.sh.
#   4. rsyncs the repo (env files + cruft excluded; checksum mode).
#   5. Remote: installs docker / host-nginx / certbot if missing,
#      issues Let's Encrypt certs for both domains (idempotent),
#      installs the plowered vhosts + rate-limit zones, reloads nginx.
#   6. Rebuilds + restarts ONLY the plowered compose project.
#   7. Health-checks both loopback ports + shows the containers.
#
# DB schema migrations run automatically inside the API at boot — no
# manual migrate step.
# =============================================================================
set -euo pipefail

VPS_IP="${PLOWERED_VPS_IP:-212.38.94.234}"
VPS_USER="${PLOWERED_VPS_USER:-root}"
VPS_DIR="/opt/plowered"
EMAIL="satyam.pathak@s2datasystems.in"
WEB_HOST_PORT=3005
API_HOST_PORT=8085

GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
info()  { echo -e "${GREEN}[INFO]${NC} $1"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# ── Local pre-flight ─────────────────────────────────────────────────────────
[[ -f docker-compose.production.yml ]]      || error "docker-compose.production.yml not found"
[[ -f deploy/compose.host-edge.yml ]]       || error "deploy/compose.host-edge.yml not found"
[[ -f deploy/nginx-host/plowered-web.conf ]] || error "deploy/nginx-host vhosts not found"
[[ -f .env.production ]] || error ".env.production not found — run ./deploy/secrets-gen.sh first."

envval() { grep -E "^$1=" .env.production | head -1 | cut -d= -f2-; }
WEB_DOMAIN="$(envval WEB_DOMAIN)";  [[ -n "$WEB_DOMAIN" ]]  || error "WEB_DOMAIN missing from .env.production"
API_DOMAIN="$(envval API_DOMAIN)";  [[ -n "$API_DOMAIN" ]]  || error "API_DOMAIN missing from .env.production"

echo ""
warn "You are about to deploy Plowered to PRODUCTION:"
warn "  web: https://${WEB_DOMAIN}   api: https://${API_DOMAIN}   vps: ${VPS_USER}@${VPS_IP}"
read -r -p "Type 'prod' to confirm, or press Ctrl+C to abort: " confirm
[[ "$confirm" == "prod" ]] || error "Aborted."

# ── SSH connection multiplexing — authenticate once ──────────────────────────
SSH_CTRL_DIR="$(mktemp -d -t plowered-ssh.XXXXXX)"
SSH_CTRL_SOCK="${SSH_CTRL_DIR}/sock"
SSH_OPTS=(-o "ControlMaster=auto" -o "ControlPath=${SSH_CTRL_SOCK}" -o "ControlPersist=10m")
export RSYNC_RSH="ssh -o ControlMaster=auto -o ControlPath=${SSH_CTRL_SOCK} -o ControlPersist=10m"

cleanup_ssh() {
    ssh "${SSH_OPTS[@]}" -O exit "${VPS_USER}@${VPS_IP}" 2>/dev/null || true
    rm -rf "$SSH_CTRL_DIR" 2>/dev/null || true
}
trap cleanup_ssh EXIT

info "Opening SSH master connection to ${VPS_USER}@${VPS_IP} (authenticate once)..."
ssh "${SSH_OPTS[@]}" -o ConnectTimeout=10 "${VPS_USER}@${VPS_IP}" true \
    || error "Could not ssh to ${VPS_USER}@${VPS_IP}."

echo ""
echo "══════════ Deploy started: $(date '+%Y-%m-%d %H:%M:%S') ══════════"

# ── Step 1: push the env file (local copy is the source of truth) ────────────
info "Pushing .env.production → ${VPS_DIR}/.env.production ..."
ssh "${SSH_OPTS[@]}" "${VPS_USER}@${VPS_IP}" "mkdir -p ${VPS_DIR}"
scp "${SSH_OPTS[@]}" .env.production "${VPS_USER}@${VPS_IP}:${VPS_DIR}/.env.production"
ssh "${SSH_OPTS[@]}" "${VPS_USER}@${VPS_IP}" "chmod 600 ${VPS_DIR}/.env.production"
info "✓ env synced"

# ── Step 2: rsync code (env files + cruft excluded; checksum mode) ───────────
info "Syncing code to VPS..."
rsync -avzc --delete \
    --include='.env.example' \
    --include='.env.production.example' \
    --exclude='.git' \
    --exclude='.github' \
    --exclude='.claude' \
    --exclude='.vscode' \
    --exclude='.idea' \
    --exclude='.DS_Store' \
    --exclude='*.swp' \
    --exclude='.env' \
    --exclude='.env.*' \
    --exclude='.secrets' \
    --exclude='bin' \
    --exclude='data' \
    --exclude='logs' \
    --exclude='*.log' \
    --exclude='coverage*' \
    --exclude='web/node_modules' \
    --exclude='web/.next' \
    --exclude='web/.turbo' \
    --exclude='web/demo-output' \
    --exclude='web/test-results' \
    --exclude='web/tsconfig.tsbuildinfo' \
    . "${VPS_USER}@${VPS_IP}:${VPS_DIR}"
info "✓ code synced"

# ── Step 3: remote setup + build + start ─────────────────────────────────────
info "Running remote setup..."
ssh "${SSH_OPTS[@]}" "${VPS_USER}@${VPS_IP}" \
    "WEB_DOMAIN='${WEB_DOMAIN}' \
     API_DOMAIN='${API_DOMAIN}' \
     EMAIL='${EMAIL}' \
     WEB_HOST_PORT='${WEB_HOST_PORT}' \
     API_HOST_PORT='${API_HOST_PORT}' \
     bash -s" <<'REMOTE_SCRIPT'
set -euo pipefail

VPS_DIR="/opt/plowered"
GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
info()  { echo -e "${GREEN}[INFO]${NC} $1"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

cd "$VPS_DIR"
[ -f .env.production ] || error ".env.production missing on VPS"

# ── Docker ──
if ! command -v docker &> /dev/null; then
    info "Installing Docker..."
    curl -fsSL https://get.docker.com | sh
    systemctl enable docker && systemctl start docker
fi
info "Docker: $(docker --version)"

# ── Host nginx + certbot ──
if ! command -v nginx &> /dev/null; then
    apt-get update -y && apt-get install -y nginx
    systemctl enable nginx
fi
if ! command -v certbot &> /dev/null; then
    apt-get install -y certbot
fi

# ── Rate-limit zones (http context — must be in conf.d) ──
cp "$VPS_DIR/deploy/nginx-host/plowered-zones.conf" /etc/nginx/conf.d/plowered-zones.conf

# ── Let's Encrypt per domain (idempotent, webroot bootstrap) ──
mkdir -p /var/www/certbot
setup_ssl() {
    local DOMAIN=$1
    if [ ! -d "/etc/letsencrypt/live/$DOMAIN" ]; then
        info "Issuing certificate for $DOMAIN..."
        # Temporary HTTP-only vhost so the ACME challenge has a home
        # before the real (ssl) vhost can load.
        echo "server {
    listen 80;
    listen [::]:80;
    server_name $DOMAIN;
    location /.well-known/acme-challenge/ { root /var/www/certbot; }
    location / { return 200 \"setting up ssl\"; add_header Content-Type text/plain; }
}" > "/etc/nginx/sites-available/$DOMAIN"
        ln -sf "/etc/nginx/sites-available/$DOMAIN" "/etc/nginx/sites-enabled/$DOMAIN"
        nginx -t && systemctl reload nginx
        certbot certonly --webroot -w /var/www/certbot -d "$DOMAIN" \
            --email "$EMAIL" --agree-tos --non-interactive --keep-until-expiring
        info "✓ certificate for $DOMAIN"
    else
        info "Certificate for $DOMAIN exists, skipping issue."
    fi
}
setup_ssl "$WEB_DOMAIN"
setup_ssl "$API_DOMAIN"

# ── Install the real vhosts (replace any bootstrap stubs) ──
info "Installing Plowered vhosts..."
cp "$VPS_DIR/deploy/nginx-host/plowered-web.conf" "/etc/nginx/sites-available/$WEB_DOMAIN"
cp "$VPS_DIR/deploy/nginx-host/plowered-api.conf" "/etc/nginx/sites-available/$API_DOMAIN"
ln -sf "/etc/nginx/sites-available/$WEB_DOMAIN" "/etc/nginx/sites-enabled/$WEB_DOMAIN"
ln -sf "/etc/nginx/sites-available/$API_DOMAIN" "/etc/nginx/sites-enabled/$API_DOMAIN"
nginx -t || error "nginx config test failed"
systemctl reload nginx
info "✓ host nginx configured + reloaded"

# ── Compose: scoped strictly to the plowered project ──
export COMPOSE_PROJECT_NAME=plowered
export WEB_HOST_PORT API_HOST_PORT
COMPOSE="docker compose --env-file .env.production -f docker-compose.production.yml -f deploy/compose.host-edge.yml"

info "Stopping previous plowered containers (project-scoped)..."
$COMPOSE down --remove-orphans 2>/dev/null || true

info "Building images (a few minutes on first run)..."
$COMPOSE build

info "Starting containers..."
$COMPOSE up -d

info "Waiting for the API to come up..."
ok_api=""
for i in $(seq 1 30); do
    if curl -sf -o /dev/null "http://127.0.0.1:${API_HOST_PORT}/healthz"; then ok_api=1; break; fi
    sleep 2
done
if [ -n "$ok_api" ]; then
    info "✓ API healthy on 127.0.0.1:${API_HOST_PORT}"
else
    warn "API health check failed — run: $COMPOSE logs plowered-api"
fi

ok_web=""
for i in $(seq 1 15); do
    if curl -sf -o /dev/null "http://127.0.0.1:${WEB_HOST_PORT}/"; then ok_web=1; break; fi
    sleep 2
done
if [ -n "$ok_web" ]; then
    info "✓ Web healthy on 127.0.0.1:${WEB_HOST_PORT}"
else
    warn "Web health check failed — run: $COMPOSE logs plowered-web"
fi

echo ""
info "Plowered containers:"
docker ps --filter "name=plowered" --format "  {{.Names}}  {{.Status}}  {{.Ports}}"

echo ""
info "═══════════════════════════════════════════"
info " Deploy complete!"
info " Web:      https://${WEB_DOMAIN}"
info " API:      https://${API_DOMAIN}"
info " API docs: https://${API_DOMAIN}/docs"
info "═══════════════════════════════════════════"
REMOTE_SCRIPT

echo ""
info "Done — production is live."
