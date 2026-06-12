# Cloud-native support plan

Goal: a customer can run Plowered on **their** cloud (AWS, Azure, or GCP)
using that cloud's managed services end to end — or buy it from us hosted.
One codebase, no forks; every cloud-specific piece sits behind an
interface that already exists in `internal/core/`.

This document is the plan of record. Each phase lists what ships, the
interface it plugs into, and the env contract. Status is tracked inline.

---

## Where each cloud plugs in

| Capability | Interface (already in tree) | AWS | Azure | GCP |
|---|---|---|---|---|
| LLM chat + embeddings | `aiprovider` adapters | Bedrock — **shipped** | Azure OpenAI — **shipped** | Vertex AI — **shipped** |
| Warehouse / query | `warehouse.MultiFactory` | Athena, Redshift — **shipped** | (Snowflake-on-Azure) — **shipped** | BigQuery — **shipped** |
| NoSQL crawl | source-adapter registry | DynamoDB — **shipped** | Cosmos DB — planned (P3) | Firestore — planned (P3) |
| Object storage | `blob.ObjectStore` | S3 — **shipped** | Blob Storage — **P1** | GCS — **P1** |
| Transactional email | `email.Sender` | SES — **P1** | ACS Email — P2 | (SendGrid) — P2 |
| Metadata DB | plain `postgres://` URL | RDS — works | Azure DB for PG — works | Cloud SQL — works |
| Job queue | plain `redis://` URL | ElastiCache — works | Azure Cache — works | Memorystore — works |
| Secrets master key | `secrets.Vault` (AES-GCM) | KMS wrap — P2 | Key Vault wrap — P2 | Cloud KMS wrap — P2 |
| Customer SSO | OIDC slot in auth middleware | Cognito — P2 | Entra ID — P2 | Google Identity — P2 |
| TLS / edge | nginx (bundled) or the cloud LB | ALB + ACM — works | App Gateway / Front Door — works | Cloud LB + managed certs — works |
| IaC | — | Terraform module — P3 | Terraform/Bicep — P3 | Terraform — P3 |
| Kubernetes | Dockerfiles exist | EKS via Helm — P3 | AKS via Helm — P3 | GKE via Helm — P3 |
| Marketplace | — | P4 | P4 | P4 |

"Works" = standard protocol, point the env var at the managed endpoint,
nothing to build. "Shipped" = adapter merged. P1-P4 = phases below.

---

## Phase 1 — storage + email parity (THIS SESSION)

The blocker for "deploy on Azure/GCP today" is that `blob.ObjectStore`
only has an S3 backend and `email.Sender` only has Resend. Everything
else either already works (postgres/redis URLs) or already shipped
(LLM adapters).

Ships:

1. **`internal/core/blob/azure.go`** — Azure Blob Storage backend.
   SDK: `github.com/Azure/azure-sdk-for-go/sdk/storage/azblob`.
   Auth: connection string OR `DefaultAzureCredential` (managed
   identity, workload identity, env creds — same resolution story as
   the AWS chain). SignedURL via user-delegation SAS.

2. **`internal/core/blob/gcs.go`** — Google Cloud Storage backend.
   SDK: `cloud.google.com/go/storage` (already an indirect dep via
   BigQuery). Auth: ADC (service-account JSON, workload identity,
   metadata server). SignedURL via V4 signing.

3. **`internal/core/email/ses.go`** — Amazon SES v2 sender.
   SDK: `github.com/aws/aws-sdk-go-v2/service/sesv2`. Mirrors
   ResendSender; same `Sender` interface, same soft-fail contract.

4. **Kind-driven boot wiring** in `cmd/plowered/main.go`:

   ```bash
   # Object store — pick one backend.
   PLOWERED_OBJECT_STORE_KIND=s3|azure-blob|gcs|memory   # default: s3 if bucket set, else memory

   # s3 (and MinIO/R2 via endpoint):
   PLOWERED_S3_BUCKET=…  PLOWERED_S3_REGION=…  PLOWERED_S3_ENDPOINT=…  PLOWERED_S3_PATH_STYLE=1

   # azure-blob:
   PLOWERED_AZURE_CONTAINER=…
   PLOWERED_AZURE_ACCOUNT=…              # storage account name (DefaultAzureCredential auth)
   PLOWERED_AZURE_CONNECTION_STRING=…    # OR full connection string (key auth)

   # gcs:
   PLOWERED_GCS_BUCKET=…                 # auth via Application Default Credentials

   # Email — pick one provider.
   PLOWERED_EMAIL_PROVIDER=resend|ses|log   # default: resend if key set, else log
   PLOWERED_RESEND_API_KEY=…                # resend
   PLOWERED_SES_REGION=…                    # ses (creds via the AWS chain)
   ```

5. **`GET /v1/cloud/status`** (admin-gated) — reports the effective
   bindings (kinds + non-secret identifiers) so the UI and support
   can see what's wired without shelling into the box.

6. **`/settings/cloud`** page — per-cloud groups with real brand
   service icons, showing connected/available state per integration.

## Phase 2 — enterprise auth + key custody

- OIDC middleware adapter (one implementation serves Cognito, Entra
  ID, Google Identity, Okta, Auth0). The slot in
  `internal/api/middleware/auth.go` is reserved.
- Master-key wrap via cloud KMS: `PLOWERED_MASTER_KEY_PROVIDER=env|aws-kms|azure-keyvault|gcp-kms`.
  The AES vault keeps sealing; only key custody moves.
- ACS Email + SendGrid senders for Azure/GCP-native email.

## Phase 3 — IaC + Kubernetes

- `deploy/terraform/aws/` — VPC + RDS + ElastiCache + S3 + ECS
  Fargate + ALB + ACM. The production compose file is the spec.
- `deploy/terraform/azurerm/` — Container Apps + Azure DB + Azure
  Cache + Blob + Front Door.
- `deploy/terraform/gcp/` — Cloud Run + Cloud SQL + Memorystore +
  GCS + HTTPS LB.
- `deploy/helm/plowered/` — one chart for EKS / AKS / GKE.
- Cosmos DB + Firestore source adapters (crawl parity).

## Phase 4 — marketplaces

AWS Marketplace, Azure Marketplace, GCP Marketplace listings with
metered billing. Only worth starting once BYOC customers exist.

---

## Design rules (apply to every phase)

1. **No cloud SDK calls outside `internal/core/<pkg>` or
   `internal/adapters/`.** Handlers and services see interfaces only.
2. **Credential resolution follows each cloud's default chain** (env →
   file → workload identity → instance metadata). Never invent a
   bespoke credential format.
3. **Boot never crashes on a misconfigured backend** — log loudly and
   fall back to the in-memory implementation, same as S3 does today.
4. **No secrets in `GET /v1/cloud/status`** — kinds, bucket/container
   names, regions; never keys or connection strings.
5. **Every adapter gets the same three tests**: constructor validation,
   round-trip against the in-memory fake (where the SDK allows), and
   not-found semantics (`blob.ErrNotFound` wrapping).
