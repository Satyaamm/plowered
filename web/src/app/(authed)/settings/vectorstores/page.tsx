"use client";

import { useState } from "react";
import {
  Badge,
  Body1,
  Button,
  Caption1,
  Card,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  DialogTrigger,
  Dropdown,
  Field,
  Input,
  MessageBar,
  MessageBarBody,
  Option,
  Spinner,
  Subtitle1,
  Subtitle2,
  Switch,
  Text,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  Add20Regular,
  CheckmarkCircle16Filled,
  Delete20Regular,
  Play20Regular,
  Sparkle24Regular,
} from "@fluentui/react-icons";
import { PageHeader } from "@/components/page-header";
import { PageIntro } from "@/components/page-intro";
import { InfoLabel } from "@/components/info-label";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";
import {
  VectorStore,
  VectorStoreInput,
  VectorStoreKind,
  useCreateVectorStore,
  useDeleteVectorStore,
  useRole,
  useSetPrimaryVectorStore,
  useTestInlineVectorStore,
  useTestStoredVectorStore,
  useVectorStores,
} from "@/lib/hooks";

const KIND_LABELS: Record<VectorStoreKind, string> = {
  pgvector: "Postgres (pgvector / asset_embeddings)",
  memory: "In-memory (dev only)",
  pinecone: "Pinecone",
  weaviate: "Weaviate",
  qdrant: "Qdrant",
};

type FieldKey = "endpoint" | "index_name" | "class_name" | "collection" | "dimension" | "api_key";

const KIND_FIELDS: Record<VectorStoreKind, FieldKey[]> = {
  pgvector: [],
  memory:   [],
  pinecone: ["endpoint", "index_name", "dimension", "api_key"],
  weaviate: ["endpoint", "class_name", "api_key"],
  qdrant:   ["endpoint", "collection", "api_key"],
};

const FIELD_LABELS: Record<FieldKey, string> = {
  endpoint:   "Endpoint URL",
  index_name: "Index name",
  class_name: "Class name",
  collection: "Collection name",
  dimension:  "Vector dimension",
  api_key:    "API key",
};

const FIELD_PLACEHOLDERS: Record<FieldKey, string> = {
  endpoint:   "https://my-index.svc.us-east-1-aws.pinecone.io",
  index_name: "plowered-prod",
  class_name: "Asset",
  collection: "plowered",
  dimension:  "1536",
  api_key:    "paste-your-key-here",
};

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "16px" },
  card: { padding: "12px 14px" },
  row: { display: "flex", alignItems: "center", gap: "10px", flexWrap: "wrap" },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
});

