"use client";

import { useMemo, useState } from "react";
import {
  Badge,
  Body1,
  Button,
  Card,
  CardHeader,
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
  Subtitle2,
  Text,
  Textarea,
  Title3,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  Add20Regular,
  ChevronDown16Regular,
  ChevronRight16Regular,
  Delete20Regular,
  Dismiss24Regular,
  Edit20Regular,
} from "@fluentui/react-icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { glossaryApi, type GlossaryTerm } from "@/lib/api";
import { PageHeader } from "@/components/page-header";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";

const STATUS_COLOR: Record<string, "subtle" | "warning" | "success" | "danger"> = {
  draft: "subtle",
  approved: "success",
  deprecated: "danger",
};

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "20px" },
  layout: { display: "grid", gridTemplateColumns: "320px 1fr", gap: "16px", minHeight: "560px" },
  treeCard: { padding: "8px 0" },
  treeRow: {
    display: "flex",
    alignItems: "center",
    gap: "6px",
    padding: "6px 12px",
    cursor: "pointer",
    fontSize: "13px",
    "&:hover": { backgroundColor: tokens.colorNeutralBackground2 },
  },
  treeRowSelected: { backgroundColor: tokens.colorBrandBackground2 },
  treeRowName: { flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" },
  detail: { display: "flex", flexDirection: "column", gap: "16px" },
  formGrid: { display: "grid", gridTemplateColumns: "1fr 1fr", gap: "16px" },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  drawerBody: { display: "flex", flexDirection: "column", gap: "16px", padding: "16px 0" },
  drawerFooter: {
    display: "flex",
    gap: "8px",
    justifyContent: "flex-end",
    paddingTop: "12px",
    borderTop: `1px solid ${tokens.colorNeutralStroke2}`,
    marginTop: "auto",
  },
});

interface TreeNode {
  term: GlossaryTerm;
  children: TreeNode[];
  depth: number;
}

function buildTree(terms: GlossaryTerm[]): TreeNode[] {
  const byId = new Map<string, TreeNode>();
  terms.forEach((t) => byId.set(t.id, { term: t, children: [], depth: 0 }));
  const roots: TreeNode[] = [];
  byId.forEach((n) => {
    if (n.term.parent_id && byId.has(n.term.parent_id)) {
      const parent = byId.get(n.term.parent_id)!;
      parent.children.push(n);
      n.depth = parent.depth + 1;
    } else {
      roots.push(n);
    }
  });
  // Recompute depth on second pass so deep trees stay consistent.
  const fix = (n: TreeNode, d: number) => {
    n.depth = d;
    n.children.forEach((c) => fix(c, d + 1));
  };
  roots.forEach((r) => fix(r, 0));
  const sortRec = (ns: TreeNode[]) => {
    ns.sort((a, b) => a.term.name.localeCompare(b.term.name));
    ns.forEach((n) => sortRec(n.children));
  };
  sortRec(roots);
  return roots;
}

