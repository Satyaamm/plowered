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
import {
  AICapability,
  AIProvider,
  AIProviderInput,
  AIProviderKind,
  SUGGESTED_MODELS,
  useAIProviders,
  useCreateAIProvider,
  useDeleteAIProvider,
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
  deepseek: "DeepSeek",
  "openai-compatible": "Custom (OpenAI-compatible)",
};

export default function AIProvidersPage() {
  const styles = useStyles();
  const list = useAIProviders();
  const del = useDeleteAIProvider();
  const setPrimary = useSetPrimaryAIProvider();
  const testStored = useTestStoredAIProvider();
  const [open, setOpen] = useState(false);

  return (
    <div className={styles.root}>
      <PageHeader
        crumbs={[{ label: "Management" }, { label: "AI providers" }]}
        title="AI providers"
        subtitle="Bring your own Claude, OpenAI, DeepSeek or any OpenAI-compatible endpoint. Keys are stored encrypted in the secrets vault."
        actions={
          <Dialog open={open} onOpenChange={(_, d) => setOpen(d.open)}>
            <DialogTrigger disableButtonEnhancement>
              <Button appearance="primary" icon={<Add20Regular />}>
                Add provider
              </Button>
            </DialogTrigger>
            <AddProviderDialog onClose={() => setOpen(false)} />
          </Dialog>
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
  const [capability, setCapability] = useState<AICapability>("chat");
  const [isPrimary, setIsPrimary] = useState(false);
  const [tested, setTested] = useState<null | { ok: boolean; error?: string }>(
    null,
  );

  const payload = (): AIProviderInput => ({
    kind,
    name,
    model,
    base_url: baseURL || undefined,
    api_key: apiKey,
    capability,
    is_primary: isPrimary,
  });

  const canTest =
    name.trim() &&
    model.trim() &&
    apiKey.trim() &&
    (kind !== "openai-compatible" || baseURL.trim());

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
            <Field label="Provider" required>
              <Dropdown
                value={KIND_LABELS[kind]}
                selectedOptions={[kind]}
                onOptionSelect={(_, d) => {
                  setKind(d.optionValue as AIProviderKind);
                  setTested(null);
                  setModel("");
                }}
              >
                {(
                  [
                    "anthropic",
                    "openai",
                    "deepseek",
                    "openai-compatible",
                  ] as AIProviderKind[]
                ).map((k) => (
                  <Option key={k} value={k} text={KIND_LABELS[k]}>
                    {KIND_LABELS[k]}
                  </Option>
                ))}
              </Dropdown>
            </Field>

            <Field
              label="Nickname"
              required
              hint="A label only your tenant sees, e.g. “Claude Opus (prod)”."
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
                label="Model"
                required
                hint="Provider-specific model ID. Suggestions below."
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

              <Field label="Capability" required>
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

            <Field
              label="Base URL"
              required={kind === "openai-compatible"}
              hint={
                kind === "openai-compatible"
                  ? "Required. Point at any OpenAI-compatible endpoint (LiteLLM, Ollama, OpenRouter, vLLM…)."
                  : "Optional. Leave blank for the official provider host."
              }
            >
              <Input
                placeholder={
                  kind === "anthropic"
                    ? "https://api.anthropic.com"
                    : kind === "deepseek"
                      ? "https://api.deepseek.com"
                      : "https://api.openai.com"
                }
                value={baseURL}
                onChange={(_, d) => {
                  setBaseURL(d.value);
                  setTested(null);
                }}
              />
            </Field>

            <Field
              label="API key"
              required
              hint="Stored encrypted in the secrets vault. Never visible after save."
            >
              <Input
                type="password"
                value={apiKey}
                onChange={(_, d) => {
                  setApiKey(d.value);
                  setTested(null);
                }}
              />
            </Field>

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
