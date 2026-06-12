#!/usr/bin/env bash
# secrets-gen.sh — produces a .env.production by filling every
# "GENERATE_*" placeholder in .env.production.example with a fresh
# cryptographically-random value.
#
# Idempotent: if .env.production already exists, we refuse to
# overwrite. Run with --force to regenerate (you'll need to bounce
# every container so they pick up the new secrets, AND every active
# user session will be invalidated by the JWT-secret rotation).
#
# Usage:
#   ./deploy/secrets-gen.sh           # first run
#   ./deploy/secrets-gen.sh --force   # rotate all secrets

set -euo pipefail

SRC=".env.production.example"
DST=".env.production"

if [[ ! -f "$SRC" ]]; then
  echo "secrets-gen: $SRC not found — run from the repo root."
  exit 1
fi

if [[ -f "$DST" && "${1:-}" != "--force" ]]; then
  echo "secrets-gen: $DST exists; refusing to overwrite (use --force to rotate)."
  exit 1
fi

# Generate 32-byte base64 secrets (no padding, no slashes — those
# survive every shell quoting context we'd ever see in compose).
gen() { openssl rand -base64 32 | tr -d '\n=' | tr '/+' '_-'; }

POSTGRES_PW="$(gen)"
REDIS_PW="$(gen)"
MINIO_PW="$(gen)"
MASTER_KEY="$(openssl rand -base64 32)"   # exact 32-byte b64 (with padding)
JWT_SECRET="$(openssl rand -base64 32)"
GATEWAY_SECRET="$(openssl rand -base64 32)"

cp "$SRC" "$DST"
# Use a shell-portable in-place substitution (BSD sed on macOS needs
# the empty -i arg, GNU sed accepts -i).
if sed --version >/dev/null 2>&1; then
  SED_INPLACE=(sed -i)
else
  SED_INPLACE=(sed -i '')
fi

# Escape \ + & + delimiter in the replacements so sed doesn't treat
# them as backreferences.
esc() { printf '%s' "$1" | sed -e 's/[\\&|]/\\&/g'; }

"${SED_INPLACE[@]}" "s|GENERATE_POSTGRES_PASSWORD|$(esc "$POSTGRES_PW")|" "$DST"
"${SED_INPLACE[@]}" "s|GENERATE_REDIS_PASSWORD|$(esc "$REDIS_PW")|"       "$DST"
"${SED_INPLACE[@]}" "s|GENERATE_MINIO_PASSWORD|$(esc "$MINIO_PW")|"       "$DST"
"${SED_INPLACE[@]}" "s|GENERATE_MASTER_KEY|$(esc "$MASTER_KEY")|"         "$DST"
"${SED_INPLACE[@]}" "s|GENERATE_JWT_SECRET|$(esc "$JWT_SECRET")|"         "$DST"
"${SED_INPLACE[@]}" "s|GENERATE_GATEWAY_SECRET|$(esc "$GATEWAY_SECRET")|" "$DST"

chmod 600 "$DST"

echo "secrets-gen: wrote $DST (chmod 600)."
echo "  Postgres password   : $POSTGRES_PW"
echo "  Redis password      : $REDIS_PW"
echo "  MinIO password      : $MINIO_PW"
echo "  Master key          : $MASTER_KEY"
echo "  JWT secret          : $JWT_SECRET"
echo "  Gateway secret      : $GATEWAY_SECRET"
echo
echo "Stash the gateway secret in your CLI's keychain — you need it to"
echo "call ploweredapi.s2datasystems.in directly:"
echo "  curl -H 'X-Gateway-Auth: $GATEWAY_SECRET' https://ploweredapi.s2datasystems.in/healthz"
