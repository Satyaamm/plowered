"use client";

import { ReactNode } from "react";
import {
  Badge,
  Body1,
  Caption1,
  Subtitle2,
  Text,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { PageHeader } from "@/components/page-header";
import { PageIntro } from "@/components/page-intro";
import { ErrorBanner, LoadingState } from "@/components/states";
import { CloudBinding, useCloudStatus } from "@/lib/hooks/use-cloud";
import {
  ACSEmailIcon,
  AthenaIcon,
  AWSLogo,
  AzureBlobIcon,
  AzureCacheIcon,
  AzureLogo,
  AzureOpenAIIcon,
  AzurePostgresIcon,
  BedrockIcon,
  BigQueryIcon,
  CloudSQLIcon,
  CosmosDBIcon,
  DynamoDBIcon,
  ElastiCacheIcon,
  FirestoreIcon,
  GCPLogo,
  GCSIcon,
  LogIcon,
  MemoryIcon,
  MemorystoreIcon,
  MinIOIcon,
  NATSIcon,
  PostgresIcon,
  RDSIcon,
  RedisIcon,
  RedshiftIcon,
  ResendIcon,
  S3Icon,
  SESIcon,
  SnowflakeIcon,
  VertexIcon,
} from "@/components/cloud-icons";

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "20px" },
  bindingGrid: {
    display: "grid",
    gridTemplateColumns: "repeat(auto-fill, minmax(230px, 1fr))",
    gap: "12px",
  },
  bindingCard: {
    backgroundColor: tokens.colorNeutralBackground1,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "8px",
    padding: "14px 16px",
    display: "flex",
    alignItems: "center",
    gap: "12px",
  },
  bindingMeta: { display: "flex", flexDirection: "column", gap: "2px", minWidth: "0" },
  bindingDetail: {
    color: tokens.colorNeutralForeground3,
    fontSize: "12px",
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  },
  cloudSection: {
    backgroundColor: tokens.colorNeutralBackground1,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "8px",
    padding: "16px 20px",
    display: "flex",
    flexDirection: "column",
    gap: "12px",
  },
  cloudHeader: { display: "flex", alignItems: "center", gap: "10px" },
  serviceGrid: {
    display: "grid",
    gridTemplateColumns: "repeat(auto-fill, minmax(280px, 1fr))",
    gap: "8px",
  },
  serviceRow: {
    display: "flex",
    alignItems: "center",
    gap: "10px",
    padding: "8px 10px",
    borderRadius: "6px",
    backgroundColor: tokens.colorNeutralBackground2,
  },
  serviceMeta: { display: "flex", flexDirection: "column", minWidth: "0", flexGrow: "1" },
  serviceCategory: { color: tokens.colorNeutralForeground3, fontSize: "11px" },
});

// ---- live-binding kind → presentation ----------------------------------

const KIND_PRESENTATION: Record<string, { label: string; icon: ReactNode }> = {
  s3:           { label: "Amazon S3",            icon: <S3Icon /> },
  "azure-blob": { label: "Azure Blob Storage",   icon: <AzureBlobIcon /> },
  gcs:          { label: "Google Cloud Storage", icon: <GCSIcon /> },
  memory:       { label: "In-memory (volatile)", icon: <MemoryIcon /> },
  resend:       { label: "Resend",               icon: <ResendIcon /> },
  ses:          { label: "Amazon SES",           icon: <SESIcon /> },
  log:          { label: "Log only",             icon: <LogIcon /> },
  postgres:     { label: "PostgreSQL",           icon: <PostgresIcon /> },
  redis:        { label: "Redis (Asynq)",        icon: <RedisIcon /> },
  sync:         { label: "In-process sync",      icon: <MemoryIcon /> },
  nats:         { label: "NATS JetStream",       icon: <NATSIcon /> },
};

const SEAM_LABELS: Array<{ key: keyof StatusShape; label: string }> = [
  { key: "object_store", label: "Object storage" },
  { key: "email",        label: "Transactional email" },
  { key: "database",     label: "Metadata database" },
  { key: "queue",        label: "Job queue" },
  { key: "events",       label: "Event relay" },
];

