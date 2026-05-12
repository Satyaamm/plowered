"use client";

import Link from "next/link";
import { useState } from "react";
import {
  Badge,
  Body1,
  Button,
  Card,
  CardHeader,
  Caption1,
  Dropdown,
  Field,
  InfoLabel,
  Input,
  Option,
  Subtitle2,
  Table,
  TableBody,
  TableCell,
  TableHeader,
  TableHeaderCell,
  TableRow,
  Text,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { Play20Regular } from "@fluentui/react-icons";
import { useAccessPreview, type AccessRow } from "@/lib/hooks";
import { PageHeader } from "@/components/page-header";
import { ErrorBanner } from "@/components/states";

const ROLES = ["", "viewer", "editor", "steward", "admin", "super_admin"];
const VERBS = ["read", "edit", "propose", "certify", "delete", "run", "admin"];

const useStyles = makeStyles({
  panel: {
    backgroundColor: tokens.colorNeutralBackground1,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
    padding: "16px",
    display: "flex",
    flexDirection: "column",
    gap: "12px",
  },
  formRow: {
    display: "grid",
    gridTemplateColumns: "1fr 1fr 1fr 1fr auto",
    gap: "12px",
    alignItems: "end",
  },
  cardsRow: {
    display: "grid",
    gridTemplateColumns: "1fr 1fr",
    gap: "16px",
    "@media (max-width: 1024px)": { gridTemplateColumns: "1fr" },
  },
  count: {
    fontSize: "11px",
    color: tokens.colorNeutralForeground3,
  },
  mono: { fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace", fontSize: "12px" },
  reason: { color: tokens.colorNeutralForeground3, fontSize: "11px" },
});

export default function AccessPage() {
  const styles = useStyles();
  const [role, setRole] = useState("viewer");
  const [groups, setGroups] = useState("");
  const [email, setEmail] = useState("");
  const [verb, setVerb] = useState("read");

  const preview = useAccessPreview();

  const run = () =>
    preview.mutate({
      role,
      verb,
      email: email.trim() || undefined,
      groups: groups
        .split(",")
        .map((g) => g.trim())
        .filter(Boolean),
    });

  const data = preview.data;

  return (
    <>
      <PageHeader
        title="Access viewer"
        subtitle="Pick a principal — see exactly which assets they can read, and why."
        crumbs={[{ label: "Home", href: "/" }, { label: "Governance" }, { label: "Access" }]}
      />

      <div className={styles.panel}>
        <div className={styles.formRow}>
          <Field
            label={
              <InfoLabel info="Synthetic role to simulate. Used together with Groups to model a principal who doesn't exist yet. Leave blank to test a no-role visitor.">
                Role
              </InfoLabel>
            }
          >
            <Dropdown
              value={role || "(no role)"}
              selectedOptions={[role]}
              onOptionSelect={(_, d) => setRole(d.optionValue ?? "")}
            >
              {ROLES.map((r) => (
                <Option key={r || "none"} value={r} text={r || "(no role)"}>
                  {r || "(no role)"}
                </Option>
              ))}
            </Dropdown>
          </Field>
          <Field
            label={
              <InfoLabel info="Comma-separated group names the simulated principal belongs to (e.g. 'finance, eu-team'). Combined with the Role to match group-scoped allow/deny rules from the Policies page.">
                Groups (comma-separated)
              </InfoLabel>
            }
          >
            <Input value={groups} onChange={(_, d) => setGroups(d.value)} placeholder="finance, eu-team" />
          </Field>
          <Field
            label={
              <InfoLabel info="Pick an existing user to test access exactly as they would experience it — Plowered resolves their real roles + groups + tenant membership. Overrides the Role and Groups fields above.">
                Or impersonate by email
              </InfoLabel>
            }
          >
            <Input value={email} onChange={(_, d) => setEmail(d.value)} placeholder="alice@example.com" />
          </Field>
          <Field
            label={
              <InfoLabel info="The action you want to test. read = can they see it; edit = mutate; propose = suggest a change; certify = mark trusted; delete = soft-delete; run = trigger pipelines; admin = manage policies.">
                Verb
              </InfoLabel>
            }
          >
            <Dropdown
              value={verb}
              selectedOptions={[verb]}
              onOptionSelect={(_, d) => setVerb(d.optionValue ?? "read")}
            >
              {VERBS.map((v) => (
                <Option key={v} value={v} text={v}>{v}</Option>
              ))}
            </Dropdown>
          </Field>
          <Button
            appearance="primary"
            icon={<Play20Regular />}
            onClick={run}
            disabled={preview.isPending}
          >
            {preview.isPending ? "Running…" : "Preview"}
          </Button>
        </div>
        <Caption1 style={{ color: tokens.colorNeutralForeground3 }}>
          Walks up to 500 assets in the catalog and runs the policy engine
          for the principal you describe. Add tag-based deny rules on the
          Policies page to see them filter the visible set live here.
        </Caption1>
      </div>

      {preview.error && <ErrorBanner error={preview.error} />}

      {data && (
        <>
          <div className={styles.panel}>
            <Subtitle2>Principal</Subtitle2>
            <div style={{ display: "flex", gap: 12, flexWrap: "wrap" }}>
              {data.principal.email && <Text>{data.principal.email}</Text>}
              {data.principal.roles?.length ? (
                data.principal.roles.map((r) => (
                  <Badge key={r} appearance="filled" color="brand">{r}</Badge>
                ))
              ) : (
                <Text className={styles.reason}>(no roles)</Text>
              )}
              {data.principal.groups?.map((g) => (
                <Badge key={g} appearance="outline" color="informative">{g}</Badge>
              ))}
            </div>
          </div>

          <div className={styles.cardsRow}>
            <Card>
              <CardHeader
                header={<Subtitle2>Visible · {data.visible.length} / {data.total}</Subtitle2>}
              />
              <ResultTable rows={data.visible} variant="visible" />
            </Card>
            <Card>
              <CardHeader
                header={<Subtitle2>Denied · {data.denied.length} / {data.total}</Subtitle2>}
              />
              <ResultTable rows={data.denied} variant="denied" />
            </Card>
          </div>
        </>
      )}
    </>
  );
}

function ResultTable({
  rows,
  variant,
}: {
  rows: AccessRow[];
  variant: "visible" | "denied";
}) {
  const styles = useStyles();
  if (rows.length === 0) {
    return (
      <Body1 style={{ padding: "0 16px 16px", color: tokens.colorNeutralForeground3 }}>
        {variant === "visible" ? "Nothing visible." : "Nothing denied — full catalog access."}
      </Body1>
    );
  }
  return (
    <Table aria-label={`${variant} assets`}>
      <TableHeader>
        <TableRow>
          <TableHeaderCell>Asset</TableHeaderCell>
          <TableHeaderCell>Type</TableHeaderCell>
          <TableHeaderCell>Tags</TableHeaderCell>
          <TableHeaderCell>Reason</TableHeaderCell>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.slice(0, 100).map((r) => (
          <TableRow key={r.asset_id}>
            <TableCell className={styles.mono}>
              <Link
                href={`/asset/${encodeURIComponent(r.qualified_name)}`}
                style={{ color: tokens.colorBrandForeground1 }}
              >
                {r.qualified_name}
              </Link>
            </TableCell>
            <TableCell>
              <Badge appearance="outline" color="brand">{r.type || "?"}</Badge>
            </TableCell>
            <TableCell>
              <div style={{ display: "flex", gap: 4, flexWrap: "wrap" }}>
                {(r.tags ?? []).slice(0, 3).map((t) => (
                  <Badge
                    key={t}
                    appearance="outline"
                    color={t.startsWith("class:pci") ? "danger" : t.startsWith("class:pii") ? "warning" : "subtle"}
                  >
                    {t.replace(/^class:/, "")}
                  </Badge>
                ))}
                {(r.tags ?? []).length > 3 && (
                  <span className={styles.reason}>+{(r.tags ?? []).length - 3}</span>
                )}
              </div>
            </TableCell>
            <TableCell>
              <Text className={styles.reason}>{r.reason}</Text>
            </TableCell>
          </TableRow>
        ))}
        {rows.length > 100 && (
          <TableRow>
            <TableCell colSpan={4}>
              <Text className={styles.reason}>… and {rows.length - 100} more</Text>
            </TableCell>
          </TableRow>
        )}
      </TableBody>
    </Table>
  );
}
