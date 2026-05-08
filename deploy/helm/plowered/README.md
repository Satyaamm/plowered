# Plowered Helm Chart

Deploy Plowered to a Kubernetes cluster.

## Quick install

```bash
helm install plowered ./deploy/helm/plowered \
  --namespace plowered --create-namespace \
  --set secrets.values.databaseUrl="postgres://..." \
  --set secrets.values.jwtHs256Secret="$(openssl rand -hex 32)"
```

## Values worth knowing

| Key | Purpose |
|---|---|
| `image.tag` | Defaults to `Chart.AppVersion`. Override per-deploy. |
| `replicaCount` / `autoscaling.*` | Initial replicas; HPA scales 2→10 on CPU by default. |
| `service.httpPort` / `service.grpcPort` | Both ports exposed via the same Service. |
| `ingress.enabled` | Off by default; on prod, set TLS + host. |
| `secrets.existingSecret` | Reference an out-of-band Secret (sealed-secrets, ESO, etc.) instead of inlining values. |
| `config.env` | `production` / `staging` / `dev` — drives auth strictness in code. |
| `securityContext` / `podSecurityContext` | Distroless, non-root, read-only rootfs by default. |

## Production checklist

- Provide TLS termination at the ingress, not in-pod. The chart ships
  `insecure` gRPC creds in v0; mTLS is a follow-up.
- Use `secrets.existingSecret` driven by your secrets operator.
  Never commit values into a values file in git.
- Run an external PostgreSQL with point-in-time recovery and TLS.
  This chart does not bundle a database.
- Set `config.cors.allowedOrigins` to the explicit web origin; never `*`.
- Set `PLOWERED_JWT_RS256_PUB_KEY` (RS256). HS256 is preview-only.
- Pin the image tag and update via GitOps; do not chase `:latest`.