export default function VectorStoresPage() {
  const styles = useStyles();
  const { can } = useRole();
  const canAdmin = can("admin");
  const list = useVectorStores();
  const del = useDeleteVectorStore();
  const testStored = useTestStoredVectorStore();
  const setPrimary = useSetPrimaryVectorStore();
  const [open, setOpen] = useState(false);

  return (
    <div className={styles.root}>
      <PageHeader
        crumbs={[{ label: "Management" }, { label: "Vector stores" }]}
        actions={
          <>
            <PageIntro
              title="What's a vector store?"
              body="The destination for embedding vectors. Semantic search, asset describe, and Ask all generate vectors via your AI provider; this is where those vectors live and get queried by nearest-neighbour search. Different from LLM providers, which produce vectors but don't store them."
              bullets={[
                "pgvector (default) — uses the platform's Postgres + asset_embeddings table. Free, no extra infra; cosine scan is fine up to ~10k assets per tenant.",
                "Pinecone / Weaviate / Qdrant — hosted vector DBs for production workloads. Per-tenant namespace isolation; SecretURN is sealed in the vault.",
                "Memory (dev only) — in-process, lost on restart. Useful for testing without a Postgres.",
              ]}
              cta="Pick a backend, mark it primary, then run a search:reindex to fill it. Adding a new backend doesn't migrate existing vectors — you reindex into it."
            />
            {canAdmin && (
              <Dialog open={open} onOpenChange={(_, d) => setOpen(d.open)}>
                <DialogTrigger disableButtonEnhancement>
                  <Button appearance="primary" icon={<Add20Regular />}>
                    Add vector store
                  </Button>
                </DialogTrigger>
                <AddVectorStoreDialog onClose={() => setOpen(false)} />
              </Dialog>
            )}
          </>
        }
      />

      {list.isLoading && <LoadingState />}
      {list.error && <ErrorBanner error={list.error as Error} />}
      {list.data && list.data.length === 0 && (
        <EmptyState
          title="No vector stores configured"
          body="The platform falls back to the asset_embeddings Postgres table by default. Add a vector store to route semantic search through Pinecone / Weaviate / Qdrant for production scale."
        />
      )}
      {list.data?.map((v) => (
        <Card key={v.id} className={styles.card}>
          <div className={styles.row}>
            <Sparkle24Regular />
            <Subtitle2>{v.name}</Subtitle2>
            <Badge appearance="outline" color="brand">{KIND_LABELS[v.kind]}</Badge>
            {v.is_primary && (
              <Badge appearance="filled" color="success" icon={<CheckmarkCircle16Filled />}>
                primary
              </Badge>
            )}
            <span style={{ flex: 1 }} />
            {canAdmin && !v.is_primary && (
              <Button size="small" onClick={() => setPrimary.mutate(v.id)}>
                Make primary
              </Button>
            )}
            {canAdmin && (
              <Button
                size="small"
                appearance="subtle"
                icon={<Play20Regular />}
                onClick={() => testStored.mutate(v.id)}
                disabled={testStored.isPending && testStored.variables === v.id}
              >
                Test
              </Button>
            )}
            {canAdmin && (
              <Button
                size="small"
                appearance="subtle"
                icon={<Delete20Regular />}
                onClick={() => del.mutate(v.id)}
              >
                Delete
              </Button>
            )}
          </div>
          <Caption1 className={styles.meta}>
            {v.endpoint || v.collection || v.class_name || v.index_name || "—"}
            {v.last_tested_at && (
              <>
                {" · last tested "}
                {new Date(v.last_tested_at).toLocaleString()} —{" "}
                {v.last_test_ok ? "ok" : v.last_test_error || "failed"}
              </>
            )}
          </Caption1>
        </Card>
      ))}
    </div>
  );
}

