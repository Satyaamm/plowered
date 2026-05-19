"use client";

import {
  Badge,
  Body1,
  Button,
  Caption1,
  Combobox,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  Option,
  Spinner,
  Subtitle2,
  Text,
  Tooltip,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  Add16Regular,
  Dismiss16Regular,
  Sparkle20Regular,
  ShieldCheckmark16Regular,
} from "@fluentui/react-icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { glossaryApi } from "@/lib/api";
import {
  useDescribeAsset,
  useMembers,
  useUpdateAsset,
  useUpdateAssetOwners,
} from "@/lib/hooks";
import {
  Person16Regular,
  Edit16Regular,
} from "@fluentui/react-icons";
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
  panelHeader: { display: "flex", alignItems: "center", justifyContent: "space-between" },
  aiNote: {
    backgroundColor: tokens.colorNeutralBackground2,
    borderRadius: "6px",
    padding: "10px 12px",
    display: "flex",
    flexDirection: "column",
    gap: "4px",
  },
  aiNoteMeta: { color: tokens.colorNeutralForeground3, fontSize: "11px" },
  dialogBody: { display: "flex", flexDirection: "column", gap: "10px" },
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
  // AI describe works on tables, views, AND columns — the describer
  // service falls back to BuildForColumn (parent-table context) when
  // given a column asset id.
  const canDescribe = ["table", "view", "column"].includes(asset.type ?? "");

  return (
    <div className={styles.body}>
      <div className={styles.twoCol}>
        <div className={styles.panel}>
          <div className={styles.panelHeader}>
            <Subtitle2>Description</Subtitle2>
            {canDescribe && <AISuggestButton assetId={asset.id} />}
          </div>
          {asset.description ? (
            <Body1>{asset.description}</Body1>
          ) : (
            <span className={styles.empty}>
              No description yet. Add one from the connection, via API, or use Suggest with AI.
            </span>
          )}
          {/* Show the AI-generated description separately so it's clear which is human vs auto */}
          {(asset as any).description_ai && (
            <div className={styles.aiNote}>
              <Caption1 className={styles.aiNoteMeta}>
                <Sparkle20Regular style={{ width: 12, height: 12, verticalAlign: "middle" }} />
                {" "}AI-generated
              </Caption1>
              <Body1>{(asset as any).description_ai}</Body1>
            </div>
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
            <span className={styles.k}>Owners</span>
            <span className={styles.v}>
              <OwnerPicker assetId={asset.id} owners={asset.owners ?? []} />
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
    meta: { successMessage: "Term linked" },
  });
  const unassign = useMutation({
    mutationFn: (termId: string) => glossaryApi.unassign(termId, assetId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["asset-terms", assetId] }),
    meta: { successMessage: "Term unlinked" },
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

// AISuggestButton is the "Suggest with AI" trigger + review dialog.
// The mutation is silent (the dialog IS the feedback); accept fires a
// noisy toast via useUpdateAsset.
// OwnerPicker shows the current owners as inline chips and pops a
// small dialog to add/remove. The Members list is fetched once and
// cached — owners are stored by user_id but rendered by display name
// + email for legibility.
function OwnerPicker({ assetId, owners }: { assetId: string; owners: string[] }) {
  const members = useMembers();
  const update = useUpdateAssetOwners(assetId);
  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState<string[]>(owners);

  // Reset to server truth when the dialog opens — covers the case
  // where someone else changed owners between this user's edits.
  const onOpenChange = (next: boolean) => {
    if (next) setSelected(owners);
    setOpen(next);
  };

  const byId = new Map(
    (members.data ?? []).map((m) => [m.user_id, m]),
  );
  const ownerLabels = owners.map((id) => {
    const m = byId.get(id);
    return m?.full_name || m?.email || id;
  });

  const onSave = async () => {
    await update.mutateAsync(selected);
    setOpen(false);
  };

  return (
    <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
      {ownerLabels.length === 0 ? (
        <Text style={{ color: tokens.colorNeutralForeground3, fontStyle: "italic" }}>
          unassigned
        </Text>
      ) : (
        ownerLabels.map((name, i) => (
          <Badge key={i} appearance="tint" color="brand" icon={<Person16Regular />}>
            {name}
          </Badge>
        ))
      )}
      <Dialog open={open} onOpenChange={(_, d) => onOpenChange(d.open)}>
        <Button
          size="small"
          appearance="subtle"
          icon={<Edit16Regular />}
          onClick={() => onOpenChange(true)}
        >
          Edit
        </Button>
        <DialogSurface>
          <DialogBody>
            <DialogTitle>Set owners</DialogTitle>
            <DialogContent>
              <div style={{ display: "flex", flexDirection: "column", gap: 8, minWidth: 360 }}>
                <Text style={{ color: tokens.colorNeutralForeground3, fontSize: 12 }}>
                  Owners are accountable for the asset. Policies and alerts use this list as the escalation target.
                </Text>
                {members.isLoading && <Spinner size="extra-tiny" />}
                {(members.data ?? []).map((m) => {
                  const checked = selected.includes(m.user_id);
                  return (
                    <label
                      key={m.user_id}
                      style={{ display: "flex", alignItems: "center", gap: 8, cursor: "pointer" }}
                    >
                      <input
                        type="checkbox"
                        checked={checked}
                        onChange={(e) => {
                          if (e.target.checked) {
                            setSelected((prev) => Array.from(new Set([...prev, m.user_id])));
                          } else {
                            setSelected((prev) => prev.filter((id) => id !== m.user_id));
                          }
                        }}
                      />
                      <Text>
                        {m.full_name || m.email}
                        {m.full_name && (
                          <Caption1 style={{ color: tokens.colorNeutralForeground3 }}>
                            {" "}· {m.email}
                          </Caption1>
                        )}
                      </Text>
                    </label>
                  );
                })}
              </div>
            </DialogContent>
            <DialogActions>
              <Button onClick={() => setOpen(false)}>Cancel</Button>
              <Button
                appearance="primary"
                onClick={onSave}
                disabled={update.isPending}
              >
                {update.isPending ? <Spinner size="extra-tiny" /> : "Save"}
              </Button>
            </DialogActions>
          </DialogBody>
        </DialogSurface>
      </Dialog>
    </div>
  );
}

function AISuggestButton({ assetId }: { assetId: string }) {
  const styles = useStyles();
  const [open, setOpen] = useState(false);
  const describe = useDescribeAsset(assetId);
  const update = useUpdateAsset(assetId);

  const onTrigger = async () => {
    setOpen(true);
    if (!describe.data) {
      try {
        await describe.mutateAsync();
      } catch {
        // error rendered inline in the dialog
      }
    }
  };

  const onAccept = async () => {
    if (!describe.data) return;
    await update.mutateAsync({ description_ai: describe.data.suggestion });
    setOpen(false);
    describe.reset();
  };

  const onDiscard = () => {
    setOpen(false);
    describe.reset();
  };

  return (
    <>
      <Button
        size="small"
        appearance="subtle"
        icon={<Sparkle20Regular />}
        onClick={onTrigger}
        disabled={describe.isPending}
      >
        Suggest with AI
      </Button>
      <Dialog open={open} onOpenChange={(_, d) => setOpen(d.open)}>
        <DialogSurface>
          <DialogBody>
            <DialogTitle>AI-generated description</DialogTitle>
            <DialogContent>
              <div className={styles.dialogBody}>
                {describe.isPending && (
                  <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
                    <Spinner size="extra-tiny" />
                    <Text>Generating description…</Text>
                  </div>
                )}
                {describe.error && (
                  <Text style={{ color: tokens.colorPaletteRedForeground1 }}>
                    {(describe.error as Error).message}
                  </Text>
                )}
                {describe.data && (
                  <>
                    <Body1>{describe.data.suggestion}</Body1>
                    <Caption1 className={styles.aiNoteMeta}>
                      {describe.data.model} ·{" "}
                      {describe.data.input_tokens + describe.data.output_tokens} tokens
                    </Caption1>
                  </>
                )}
              </div>
            </DialogContent>
            <DialogActions>
              <Button onClick={onDiscard}>Discard</Button>
              <Button
                appearance="secondary"
                onClick={() => describe.mutate()}
                disabled={describe.isPending}
              >
                Regenerate
              </Button>
              <Button
                appearance="primary"
                onClick={onAccept}
                disabled={!describe.data || update.isPending}
              >
                {update.isPending ? <Spinner size="extra-tiny" /> : "Save"}
              </Button>
            </DialogActions>
          </DialogBody>
        </DialogSurface>
      </Dialog>
    </>
  );
}
