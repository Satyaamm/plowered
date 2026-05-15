"use client";

import { useEffect, useMemo, useState } from "react";
import {
  Body1,
  Button,
  Combobox,
  Drawer,
  DrawerBody,
  DrawerHeader,
  DrawerHeaderTitle,
  Dropdown,
  Field,
  Input,
  MessageBar,
  MessageBarBody,
  Option,
  Switch,
  Textarea,
  Title3,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { Dismiss24Regular } from "@fluentui/react-icons";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { useCreateCheck, useUpdateCheck } from "@/lib/hooks";
import { InfoLabel } from "@/components/info-label";
import type { Check } from "@/lib/types-orchestration";

const TYPES: { value: string; label: string; help: string }[] = [
  { value: "row_count",   label: "Row count",   help: "Asserts row count is within [min, max]." },
  { value: "not_null",    label: "Not null",    help: "Counts NULLs in a column; fails if any are present." },
  { value: "freshness",   label: "Freshness",   help: "Asserts the most recent timestamp is within max_age_seconds of now." },
  { value: "uniqueness",  label: "Uniqueness",  help: "Asserts a column (or a column tuple) has no duplicate values." },
  { value: "custom_sql",  label: "Custom SQL",  help: "Runs your SQL; passes when the result matches the expected shape." },
];

const SEVERITIES = ["info", "warning", "error", "critical"];

const useStyles = makeStyles({
  body: { display: "flex", flexDirection: "column", gap: "18px", padding: "16px 0" },
  row: { display: "grid", gridTemplateColumns: "1fr 1fr", gap: "12px" },
  configBlock: {
    backgroundColor: tokens.colorNeutralBackground2,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
    padding: "12px",
    display: "flex",
    flexDirection: "column",
    gap: "10px",
  },
  hint: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  footer: {
    display: "flex",
    gap: "8px",
    justifyContent: "flex-end",
    paddingTop: "12px",
    borderTop: `1px solid ${tokens.colorNeutralStroke2}`,
    marginTop: "auto",
  },
});

interface Props {
  open: boolean;
  onClose: () => void;
  /** When provided, the drawer opens in edit mode and pre-fills from this check. */
  existing?: Check | null;
  /** When provided, the asset selector is locked to this asset. */
  fixedAsset?: { id: string; qn?: string };
}

export function CheckDesigner({ open, onClose, existing, fixedAsset }: Props) {
  const styles = useStyles();
  const create = useCreateCheck();
  const update = useUpdateCheck(existing?.ID ?? "");

  const [name, setName] = useState("");
  const [type, setType] = useState<string>("row_count");
  const [severity, setSeverity] = useState("warning");
  const [enabled, setEnabled] = useState(true);
  const [assetId, setAssetId] = useState(fixedAsset?.id ?? "");
  const [assetQuery, setAssetQuery] = useState(fixedAsset?.qn ?? "");
  const [config, setConfig] = useState<Record<string, string>>({});

  // Reset state when opening with new context.
  useEffect(() => {
    if (!open) return;
    if (existing) {
      setName(existing.Name);
      setType(existing.Type);
      setSeverity(existing.Severity ?? "warning");
      setEnabled(existing.Enabled);
      setAssetId(existing.AssetID);
      setAssetQuery(existing.AssetQN ?? "");
      setConfig(stringifyConfig(existing.Config ?? {}));
    } else {
      setName("");
      setType("row_count");
      setSeverity("warning");
      setEnabled(true);
      setAssetId(fixedAsset?.id ?? "");
      setAssetQuery(fixedAsset?.qn ?? "");
      setConfig({});
    }
  }, [open, existing, fixedAsset]);

  const assetSearch = useQuery({
    queryKey: ["asset-search", assetQuery],
    queryFn: ({ signal }) => api.search(assetQuery || "*", { limit: 20 }, { signal }),
    enabled: open && !fixedAsset,
    staleTime: 5_000,
    select: (d) => (d.hits ?? []).map((h) => h.asset),
  });

  const valid = name && assetId && type;
  const submitErr = create.error ?? update.error;
  const pending = create.isPending || update.isPending;

  const submit = async () => {
    if (!valid) return;
    const payload: Partial<Check> = {
      Name: name,
      AssetID: assetId,
      Type: type as Check["Type"],
      Severity: severity as Check["Severity"],
      Enabled: enabled,
      Config: parseConfig(type, config),
    };
    if (existing) {
      await update.mutateAsync(payload);
    } else {
      await create.mutateAsync(payload);
    }
    onClose();
  };

  return (
    <Drawer
      type="overlay"
      separator
      open={open}
      onOpenChange={(_, d) => !d.open && onClose()}
      position="end"
      size="medium"
    >
      <DrawerHeader>
        <DrawerHeaderTitle
          action={
            <Button appearance="subtle" icon={<Dismiss24Regular />} onClick={onClose} />
          }
        >
          <Title3>{existing ? "Edit check" : "New check"}</Title3>
        </DrawerHeaderTitle>
      </DrawerHeader>
      <DrawerBody>
        <div className={styles.body}>
          <Field
            label={
              <InfoLabel info="Short identifier for this check — shown in run results, alerts, and the asset's quality panel. Pick something a stakeholder can read at a glance (e.g. 'orders rowcount > 0').">
                Name
              </InfoLabel>
            }
            required
          >
            <Input value={name} onChange={(_, d) => setName(d.value)} placeholder="e.g. orders rowcount > 0" />
          </Field>

          {!fixedAsset && (
            <Field
              label={
                <InfoLabel info="The catalog asset this check runs against. Search by qualified name (e.g. postgres.public.users). Only assets your role can read are returned.">
                  Asset
                </InfoLabel>
              }
              required
            >
              <Combobox
                value={assetQuery}
                selectedOptions={assetId ? [assetId] : []}
                onOptionSelect={(_, d) => {
                  setAssetId(d.optionValue ?? "");
                  setAssetQuery(d.optionText ?? "");
                }}
                onInput={(e) => setAssetQuery((e.target as HTMLInputElement).value)}
                placeholder="postgres.public.users"
              >
                {(assetSearch.data ?? []).map((a) => (
                  <Option key={a.id} value={a.id} text={a.qualified_name}>
                    {a.qualified_name}
                  </Option>
                ))}
              </Combobox>
            </Field>
          )}

          <div className={styles.row}>
            <Field
              label={
                <InfoLabel info="Which expectation engine to apply. row_count = volume; not_null = nullability; freshness = recency vs. now; uniqueness = no duplicates; custom_sql = your own assertion query.">
                  Type
                </InfoLabel>
              }
              required
            >
              <Dropdown
                value={TYPES.find((t) => t.value === type)?.label ?? type}
                selectedOptions={[type]}
                onOptionSelect={(_, d) => {
                  setType(d.optionValue ?? "row_count");
                  setConfig({});
                }}
              >
                {TYPES.map((t) => (
                  <Option key={t.value} value={t.value} text={t.label}>
                    {t.label}
                  </Option>
                ))}
              </Dropdown>
            </Field>
            <Field
              label={
                <InfoLabel info="info = visible in dashboards only. warning = posts to the asset and #data-quality channels. error = pages the asset owner. critical = pages on-call + locks downstream pipelines until resolved.">
                  Severity
                </InfoLabel>
              }
            >
              <Dropdown
                value={severity}
                selectedOptions={[severity]}
                onOptionSelect={(_, d) => setSeverity(d.optionValue ?? "warning")}
              >
                {SEVERITIES.map((s) => (
                  <Option key={s} value={s} text={s}>
                    {s}
                  </Option>
                ))}
              </Dropdown>
            </Field>
          </div>

          <div className={styles.configBlock}>
            <Body1>{TYPES.find((t) => t.value === type)?.help}</Body1>
            <ConfigFields type={type} config={config} setConfig={setConfig} />
          </div>

          <Switch
            label="Enabled"
            checked={enabled}
            onChange={(_, d) => setEnabled(d.checked)}
          />

          {submitErr && (
            <MessageBar intent="error">
              <MessageBarBody>{(submitErr as Error).message}</MessageBarBody>
            </MessageBar>
          )}

          <div className={styles.footer}>
            <Button onClick={onClose} disabled={pending}>Cancel</Button>
            <Button
              appearance="primary"
              onClick={submit}
              disabled={!valid || pending}
            >
              {pending ? "Saving…" : existing ? "Save" : "Create"}
            </Button>
          </div>
        </div>
      </DrawerBody>
    </Drawer>
  );
}

function ConfigFields({
  type,
  config,
  setConfig,
}: {
  type: string;
  config: Record<string, string>;
  setConfig: (c: Record<string, string>) => void;
}) {
  const set = (k: string, v: string) => setConfig({ ...config, [k]: v });

  if (type === "row_count") {
    return (
      <>
        <Field label="Min rows">
          <Input value={config.min ?? ""} onChange={(_, d) => set("min", d.value)} placeholder="0" />
        </Field>
        <Field label="Max rows (optional)">
          <Input value={config.max ?? ""} onChange={(_, d) => set("max", d.value)} placeholder="" />
        </Field>
      </>
    );
  }
  if (type === "not_null") {
    return (
      <Field label="Column" required>
        <Input value={config.column ?? ""} onChange={(_, d) => set("column", d.value)} placeholder="email" />
      </Field>
    );
  }
  if (type === "freshness") {
    return (
      <>
        <Field label="Timestamp column" required>
          <Input value={config.column ?? ""} onChange={(_, d) => set("column", d.value)} placeholder="updated_at" />
        </Field>
        <Field label="Max age (seconds)" required hint="Fail when newest row is older than this">
          <Input value={config.max_age_seconds ?? ""} onChange={(_, d) => set("max_age_seconds", d.value)} placeholder="86400" />
        </Field>
      </>
    );
  }
  if (type === "uniqueness") {
    return (
      <Field label="Column(s)" required hint="Comma-separated for composite uniqueness">
        <Input value={config.columns ?? ""} onChange={(_, d) => set("columns", d.value)} placeholder="user_id" />
      </Field>
    );
  }
  if (type === "custom_sql") {
    return (
      <>
        <Field label="SQL" required hint="Should return one numeric column">
          <Textarea
            rows={6}
            value={config.sql ?? ""}
            onChange={(_, d) => set("sql", d.value)}
            placeholder="SELECT COUNT(*) FROM orders WHERE total < 0"
            style={{ fontFamily: "ui-monospace, monospace" }}
          />
        </Field>
        <Field label="Expectation">
          <Dropdown
            value={config.expect ?? "zero_rows"}
            selectedOptions={[config.expect ?? "zero_rows"]}
            onOptionSelect={(_, d) => set("expect", d.optionValue ?? "zero_rows")}
          >
            <Option value="zero_rows" text="Zero rows / count = 0">Zero rows / count = 0</Option>
            <Option value="non_zero" text="Non-zero">Non-zero</Option>
          </Dropdown>
        </Field>
      </>
    );
  }
  return null;
}

function stringifyConfig(c: Record<string, unknown>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(c)) {
    out[k] = typeof v === "string" ? v : JSON.stringify(v);
  }
  return out;
}

function parseConfig(type: string, c: Record<string, string>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(c)) {
    if (v === "" || v === undefined) continue;
    // numeric coercion for known numeric keys
    if (k === "min" || k === "max" || k === "max_age_seconds") {
      const n = Number(v);
      if (!Number.isNaN(n)) out[k] = n;
      continue;
    }
    out[k] = v;
  }
  // Type-specific normalization
  if (type === "uniqueness" && typeof out.columns === "string") {
    out.columns = (out.columns as string).split(",").map((s) => s.trim()).filter(Boolean);
  }
  return out;
}
