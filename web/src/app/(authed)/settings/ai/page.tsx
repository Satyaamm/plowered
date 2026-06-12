"use client";

import { useState } from "react";
import {
  Badge,
  Body1,
  Button,
  Caption1,
  Card,
  Combobox,
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
  Subtitle2,
  Switch,
  Text,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  Add20Regular,
  CheckmarkCircle20Regular,
  Delete20Regular,
  ErrorCircle20Regular,
  Sparkle24Regular,
} from "@fluentui/react-icons";
import { PageHeader } from "@/components/page-header";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";
import { InfoLabel } from "@/components/info-label";
import {
  AICapability,
  AIProvider,
  AIProviderInput,
  AIProviderKind,
  SUGGESTED_MODELS,
  useAIProviders,
  useCreateAIProvider,
  useDeleteAIProvider,
  useRole,
  useSetPrimaryAIProvider,
  useTestInlineAIProvider,
  useTestStoredAIProvider,
} from "@/lib/hooks";

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "16px" },
  card: {
    backgroundColor: tokens.colorNeutralBackground1,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
    padding: "16px 20px",
    display: "flex",
    flexDirection: "column",
    gap: "10px",
  },
  cardHeader: {
    display: "flex",
    justifyContent: "space-between",
    alignItems: "flex-start",
    gap: "16px",
  },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  formGrid: {
    display: "grid",
    gridTemplateColumns: "1fr 1fr",
    gap: "12px",
  },
  actionRow: { display: "flex", gap: "8px", alignItems: "center" },
});

const KIND_LABELS: Record<AIProviderKind, string> = {
  anthropic: "Anthropic (Claude)",
  openai: "OpenAI (GPT)",
  gemini: "Google Gemini",
  "azure-openai": "Azure OpenAI",
  bedrock: "AWS Bedrock",
  vertex: "GCP Vertex AI",
  cohere: "Cohere",
  voyage: "Voyage AI (embeddings)",
  mistral: "Mistral AI",
  groq: "Groq",
  together: "Together AI",
  fireworks: "Fireworks AI",
  perplexity: "Perplexity",
  xai: "xAI (Grok)",
  deepseek: "DeepSeek",
  ollama: "Ollama (self-hosted)",
  "openai-compatible": "Custom (OpenAI-compatible)",
};

// requiredFields drives the wizard — which extra inputs to show beyond
// the universal {name, model, api_key, capability} set. Kept here so
// adding a new kind only requires touching this map + KIND_LABELS +
// SUGGESTED_MODELS, not the form JSX.
type FieldKey =
  | "api_key"
  | "base_url"
  | "deployment"
  | "api_version"
  | "region"
  | "project"
  | "location";

const KIND_FIELDS: Record<AIProviderKind, FieldKey[]> = {
  anthropic:         ["api_key"],
  openai:            ["api_key"],
  gemini:            ["api_key"],
  cohere:            ["api_key"],
  voyage:            ["api_key"],
  mistral:           ["api_key"],
  groq:              ["api_key"],
  together:          ["api_key"],
  fireworks:         ["api_key"],
  perplexity:        ["api_key"],
  xai:               ["api_key"],
  deepseek:          ["api_key"],
  ollama:            ["base_url"],
  "openai-compatible": ["base_url", "api_key"],
  "azure-openai":    ["base_url", "deployment", "api_version", "api_key"],
  bedrock:           ["region", "api_key"],
  vertex:            ["project", "location", "api_key"],
};

// FIELD_LABELS + FIELD_PLACEHOLDERS drive how each per-kind input
// renders. The api_key field's label changes per kind to match what the
// provider calls it ("Service account JSON" for Vertex etc.).
const FIELD_LABELS: Record<FieldKey, string> = {
  api_key:     "API key",
  base_url:    "Base URL",
  deployment:  "Deployment name",
  api_version: "API version",
  region:      "AWS region",
  project:     "GCP project ID",
  location:    "GCP location",
};