export default function GlossaryPage() {
  const styles = useStyles();
  const qc = useQueryClient();
  const list = useQuery({
    queryKey: ["glossary"],
    queryFn: () => glossaryApi.list(),
    select: (d) => d.terms ?? [],
  });

  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editing, setEditing] = useState<GlossaryTerm | null>(null);

  const terms = list.data ?? [];
  const tree = useMemo(() => buildTree(terms), [terms]);
  const selected = terms.find((t) => t.id === selectedId) ?? null;

  const del = useMutation({
    mutationFn: (id: string) => glossaryApi.remove(id),
    onSuccess: () => {
      setSelectedId(null);
      qc.invalidateQueries({ queryKey: ["glossary"] });
    },
  });

  const renderTree = (nodes: TreeNode[]): React.ReactNode =>
    nodes.map((n) => {
      const hasChildren = n.children.length > 0;
      const open = expanded.has(n.term.id);
      return (
        <div key={n.term.id}>
          <div
            className={`${styles.treeRow} ${n.term.id === selectedId ? styles.treeRowSelected : ""}`}
            style={{ paddingLeft: 8 + n.depth * 16 }}
            onClick={() => setSelectedId(n.term.id)}
          >
            {hasChildren ? (
              <span
                onClick={(e) => {
                  e.stopPropagation();
                  setExpanded((prev) => {
                    const next = new Set(prev);
                    if (next.has(n.term.id)) next.delete(n.term.id);
                    else next.add(n.term.id);
                    return next;
                  });
                }}
                style={{ display: "inline-flex", width: 16 }}
              >
                {open ? <ChevronDown16Regular /> : <ChevronRight16Regular />}
              </span>
            ) : (
              <span style={{ width: 16 }} />
            )}
            <span className={styles.treeRowName}>{n.term.name}</span>
            <Badge appearance="outline" color={STATUS_COLOR[n.term.status] ?? "subtle"}>
              {n.term.status}
            </Badge>
          </div>
          {hasChildren && open && renderTree(n.children)}
        </div>
      );
    });

  return (
    <>
      <PageHeader
        title="Business glossary"
        subtitle="Authoritative business definitions linked to physical assets."
        crumbs={[{ label: "Home", href: "/" }, { label: "Governance" }, { label: "Glossary" }]}
        actions={
          <Button
            appearance="primary"
            icon={<Add20Regular />}
            onClick={() => {
              setEditing(null);
              setDrawerOpen(true);
            }}
          >
            New term
          </Button>
        }
      />

      {list.isLoading && <LoadingState />}
      {list.error && <ErrorBanner error={list.error} />}
      {list.data && list.data.length === 0 && (
        <EmptyState
          title="No glossary terms yet"
          body="Define a business concept (e.g., Customer, Order, MRR) and link it to the catalog assets that implement it."
          action={
            <Button
              appearance="primary"
              icon={<Add20Regular />}
              onClick={() => {
                setEditing(null);
                setDrawerOpen(true);
              }}
            >
              New term
            </Button>
          }
        />
      )}

      {list.data && list.data.length > 0 && (
        <div className={styles.layout}>
          <Card className={styles.treeCard}>
            <CardHeader header={<Subtitle2>Terms</Subtitle2>} />
            <div>{renderTree(tree)}</div>
          </Card>

          <div className={styles.detail}>
            {!selected ? (
              <Card>
                <Body1 style={{ padding: 16 }}>Select a term on the left to see its details.</Body1>
              </Card>
            ) : (
              <Card>
                <CardHeader
                  header={<Subtitle2>{selected.name}</Subtitle2>}
                  action={
                    <div style={{ display: "flex", gap: 8 }}>
                      <Button
                        size="small"
                        icon={<Edit20Regular />}
                        onClick={() => {
                          setEditing(selected);
                          setDrawerOpen(true);
                        }}
                      >
                        Edit
                      </Button>
                      <Button
                        size="small"
                        icon={<Delete20Regular />}
                        onClick={() => {
                          if (confirm(`Delete term "${selected.name}"?`)) del.mutate(selected.id);
                        }}
                      >
                        Delete
                      </Button>
                    </div>
                  }
                />
                <div style={{ display: "flex", flexDirection: "column", gap: 12, padding: "0 16px 16px" }}>
                  <div style={{ display: "flex", gap: 12, alignItems: "center" }}>
                    <Badge appearance="filled" color={STATUS_COLOR[selected.status] ?? "subtle"}>
                      {selected.status}
                    </Badge>
                    <Text className={styles.meta}>
                      Updated {new Date(selected.updated_at).toLocaleString()}
                    </Text>
                  </div>
                  <Text>{selected.definition || "No definition yet."}</Text>
                </div>
              </Card>
            )}

            {selected && (
              <Card>
                <CardHeader header={<Subtitle2>Linked assets</Subtitle2>} />
                <LinkedAssets termId={selected.id} />
              </Card>
            )}
          </div>
        </div>
      )}

      <TermDrawer
        open={drawerOpen}
        onClose={() => setDrawerOpen(false)}
        existing={editing}
        terms={terms}
      />
    </>
  );
}

function LinkedAssets({ termId }: { termId: string }) {
  const styles = useStyles();
  const q = useQuery({
    queryKey: ["glossary-assets", termId],
    queryFn: () => glossaryApi.assetsForTerm(termId),
    select: (d) => d.asset_ids ?? [],
  });
  if (q.isLoading) return <LoadingState />;
  if (q.error) return <ErrorBanner error={q.error} />;
  const ids = q.data ?? [];
  if (ids.length === 0)
    return (
      <Body1 style={{ padding: 16 }} className={styles.meta}>
        No assets linked yet. Open an asset and add this term from its detail page.
      </Body1>
    );
  return (
    <div style={{ padding: "0 16px 16px", display: "flex", flexDirection: "column", gap: 4 }}>
      {ids.map((id) => (
        <Text key={id} className={styles.meta} style={{ fontFamily: "ui-monospace, monospace" }}>
          {id}
        </Text>
      ))}
    </div>
  );
}