interface StatusShape {
  object_store: CloudBinding;
  email: CloudBinding;
  database: CloudBinding;
  queue: CloudBinding;
  events: CloudBinding;
}

// ---- per-cloud service matrix (static capability data) -------------------

type ServiceStatus = "shipped" | "works" | "planned";

interface CloudService {
  name: string;
  category: string;
  status: ServiceStatus;
  icon: ReactNode;
  // when set, the live binding with this kind marks the row Connected
  bindingKind?: string;
}

const AWS_SERVICES: CloudService[] = [
  { name: "Amazon S3",          category: "Object storage",  status: "shipped", icon: <S3Icon />,          bindingKind: "s3" },
  { name: "Amazon SES",         category: "Email",           status: "shipped", icon: <SESIcon />,         bindingKind: "ses" },
  { name: "Amazon Bedrock",     category: "AI / LLM",        status: "shipped", icon: <BedrockIcon /> },
  { name: "Amazon Athena",      category: "Query engine",    status: "shipped", icon: <AthenaIcon /> },
  { name: "Amazon Redshift",    category: "Warehouse",       status: "shipped", icon: <RedshiftIcon /> },
  { name: "Amazon DynamoDB",    category: "NoSQL source",    status: "shipped", icon: <DynamoDBIcon /> },
  { name: "Amazon RDS",         category: "Metadata DB",     status: "works",   icon: <RDSIcon /> },
  { name: "Amazon ElastiCache", category: "Job queue",       status: "works",   icon: <ElastiCacheIcon /> },
];

const AZURE_SERVICES: CloudService[] = [
  { name: "Azure Blob Storage",        category: "Object storage", status: "shipped", icon: <AzureBlobIcon />, bindingKind: "azure-blob" },
  { name: "Azure OpenAI",              category: "AI / LLM",       status: "shipped", icon: <AzureOpenAIIcon /> },
  { name: "Azure DB for PostgreSQL",   category: "Metadata DB",    status: "works",   icon: <AzurePostgresIcon /> },
  { name: "Azure Cache for Redis",     category: "Job queue",      status: "works",   icon: <AzureCacheIcon /> },
  { name: "Communication Services",    category: "Email",          status: "planned", icon: <ACSEmailIcon /> },
  { name: "Azure Cosmos DB",           category: "NoSQL source",   status: "planned", icon: <CosmosDBIcon /> },
];

const GCP_SERVICES: CloudService[] = [
  { name: "Google Cloud Storage", category: "Object storage", status: "shipped", icon: <GCSIcon />, bindingKind: "gcs" },
  { name: "Vertex AI",            category: "AI / LLM",       status: "shipped", icon: <VertexIcon /> },
  { name: "BigQuery",             category: "Warehouse",      status: "shipped", icon: <BigQueryIcon /> },
  { name: "Cloud SQL",            category: "Metadata DB",    status: "works",   icon: <CloudSQLIcon /> },
  { name: "Memorystore",          category: "Job queue",      status: "works",   icon: <MemorystoreIcon /> },
  { name: "Firestore",            category: "NoSQL source",   status: "planned", icon: <FirestoreIcon /> },
];

const OTHER_SERVICES: CloudService[] = [
  { name: "PostgreSQL (self-hosted)", category: "Metadata DB",    status: "shipped", icon: <PostgresIcon />, bindingKind: "postgres" },
  { name: "Redis (self-hosted)",      category: "Job queue",      status: "shipped", icon: <RedisIcon />,    bindingKind: "redis" },
  { name: "NATS JetStream",           category: "Event relay",    status: "shipped", icon: <NATSIcon />,     bindingKind: "nats" },
  { name: "MinIO (S3-compatible)",    category: "Object storage", status: "shipped", icon: <MinIOIcon /> },
  { name: "Resend",                   category: "Email",          status: "shipped", icon: <ResendIcon />,   bindingKind: "resend" },
  { name: "Snowflake",                category: "Warehouse",      status: "shipped", icon: <SnowflakeIcon /> },
];

