"use client";

import {
  Badge,
  Body1,
  Button,
  Caption1,
  Combobox,
  Option,
  Subtitle2,
  Tooltip,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { Add16Regular, Dismiss16Regular, ShieldCheckmark16Regular } from "@fluentui/react-icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { glossaryApi } from "@/lib/api";
import type { Asset } from "@/lib/types";

const useStyles = makeStyles({
  body: { display: "flex", flexDirection: "column", gap: "20px" },
  twoCol: {
    display: "grid",
    gridTemplateColumns: "minmax(0, 2fr) minmax(0, 1fr)",
    gap: "20px",
    "@media (max-width: 980px)": { gridTemplateColumns: "1fr" },
  },
  panel: {
    backgroundColor: tokens.colorNeutralBackground1,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
    padding: "16px",
    display: "flex",
    flexDirection: "column",
    gap: "12px",
  },
  kvGrid: {
    display: "grid",
    gridTemplateColumns: "max-content 1fr",
    columnGap: "16px",
    rowGap: "8px",
  },
  k: {
    color: tokens.colorNeutralForeground3,
    fontSize: "12px",
    textTransform: "uppercase",
    letterSpacing: "0.04em",
  },
  v: { fontSize: "13px", color: tokens.colorNeutralForeground1 },
  mono: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: "12px",
  },
  tagRow: { display: "flex", flexWrap: "wrap", gap: "6px" },
  empty: { color: tokens.colorNeutralForeground3, fontStyle: "italic" },
});

function tagColor(tag: string): "informative" | "danger" | "warning" | "success" | "subtle" {
  if (tag.startsWith("class:phi") || tag.startsWith("class:pci")) return "danger";
  if (tag.startsWith("class:pii") || tag.startsWith("class:secret")) return "warning";
  return "informative";
}

export function OverviewTab({ asset }: { asset: Asset }) {
  const styles = useStyles();
  const trust = asset.trust ?? "unverified";
  const isCertified = trust === "certified" || trust === "reviewed";
  const props = (asset.properties ?? {}) as Record<string, any>;

  return (
    <div className={styles.body}>
      <div className={styles.twoCol}>
        <div className={styles.panel}>
          <Subtitle2>Description</Subtitle2>
          {asset.description ? (
            <Body1>{asset.description}</Body1>
          ) : (
            <span className={styles.empty}>
              No description yet. Add one from the connection or via API.
            </span>
          )}
        </div>

        <div className={styles.panel}>
          <Subtitle2>Properties</Subtitle2>
          <div className={styles.kvGrid}>
            <span className={styles.k}>Type</span>
            <span className={styles.v}>
              <Badge appearance="outline" color="brand">{asset.type}</Badge>
            </span>
            <span className={styles.k}>Trust</span>
            <span className={styles.v}>
              <Badge
                appearance={isCertified ? "filled" : "outline"}
                color={
                  trust === "certified"
                    ? "success"
                    : trust === "reviewed"
                      ? "informative"
                      : trust === "deprecated"
                        ? "danger"
                        : "subtle"
                }
                icon={isCertified ? <ShieldCheckmark16Regular /> : undefined}
              >
                {trust}
              </Badge>
            </span>
            <span className={styles.k}>Owner</span>
            <span className={styles.v}>
              {(asset.owners ?? []).length > 0 ? asset.owners!.join(", ") : "—"}
            </span>
            {props.connection && (
              <>
                <span className={styles.k}>Connection</span>
                <span className={`${styles.v} ${styles.mono}`}>{String(props.connection)}</span>
              </>
            )}
            {props.schema && (
              <>
                <span className={styles.k}>Schema</span>
                <span className={`${styles.v} ${styles.mono}`}>{String(props.schema)}</span>
              </>
            )}
            {props.table && (
              <>
                <span className={styles.k}>Table</span>
                <span className={`${styles.v} ${styles.mono}`}>{String(props.table)}</span>
              </>
            )}
            {props.data_type && (
              <>
                <span className={styles.k}>Data type</span>
                <span className={`${styles.v} ${styles.mono}`}>{String(props.data_type)}</span>
              </>
            )}
            {props.nullable !== undefined && (
              <>
                <span className={styles.k}>Nullable</span>
                <span className={styles.v}>{props.nullable ? "yes" : "no"}</span>
              </>
            )}
            {props.default !== undefined && props.default !== "" && (
              <>
                <span className={styles.k}>Default</span>
                <span className={`${styles.v} ${styles.mono}`}>{String(props.default)}</span>
              </>
            )}
            <span className={styles.k}>Updated</span>
            <span className={styles.v}>
              {asset.updated_at
                ? new Date(asset.updated_at).toLocaleString()
                : "—"}
            </span>
            <span className={styles.k}>Created</span>
            <span className={styles.v}>
              {asset.created_at
                ? new Date(asset.created_at).toLocaleString()
                : "—"}
            </span>
          </div>
        </div>
      </div>

      <div className={styles.panel}>
        <Subtitle2>Classifications</Subtitle2>
        {asset.tags && asset.tags.length > 0 ? (
          <div className={styles.tagRow}>
            {asset.tags.map((t) => (
              <Tooltip key={t} content={t} relationship="label">
                <Badge appearance="filled" color={tagColor(t)}>
                  {t.replace(/^class:/, "")}
                </Badge>
              </Tooltip>
            ))}
          </div>
        ) : (
          <span className={styles.empty}>None</span>
        )}
        <Caption1 style={{ color: tokens.colorNeutralForeground3 }}>
          PII / PHI / PCI / secret tags are auto-detected during crawl from
          column names. Manual tags merge with auto-classifications.
        </Caption1>
      </div>

      <div className={styles.panel}>
        <Subtitle2>Glossary terms</Subtitle2>
        <AssetTerms assetId={asset.id} />
      </div>
    </div>
  );
}