function AddVectorStoreDialog({ onClose }: { onClose: () => void }) {
  const create = useCreateVectorStore();
  const testInline = useTestInlineVectorStore();
  const [kind, setKind] = useState<VectorStoreKind>("pgvector");
  const [name, setName] = useState("");
  const [endpoint, setEndpoint] = useState("");
  const [indexName, setIndexName] = useState("");
  const [className, setClassName] = useState("");
  const [collection, setCollection] = useState("");
  const [dimension, setDimension] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [isPrimary, setIsPrimary] = useState(true);
  const [tested, setTested] = useState<null | { ok: boolean; error?: string }>(null);

  const fields = KIND_FIELDS[kind];
  const has = (k: FieldKey) => fields.includes(k);

  const payload = (): VectorStoreInput => ({
    kind,
    name,
    endpoint: has("endpoint") && endpoint ? endpoint : undefined,
    index_name: has("index_name") && indexName ? indexName : undefined,
    class_name: has("class_name") && className ? className : undefined,
    collection: has("collection") && collection ? collection : undefined,
    dimension: has("dimension") && dimension ? Number(dimension) : undefined,
    api_key: has("api_key") && apiKey ? apiKey : undefined,
    is_primary: isPrimary,
  });

  const canSave =
    name.trim() !== "" &&
    (!has("endpoint") || endpoint.trim() !== "") &&
    (!has("index_name") || indexName.trim() !== "") &&
    (!has("class_name") || className.trim() !== "") &&
    (!has("collection") || collection.trim() !== "") &&
    (kind === "pgvector" || kind === "memory" || tested?.ok === true);

  const runTest = async () => {
    try {
      const res = await testInline.mutateAsync(payload());
      setTested(res);
    } catch (e: unknown) {
      const err = e as { payload?: { error?: string }; message?: string };
      setTested({ ok: false, error: err.payload?.error || err.message || "test failed" });
    }
  };

  const runSave = async () => {
    await create.mutateAsync(payload());
    onClose();
  };

  return (
    <DialogSurface>
      <DialogBody>
        <DialogTitle>Add vector store</DialogTitle>
        <DialogContent>
          <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
            <Field
              label={
                <InfoLabel info="Pick a backend. pgvector uses the platform's existing asset_embeddings table — free, fine up to ~10k vectors per tenant. Pinecone / Weaviate / Qdrant scale beyond that.">
                  Backend
                </InfoLabel>
              }
              required
            >
              <Dropdown
                value={KIND_LABELS[kind]}
                selectedOptions={[kind]}
                onOptionSelect={(_, d) => {
                  setKind(d.optionValue as VectorStoreKind);
                  setTested(null);
                }}
              >
                {(Object.keys(KIND_LABELS) as VectorStoreKind[]).map((k) => (
                  <Option key={k} value={k} text={KIND_LABELS[k]}>
                    {KIND_LABELS[k]}
                  </Option>
                ))}
              </Dropdown>
            </Field>

            <Field
              label={
                <InfoLabel info="A label only your tenant sees — e.g. 'Pinecone prod-us-east'. Used in the queue + audit logs.">
                  Nickname
                </InfoLabel>
              }
              required
            >
              <Input value={name} onChange={(_, d) => { setName(d.value); setTested(null); }} />
            </Field>

            {has("endpoint") && (
              <Field label={FIELD_LABELS.endpoint} required>
                <Input
                  placeholder={FIELD_PLACEHOLDERS.endpoint}
                  value={endpoint}
                  onChange={(_, d) => { setEndpoint(d.value); setTested(null); }}
                />
              </Field>
            )}
            {has("index_name") && (
              <Field label={FIELD_LABELS.index_name} required>
                <Input
                  placeholder={FIELD_PLACEHOLDERS.index_name}
                  value={indexName}
                  onChange={(_, d) => { setIndexName(d.value); setTested(null); }}
                />
              </Field>
            )}
            {has("class_name") && (
              <Field label={FIELD_LABELS.class_name} required>
                <Input
                  placeholder={FIELD_PLACEHOLDERS.class_name}
                  value={className}
                  onChange={(_, d) => { setClassName(d.value); setTested(null); }}
                />
              </Field>
            )}
            {has("collection") && (
              <Field label={FIELD_LABELS.collection} required>
                <Input
                  placeholder={FIELD_PLACEHOLDERS.collection}
                  value={collection}
                  onChange={(_, d) => { setCollection(d.value); setTested(null); }}
                />
              </Field>
            )}
            {has("dimension") && (
              <Field
                label={
                  <InfoLabel info="Vector dimension declared on the index/collection. Must match what your embedding model outputs (e.g. OpenAI text-embedding-3-small → 1536).">
                    {FIELD_LABELS.dimension}
                  </InfoLabel>
                }
              >
                <Input
                  placeholder={FIELD_PLACEHOLDERS.dimension}
                  value={dimension}
                  onChange={(_, d) => { setDimension(d.value); setTested(null); }}
                />
              </Field>
            )}
            {has("api_key") && (
              <Field
                label={
                  <InfoLabel info="Stored encrypted at rest (AES-256-GCM). Never returned through the API after save — to rotate, edit and provide a new key.">
                    {FIELD_LABELS.api_key}
                  </InfoLabel>
                }
                required
              >
                <Input
                  type="password"
                  placeholder={FIELD_PLACEHOLDERS.api_key}
                  value={apiKey}
                  onChange={(_, d) => { setApiKey(d.value); setTested(null); }}
                />
              </Field>
            )}

            <Switch
              label="Make this the tenant default"
              checked={isPrimary}
              onChange={(_, d) => setIsPrimary(d.checked)}
            />

            {tested && (
              <MessageBar intent={tested.ok ? "success" : "error"}>
                <MessageBarBody>
                  {tested.ok
                    ? "Connectivity verified. You can save."
                    : `Test failed: ${tested.error}`}
                </MessageBarBody>
              </MessageBar>
            )}
            {(kind === "pgvector" || kind === "memory") && (
              <Body1 style={{ color: tokens.colorNeutralForeground3, fontSize: 12 }}>
                <Text>This backend is local — no connectivity test needed.</Text>
              </Body1>
            )}
          </div>
        </DialogContent>
        <DialogActions>
          <DialogTrigger disableButtonEnhancement>
            <Button appearance="secondary" onClick={onClose}>Cancel</Button>
          </DialogTrigger>
          {kind !== "pgvector" && kind !== "memory" && (
            <Button onClick={runTest} disabled={testInline.isPending}>
              {testInline.isPending ? <Spinner size="extra-tiny" /> : "Test"}
            </Button>
          )}
          <Button
            appearance="primary"
            onClick={runSave}
            disabled={!canSave || create.isPending}
          >
            {create.isPending ? <Spinner size="extra-tiny" /> : "Save"}
          </Button>
        </DialogActions>
      </DialogBody>
    </DialogSurface>
  );
}