const FIELD_PLACEHOLDERS: Record<FieldKey, string> = {
  api_key:     "paste-your-key-here",
  base_url:    "https://your-host.example.com",
  deployment:  "my-gpt4o-deployment",
  api_version: "2024-06-01",
  region:      "us-east-1",
  project:     "my-gcp-project",
  location:    "us-central1",
};

function apiKeyLabelFor(kind: AIProviderKind): string {
  if (kind === "vertex") return "Service account JSON (optional — defaults to ADC)";
  if (kind === "bedrock") return "AWS creds JSON (optional — defaults to IAM role)";
  if (kind === "ollama") return "API key (Ollama usually has none)";
  return "API key";
}

function apiKeyPlaceholderFor(kind: AIProviderKind): string {
  if (kind === "vertex") return '{"type":"service_account",...}';
  if (kind === "bedrock") return '{"access_key_id":"AKIA...","secret_access_key":"..."}';
  return "paste-your-key-here";
}

export default function AIProvidersPage() {
  const styles = useStyles();
  const list = useAIProviders();
  const del = useDeleteAIProvider();
  const setPrimary = useSetPrimaryAIProvider();
  const testStored = useTestStoredAIProvider();
  const { can } = useRole();
  const canAdmin = can("admin");
  const [open, setOpen] = useState(false);

  return (
    <div className={styles.root}>
      <PageHeader
        crumbs={[{ label: "Management" }, { label: "AI providers" }]}
        title="AI providers"
        subtitle="Bring your own Claude, OpenAI, DeepSeek or any OpenAI-compatible endpoint. Keys are stored encrypted in the secrets vault."
        actions={
          canAdmin && (
            <Dialog open={open} onOpenChange={(_, d) => setOpen(d.open)}>
              <DialogTrigger disableButtonEnhancement>
                <Button appearance="primary" icon={<Add20Regular />}>
                  Add provider
                </Button>
              </DialogTrigger>
              <AddProviderDialog onClose={() => setOpen(false)} />
            </Dialog>
          )
        }
      />

      {list.isLoading && <LoadingState />}
      {list.error && <ErrorBanner error={list.error as Error} />}
      {list.data && list.data.length === 0 && (
        <EmptyState
          title="No providers configured"
          body="Add Claude, OpenAI or DeepSeek to enable semantic search, glossary auto-write and other LLM-driven features."
        />
      )}
      {list.data && list.data.length > 0 && (
        <div style={{ display: "flex", flexDirection: "column", gap: "12px" }}>
          {list.data.map((p) => (
            <ProviderRow
              key={p.id}
              p={p}
              onDelete={() => del.mutate(p.id)}
              onTest={() => testStored.mutate(p.id)}
              testing={testStored.isPending && testStored.variables === p.id}
              onSetPrimary={() => setPrimary.mutate(p.id)}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function ProviderRow({
  p,
  onDelete,
  onTest,
  testing,
  onSetPrimary,
}: {
  p: AIProvider;
  onDelete: () => void;
  onTest: () => void;
  testing: boolean;
  onSetPrimary: () => void;
}) {
  const styles = useStyles();
  return (
    <Card className={styles.card}>
      <div className={styles.cardHeader}>
        <div style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
          <div style={{ display: "flex", gap: "8px", alignItems: "center" }}>
            <Sparkle24Regular />
            <Subtitle2>{p.name}</Subtitle2>
            <Badge appearance="outline" color="brand">
              {KIND_LABELS[p.kind]}
            </Badge>
            <Badge
              appearance="outline"
              color={p.capability === "chat" ? "informative" : "success"}
            >
              {p.capability}
            </Badge>
            {p.is_primary && (
              <Badge appearance="filled" color="success">
                primary
              </Badge>
            )}
          </div>
          <Caption1 className={styles.meta}>
            {p.model}
            {p.base_url ? ` · ${p.base_url}` : ""}
          </Caption1>
          {p.last_tested_at && (
            <Caption1 className={styles.meta}>
              Last tested{" "}
              {new Date(p.last_tested_at).toLocaleString()} —{" "}
              {p.last_test_ok ? (
                <span style={{ color: tokens.colorPaletteGreenForeground2 }}>
                  <CheckmarkCircle20Regular
                    style={{ verticalAlign: "middle" }}
                  />{" "}
                  valid
                </span>
              ) : (
                <span style={{ color: tokens.colorPaletteRedForeground2 }}>
                  <ErrorCircle20Regular style={{ verticalAlign: "middle" }} />{" "}
                  {p.last_test_error || "failed"}
                </span>
              )}
            </Caption1>
          )}
        </div>
        <div className={styles.actionRow}>
          <Button onClick={onTest} disabled={testing}>
            {testing ? <Spinner size="extra-tiny" /> : "Test"}
          </Button>
          {!p.is_primary && (
            <Button appearance="subtle" onClick={onSetPrimary}>
              Make primary
            </Button>
          )}
          <Button
            appearance="subtle"
            icon={<Delete20Regular />}
            onClick={onDelete}
            aria-label="Delete provider"
          />
        </div>
      </div>
    </Card>
  );
}

function AddProviderDialog({ onClose }: { onClose: () => void }) {
  const create = useCreateAIProvider();
  const testInline = useTestInlineAIProvider();
  const [kind, setKind] = useState<AIProviderKind>("anthropic");
  const [name, setName] = useState("");
  const [model, setModel] = useState("");
  const [baseURL, setBaseURL] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [deployment, setDeployment] = useState("");
  const [apiVersion, setApiVersion] = useState("");
  const [region, setRegion] = useState("");
  const [project, setProject] = useState("");
  const [location, setLocation] = useState("");
  const [capability, setCapability] = useState<AICapability>("chat");
  const [isPrimary, setIsPrimary] = useState(false);
  const [tested, setTested] = useState<null | { ok: boolean; error?: string }>(
    null,
  );

  const fields = KIND_FIELDS[kind];
  const has = (k: FieldKey) => fields.includes(k);

  const payload = (): AIProviderInput => ({
    kind,
    name,
    model,
    base_url: has("base_url") && baseURL ? baseURL : undefined,
    api_key: apiKey || undefined,
    deployment: has("deployment") && deployment ? deployment : undefined,
    api_version: has("api_version") && apiVersion ? apiVersion : undefined,
    region: has("region") && region ? region : undefined,
    project: has("project") && project ? project : undefined,
    location: has("location") && location ? location : undefined,
    capability,
    is_primary: isPrimary,
  });

  // Per-kind validation. Bedrock + Vertex allow empty api_key (default
  // credential chain on the host); Ollama does too. Everyone else
  // requires a key.
  const apiKeyRequired = kind !== "bedrock" && kind !== "vertex" && kind !== "ollama";
  const canTest =
    name.trim() !== "" &&
    model.trim() !== "" &&
    (!apiKeyRequired || apiKey.trim() !== "") &&
    (!has("base_url") || baseURL.trim() !== "") &&
    (!has("deployment") || deployment.trim() !== "") &&
    (!has("api_version") || apiVersion.trim() !== "") &&
    (!has("region") || region.trim() !== "") &&
    (!has("project") || project.trim() !== "") &&
    (!has("location") || location.trim() !== "");

  const canSave = canTest && tested?.ok === true;

  const runTest = async () => {
    try {
      const res = await testInline.mutateAsync(payload());
      setTested(res);
    } catch (e: unknown) {
      const err = e as { payload?: { error?: string }; message?: string };
      setTested({
        ok: false,
        error: err.payload?.error || err.message || "test failed",
      });
    }
  };

  const runSave = async () => {
    await create.mutateAsync(payload());
    onClose();
  };

  return (
    <DialogSurface>
      <DialogBody>
        <DialogTitle>Add AI provider</DialogTitle>
        <DialogContent>
          <div style={{ display: "flex", flexDirection: "column", gap: "12px" }}>
            <Field
              label={
                <InfoLabel info="Which model vendor you're authenticating with. The three first-class options use the vendor's native SDK; pick 'Custom (OpenAI-compatible)' for self-hosted gateways like LiteLLM, Ollama, OpenRouter, or vLLM.">
                  Provider
                </InfoLabel>
              }
              required
            >
              <Dropdown
                value={KIND_LABELS[kind]}
                selectedOptions={[kind]}
                onOptionSelect={(_, d) => {
                  setKind(d.optionValue as AIProviderKind);
                  setTested(null);
                  setModel("");
                  // Wipe per-kind state so a half-filled Azure form
                  // doesn't survive a switch to plain OpenAI.
                  setBaseURL("");
                  setDeployment("");
                  setApiVersion("");
                  setRegion("");
                  setProject("");
                  setLocation("");
                }}
              >
                {(Object.keys(KIND_LABELS) as AIProviderKind[]).map((k) => (
                  <Option key={k} value={k} text={KIND_LABELS[k]}>
                    {KIND_LABELS[k]}
                  </Option>
                ))}
              </Dropdown>
            </Field>

            <Field
              label={
                <InfoLabel info="A label only your tenant sees — e.g. 'Claude Opus (prod)'. Used in audit logs and the provider dropdown when multiple are configured for the same capability.">
                  Nickname
                </InfoLabel>
              }
              required
            >
              <Input
                value={name}
                onChange={(_, d) => {
                  setName(d.value);
                  setTested(null);
                }}
              />
            </Field>

            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "12px" }}>
              <Field
                label={
                  <InfoLabel info="The exact model ID the provider expects in API calls — case-sensitive. Plowered sends it verbatim; mistype here and every call fails with a 'model not found' error.">
                    Model
                  </InfoLabel>
                }
                required
              >
                <Combobox
                  freeform
                  value={model}
                  onInput={(e) => {
                    setModel((e.target as HTMLInputElement).value);
                    setTested(null);
                  }}
                  onOptionSelect={(_, d) => {
                    setModel(d.optionValue ?? "");
                    setTested(null);
                  }}
                >
                  {SUGGESTED_MODELS[kind].map((m) => (
                    <Option key={m} value={m}>
                      {m}
                    </Option>
                  ))}
                </Combobox>
              </Field>

              <Field
                label={
                  <InfoLabel info="Chat: text generation, glossary auto-write, semantic answers. Embeddings: vector representations for semantic search. Each provider entry serves exactly one capability.">
                    Capability
                  </InfoLabel>
                }
                required
              >
                <Dropdown
                  value={capability === "chat" ? "Chat / generation" : "Embeddings"}
                  selectedOptions={[capability]}
                  onOptionSelect={(_, d) => {
                    setCapability(d.optionValue as AICapability);
                    setTested(null);
                  }}
                >
                  <Option value="chat">Chat / generation</Option>
                  <Option value="embed">Embeddings</Option>
                </Dropdown>
              </Field>
            </div>

            {/* Per-kind fields driven by KIND_FIELDS so the form only
                renders inputs that the selected provider actually
                consumes. Order matters: connection-shape fields first
                (base_url, deployment, etc.), credentials last so the
                user reads the form top-to-bottom. */}
            {has("base_url") && (
              <Field
                label={
                  <InfoLabel info="Required by this provider. Point at the API endpoint — for Azure OpenAI it's https://<resource>.openai.azure.com; for Ollama it's the local /v1 endpoint; for openai-compatible gateways (LiteLLM, OpenRouter, vLLM) any /v1-style URL.">
                    {FIELD_LABELS.base_url}
                  </InfoLabel>
                }
                required
              >
                <Input
                  placeholder={FIELD_PLACEHOLDERS.base_url}
                  value={baseURL}
                  onChange={(_, d) => { setBaseURL(d.value); setTested(null); }}
                />
              </Field>
            )}

            {has("deployment") && (
              <Field
                label={
                  <InfoLabel info="Azure OpenAI routes requests to a specific deployment of an underlying OpenAI model. The deployment name is what you chose in the Azure portal — it's NOT the model id (e.g. 'my-gpt4o-prod', not 'gpt-4o').">
                    {FIELD_LABELS.deployment}
                  </InfoLabel>
                }
                required
              >
                <Input
                  placeholder={FIELD_PLACEHOLDERS.deployment}
                  value={deployment}
                  onChange={(_, d) => { setDeployment(d.value); setTested(null); }}
                />
              </Field>
            )}

            {has("api_version") && (
              <Field
                label={
                  <InfoLabel info="Azure OpenAI pins behaviour to a specific API version. Use the latest GA value from Microsoft's reference docs unless you're locked to a preview feature.">
                    {FIELD_LABELS.api_version}
                  </InfoLabel>
                }
                required
              >
                <Input
                  placeholder={FIELD_PLACEHOLDERS.api_version}
                  value={apiVersion}
                  onChange={(_, d) => { setApiVersion(d.value); setTested(null); }}
                />
              </Field>
            )}

            {has("region") && (
              <Field
                label={
                  <InfoLabel info="AWS region the Bedrock model is enabled in. Model availability varies by region — check the Bedrock console under 'Model access' before saving.">
                    {FIELD_LABELS.region}
                  </InfoLabel>
                }
                required
              >
                <Input
                  placeholder={FIELD_PLACEHOLDERS.region}
                  value={region}
                  onChange={(_, d) => { setRegion(d.value); setTested(null); }}
                />
              </Field>
            )}

            {(has("project") || has("location")) && (
              <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "12px" }}>
                {has("project") && (
                  <Field
                    label={
                      <InfoLabel info="GCP project ID hosting the Vertex AI workload. Not the project NUMBER — the lowercase string ID (e.g. 'my-data-prod-12345').">
                        {FIELD_LABELS.project}
                      </InfoLabel>
                    }
                    required
                  >
                    <Input
                      placeholder={FIELD_PLACEHOLDERS.project}
                      value={project}
                      onChange={(_, d) => { setProject(d.value); setTested(null); }}
                    />
                  </Field>
                )}
                {has("location") && (
                  <Field
                    label={
                      <InfoLabel info="GCP region the model is served from. Vertex availability differs by region — Gemini in us-central1, Claude on Vertex in us-east5, etc.">
                        {FIELD_LABELS.location}
                      </InfoLabel>
                    }
                    required
                  >
                    <Input
                      placeholder={FIELD_PLACEHOLDERS.location}
                      value={location}
                      onChange={(_, d) => { setLocation(d.value); setTested(null); }}
                    />
                  </Field>
                )}
              </div>
            )}

            {has("api_key") && (
              <Field
                label={
                  <InfoLabel info="Stored encrypted at rest (AES-256-GCM) in the secrets vault. Never returned through the API after save — to rotate, delete and re-create the provider. Bedrock / Vertex / Ollama accept an empty value to defer to the host's default credential chain.">
                    {apiKeyLabelFor(kind)}
                  </InfoLabel>
                }
                required={apiKeyRequired}
              >
                <Input
                  type="password"
                  placeholder={apiKeyPlaceholderFor(kind)}
                  value={apiKey}
                  onChange={(_, d) => { setApiKey(d.value); setTested(null); }}
                />
              </Field>
            )}

            <Switch
              label="Make this the tenant default for its capability"
              checked={isPrimary}
              onChange={(_, d) => setIsPrimary(d.checked)}
            />

            {tested && (
              <MessageBar intent={tested.ok ? "success" : "error"}>
                <MessageBarBody>
                  {tested.ok
                    ? "Credentials valid. You can save."
                    : `Test failed: ${tested.error}`}
                </MessageBarBody>
              </MessageBar>
            )}
            {!tested && (
              <Body1 style={{ color: tokens.colorNeutralForeground3, fontSize: 12 }}>
                <Text>
                  Click <strong>Test</strong> first — we ping the provider with
                  your key (no tokens consumed) and enable Save on success.
                </Text>
              </Body1>
            )}
          </div>
        </DialogContent>
        <DialogActions>
          <DialogTrigger disableButtonEnhancement>
            <Button appearance="secondary" onClick={onClose}>
              Cancel
            </Button>
          </DialogTrigger>
          <Button
            onClick={runTest}
            disabled={!canTest || testInline.isPending}
          >
            {testInline.isPending ? <Spinner size="extra-tiny" /> : "Test"}
          </Button>
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