function AssetTerms({ assetId }: { assetId: string }) {
  const styles = useStyles();
  const qc = useQueryClient();
  const [picking, setPicking] = useState(false);
  const [pickValue, setPickValue] = useState("");
  const [pickedId, setPickedId] = useState("");

  const linked = useQuery({
    queryKey: ["asset-terms", assetId],
    queryFn: () => glossaryApi.termsForAsset(assetId),
    select: (d) => d.terms ?? [],
  });
  const all = useQuery({
    queryKey: ["glossary"],
    queryFn: () => glossaryApi.list(),
    select: (d) => d.terms ?? [],
    enabled: picking,
  });

  const assign = useMutation({
    mutationFn: (termId: string) => glossaryApi.assign(termId, assetId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["asset-terms", assetId] });
      setPicking(false);
      setPickValue("");
      setPickedId("");
    },
  });
  const unassign = useMutation({
    mutationFn: (termId: string) => glossaryApi.unassign(termId, assetId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["asset-terms", assetId] }),
  });

  const linkedIds = new Set((linked.data ?? []).map((t) => t.term_id));
  const candidates = (all.data ?? []).filter((t) => !linkedIds.has(t.id));

  return (
    <>
      <div className={styles.tagRow}>
        {(linked.data ?? []).map((t) => (
          <Tooltip key={t.term_id} content={t.definition || t.name} relationship="label">
            <Badge
              appearance="filled"
              color={t.status === "approved" ? "success" : t.status === "deprecated" ? "danger" : "informative"}
              icon={
                <span
                  onClick={(e) => {
                    e.stopPropagation();
                    unassign.mutate(t.term_id);
                  }}
                  style={{ display: "inline-flex", cursor: "pointer" }}
                >
                  <Dismiss16Regular />
                </span>
              }
            >
              {t.name}
            </Badge>
          </Tooltip>
        ))}
        {!picking && (
          <Button size="small" appearance="subtle" icon={<Add16Regular />} onClick={() => setPicking(true)}>
            Link term
          </Button>
        )}
      </div>
      {picking && (
        <div style={{ display: "flex", gap: 8, marginTop: 8 }}>
          <Combobox
            value={pickValue}
            selectedOptions={pickedId ? [pickedId] : []}
            onOptionSelect={(_, d) => {
              setPickedId(d.optionValue ?? "");
              setPickValue(d.optionText ?? "");
            }}
            onInput={(e) => setPickValue((e.target as HTMLInputElement).value)}
            placeholder="Pick a glossary term…"
            style={{ flex: 1 }}
          >
            {candidates.map((t) => (
              <Option key={t.id} value={t.id} text={t.name}>
                {t.name}
              </Option>
            ))}
          </Combobox>
          <Button
            appearance="primary"
            size="small"
            disabled={!pickedId || assign.isPending}
            onClick={() => assign.mutate(pickedId)}
          >
            Add
          </Button>
          <Button size="small" onClick={() => setPicking(false)}>Cancel</Button>
        </div>
      )}
    </>
  );
}