function StatusBadge({ status, connected }: { status: ServiceStatus; connected: boolean }) {
  if (connected) {
    return <Badge appearance="filled" color="success">Connected</Badge>;
  }
  switch (status) {
    case "shipped":
      return <Badge appearance="tint" color="brand">Available</Badge>;
    case "works":
      return <Badge appearance="tint" color="informative">Compatible</Badge>;
    case "planned":
      return <Badge appearance="outline" color="subtle">Planned</Badge>;
  }
}

export default function CloudSettingsPage() {
  const styles = useStyles();
  const { data: status, isLoading, error } = useCloudStatus();

  const connectedKinds = new Set<string>();
  if (status) {
    for (const { key } of SEAM_LABELS) {
      const b = status[key];
      if (b?.kind) connectedKinds.add(b.kind);
    }
  }

  return (
    <div className={styles.root}>
      <PageHeader
        crumbs={[{ label: "Management" }, { label: "Cloud" }]}
        actions={
          <PageIntro
            title="What is this page?"
            body="Shows which infrastructure backend each platform seam resolved to at boot — object storage, email, database, queue, events — plus the matrix of supported cloud services per provider. Bindings are configured through environment variables on the API process; this page is read-only."
            bullets={[
              "Connected — a live binding is using this service right now.",
              "Available — the adapter ships in this build; set the env vars and restart to use it.",
              "Compatible — works through a standard protocol (postgres:// or redis:// URL); nothing to install.",
              "Planned — on the roadmap (see infra/CLOUD_NATIVE.md).",
            ]}
            cta="To switch a binding, change PLOWERED_OBJECT_STORE_KIND / PLOWERED_EMAIL_PROVIDER (plus per-kind credentials) and restart the API."
          />
        }
      />

      {error && <ErrorBanner error={error as Error} />}
      {isLoading && <LoadingState label="Loading cloud status…" />}

      {status && (
        <>
          <Subtitle2>Live bindings</Subtitle2>
          <div className={styles.bindingGrid}>
            {SEAM_LABELS.map(({ key, label }) => {
              const binding = status[key];
              const pres = KIND_PRESENTATION[binding?.kind ?? ""] ?? {
                label: binding?.kind ?? "unknown",
                icon: <MemoryIcon />,
              };
              return (
                <div key={key} className={styles.bindingCard}>
                  {pres.icon}
                  <div className={styles.bindingMeta}>
                    <Caption1>{label}</Caption1>
                    <Text weight="semibold">{pres.label}</Text>
                    {binding?.detail && (
                      <span className={styles.bindingDetail}>{binding.detail}</span>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        </>
      )}

      <Subtitle2>Supported services</Subtitle2>

      {[
        { logo: <AWSLogo size={26} />, name: "Amazon Web Services", services: AWS_SERVICES },
        { logo: <AzureLogo size={26} />, name: "Microsoft Azure", services: AZURE_SERVICES },
        { logo: <GCPLogo size={26} />, name: "Google Cloud", services: GCP_SERVICES },
        { logo: null, name: "Self-hosted & SaaS", services: OTHER_SERVICES },
      ].map((cloud) => (
        <div key={cloud.name} className={styles.cloudSection}>
          <div className={styles.cloudHeader}>
            {cloud.logo}
            <Subtitle2>{cloud.name}</Subtitle2>
          </div>
          <div className={styles.serviceGrid}>
            {cloud.services.map((svc) => (
              <div key={svc.name} className={styles.serviceRow}>
                {svc.icon}
                <div className={styles.serviceMeta}>
                  <Body1>{svc.name}</Body1>
                  <span className={styles.serviceCategory}>{svc.category}</span>
                </div>
                <StatusBadge
                  status={svc.status}
                  connected={!!svc.bindingKind && connectedKinds.has(svc.bindingKind)}
                />
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}
