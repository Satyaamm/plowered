#!/usr/bin/env bash
# init-certs.sh — bootstrap Let's Encrypt certificates for both
# subdomains. Run once after first deploy, before nginx can serve
# HTTPS. Idempotent — subsequent runs are no-ops if certs exist.
#
# Usage:
#   ./deploy/init-certs.sh you@s2datasystems.in
#
# The compose stack's certbot service handles renewals from then on.
#
# Why this exists: nginx fails to start without the cert files at the
# paths in its vhosts. Chicken-and-egg: we need nginx for the ACME
# challenge, but nginx won't start without certs. The fix: bring up a
# minimal "challenge-only" nginx first, run certbot to get the certs,
# then restart nginx for real.

set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: $0 <admin-email>"
  exit 1
fi
EMAIL="$1"

WEB_DOMAIN="${WEB_DOMAIN:-plowered.s2datasystems.in}"
API_DOMAIN="${API_DOMAIN:-ploweredapi.s2datasystems.in}"

CERTBOT_ETC="$(docker volume inspect plowered_certbotetc -f '{{ .Mountpoint }}' 2>/dev/null || true)"

# 1. If certs already exist for both domains, skip.
if [[ -n "$CERTBOT_ETC" \
    && -d "$CERTBOT_ETC/live/$WEB_DOMAIN" \
    && -d "$CERTBOT_ETC/live/$API_DOMAIN" ]]; then
  echo "[init-certs] certs already exist for $WEB_DOMAIN + $API_DOMAIN; nothing to do."
  exit 0
fi

# 2. Drop a temporary HTTP-only nginx config so the ACME challenge
#    succeeds without requiring TLS. We do this by writing a stub
#    vhost that nginx loads INSTEAD of the real ones during the
#    bootstrap.
echo "[init-certs] writing bootstrap nginx config..."
mkdir -p deploy/nginx/conf.d.bootstrap
cat > deploy/nginx/conf.d.bootstrap/bootstrap.conf <<EOF
server {
    listen 80 default_server;
    server_name $WEB_DOMAIN $API_DOMAIN;
    location /.well-known/acme-challenge/ { root /var/www/certbot; }
    location / { return 200 "bootstrap\n"; add_header Content-Type text/plain; }
}
EOF

# 3. Bring up a minimal nginx container that mounts only the
#    bootstrap config + the certbot webroot. Detached so the
#    certbot invocation below can reach it.
echo "[init-certs] starting bootstrap nginx..."
docker run -d --rm --name plowered-cert-bootstrap \
  -p 80:80 \
  -v plowered_certbotwww:/var/www/certbot \
  -v "$(pwd)/deploy/nginx/conf.d.bootstrap":/etc/nginx/conf.d:ro \
  nginx:1.27-alpine

# Give nginx a beat to bind the port.
sleep 2

# 4. Run certbot for each domain. --agree-tos is required;
#    --no-eff-email opts out of EFF newsletter. Force renewal off so
#    we don't burn quota on a re-run.
issue() {
  local domain="$1"
  echo "[init-certs] requesting cert for $domain..."
  docker run --rm \
    -v plowered_certbotetc:/etc/letsencrypt \
    -v plowered_certbotwww:/var/www/certbot \
    certbot/certbot:latest \
    certonly --webroot --webroot-path=/var/www/certbot \
    --email "$EMAIL" --agree-tos --no-eff-email \
    -d "$domain" \
    --non-interactive \
    --keep-until-expiring
}
issue "$WEB_DOMAIN"
issue "$API_DOMAIN"

# 5. Tear down the bootstrap nginx + scrap the temp config.
echo "[init-certs] stopping bootstrap nginx..."
docker stop plowered-cert-bootstrap >/dev/null
rm -rf deploy/nginx/conf.d.bootstrap

echo "[init-certs] done. Bring the full stack up:"
echo "  docker compose --env-file .env.production -f docker-compose.production.yml up -d"