function TermDrawer({
  open,
  onClose,
  existing,
  terms,
}: {
  open: boolean;
  onClose: () => void;
  existing: GlossaryTerm | null;
  terms: GlossaryTerm[];
}) {
  const styles = useStyles();
  const qc = useQueryClient();

  const [name, setName] = useState(existing?.name ?? "");
  const [definition, setDefinition] = useState(existing?.definition ?? "");
  const [parentId, setParentId] = useState(existing?.parent_id ?? "");
  const [status, setStatus] = useState<string>(existing?.status ?? "draft");

  // Reset state when (re)opened.
  useMemo(() => {
    if (open) {
      setName(existing?.name ?? "");
      setDefinition(existing?.definition ?? "");
      setParentId(existing?.parent_id ?? "");
      setStatus(existing?.status ?? "draft");
    }
  }, [open, existing]);

  const create = useMutation({
    mutationFn: (t: Partial<GlossaryTerm>) => glossaryApi.create(t),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["glossary"] });
      onClose();
    },
  });
  const update = useMutation({
    mutationFn: (t: Partial<GlossaryTerm>) => glossaryApi.update(existing!.id, t),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["glossary"] });
      onClose();
    },
  });

  const submit = () => {
    const payload: Partial<GlossaryTerm> = {
      name,
      definition,
      parent_id: parentId || undefined,
      status: status as GlossaryTerm["status"],
    };
    if (existing) update.mutate(payload);
    else create.mutate(payload);
  };

  const err = create.error ?? update.error;
  const pending = create.isPending || update.isPending;
  const valid = name.trim().length > 0;
  const parentOptions = terms.filter((t) => !existing || t.id !== existing.id);

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
          action={<Button appearance="subtle" icon={<Dismiss24Regular />} onClick={onClose} />}
        >
          <Title3>{existing ? "Edit term" : "New term"}</Title3>
        </DrawerHeaderTitle>
      </DrawerHeader>
      <DrawerBody>
        <div className={styles.drawerBody}>
          <Field label="Name" required>
            <Input value={name} onChange={(_, d) => setName(d.value)} placeholder="e.g. Customer" />
          </Field>
          <Field label="Definition">
            <Textarea
              rows={5}
              value={definition}
              onChange={(_, d) => setDefinition(d.value)}
              placeholder="A person or organization that has placed at least one paid order…"
            />
          </Field>
          <div className={styles.formGrid}>
            <Field label="Parent term">
              <Dropdown
                value={parentOptions.find((t) => t.id === parentId)?.name ?? "(none)"}
                selectedOptions={parentId ? [parentId] : ["__none__"]}
                onOptionSelect={(_, d) =>
                  setParentId(d.optionValue === "__none__" ? "" : d.optionValue ?? "")
                }
              >
                <Option key="__none__" value="__none__" text="(none)">
                  (none)
                </Option>
                {parentOptions.map((t) => (
                  <Option key={t.id} value={t.id} text={t.name}>
                    {t.name}
                  </Option>
                ))}
              </Dropdown>
            </Field>
            <Field label="Status">
              <Dropdown
                value={status}
                selectedOptions={[status]}
                onOptionSelect={(_, d) => setStatus(d.optionValue ?? "draft")}
              >
                <Option value="draft" text="draft">draft</Option>
                <Option value="approved" text="approved">approved</Option>
                <Option value="deprecated" text="deprecated">deprecated</Option>
              </Dropdown>
            </Field>
          </div>
          {err && (
            <MessageBar intent="error">
              <MessageBarBody>{(err as Error).message}</MessageBarBody>
            </MessageBar>
          )}
          <div className={styles.drawerFooter}>
            <Button onClick={onClose} disabled={pending}>Cancel</Button>
            <Button appearance="primary" onClick={submit} disabled={!valid || pending}>
              {pending ? "Saving…" : existing ? "Save" : "Create"}
            </Button>
          </div>
        </div>
      </DrawerBody>
    </Drawer>
  );
}
