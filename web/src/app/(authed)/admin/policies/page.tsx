"use client";

import { useState } from "react";
import {
  Button,
  Card,
  CardHeader,
  Checkbox,
  Combobox,
  Dropdown,
  Field,
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
import { Delete16Regular } from "@fluentui/react-icons";
import {
  useCreatePolicy,
  useDeletePolicy,
  usePolicies,
} from "@/lib/hooks";
import { PageHeader } from "@/components/page-header";
import type {
  ConditionType,
  PolicyEffect,
  PolicyVerb,
} from "@/lib/types-orchestration";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";
import { InfoLabel } from "@/components/info-label";

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "24px" },
  twoCol: {
    display: "grid",
    gridTemplateColumns: "minmax(280px, 360px) 1fr",
    gap: "20px",
  },
  form: { display: "grid", gap: "12px" },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  effectAllow: { color: "#1F6334", fontWeight: 600 },
  effectDeny: { color: "#8E1B1B", fontWeight: 600 },
});

const ALL_VERBS: PolicyVerb[] = [
  "read",
  "edit",
  "propose",
  "certify",
  "delete",
  "run",
  "admin",
];

const CONDITION_TYPES: { value: ConditionType; label: string; placeholder: string }[] = [
  { value: "principal.role", label: "Principal role", placeholder: "viewer" },
  { value: "principal.group", label: "Principal group", placeholder: "data-team" },
  { value: "resource.tag", label: "Resource tag", placeholder: "class:pii" },
  { value: "resource.owner", label: "Resource owner", placeholder: "self" },
];

export default function PoliciesPage() {
  const styles = useStyles();
  const policies = usePolicies();
  const create = useCreatePolicy();
  const remove = useDeletePolicy();

  const [effect, setEffect] = useState<PolicyEffect>("allow");
  const [verbs, setVerbs] = useState<PolicyVerb[]>(["read"]);
  const [condType, setCondType] = useState<ConditionType>("principal.role");
  const [condValue, setCondValue] = useState("");
  const [submitError, setSubmitError] = useState<string | null>(null);

  function toggleVerb(v: PolicyVerb) {
    setVerbs((prev) =>
      prev.includes(v) ? prev.filter((x) => x !== v) : [...prev, v],
    );
  }

  async function onCreate() {
    setSubmitError(null);
    if (verbs.length === 0) {
      setSubmitError("Pick at least one verb.");
      return;
    }
    try {
      await create.mutateAsync({
        Effect: effect,
        Verbs: verbs,
        Conditions: condValue
          ? [{ Type: condType, Value: condValue }]
          : [],
      });
      setCondValue("");
      setVerbs(["read"]);
    } catch (e) {
      setSubmitError(e instanceof Error ? e.message : String(e));
    }
  }

  return (
    <div className={styles.root}>
      <PageHeader crumbs={[{ label: "Policies" }]} />

      <div className={styles.twoCol}>
        <Card>
          <CardHeader header={<Subtitle2>Add a rule</Subtitle2>} />
          <div className={styles.form}>
            <Field
              label={
                <InfoLabel info="allow grants the verbs to whoever matches the conditions; deny revokes them. Deny rules always win over allow rules when both match the same request.">
                  Effect
                </InfoLabel>
              }
            >
              <Dropdown
                selectedOptions={[effect]}
                value={effect}
                onOptionSelect={(_, d) => {
                  if (d.optionValue) setEffect(d.optionValue as PolicyEffect);
                }}
              >
                <Option value="allow">allow</Option>
                <Option value="deny">deny</Option>
              </Dropdown>
            </Field>

            <Field
              label={
                <InfoLabel info="The actions this rule applies to. read = view metadata + lineage; edit = mutate; propose = suggest a change pending approval; certify = mark trusted; delete = soft-delete; run = trigger pipelines/checks; admin = manage policies and members.">
                  Verbs
                </InfoLabel>
              }
            >
              <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
                {ALL_VERBS.map((v) => (
                  <Checkbox
                    key={v}
                    label={v}
                    checked={verbs.includes(v)}
                    onChange={() => toggleVerb(v)}
                  />
                ))}
              </div>
            </Field>

            <Field
              label={
                <InfoLabel info="Restricts who or what the rule applies to. principal.role/group narrows by identity; resource.tag/owner narrows by target. Leave empty to apply to everyone in the workspace.">
                  Condition (optional)
                </InfoLabel>
              }
            >
              <Combobox
                selectedOptions={[condType]}
                value={condType}
                onOptionSelect={(_, d) => {
                  if (d.optionValue) setCondType(d.optionValue as ConditionType);
                }}
              >
                {CONDITION_TYPES.map((c) => (
                  <Option key={c.value} value={c.value}>
                    {c.label}
                  </Option>
                ))}
              </Combobox>
              <Input
                value={condValue}
                onChange={(_, d) => setCondValue(d.value)}
                placeholder={
                  CONDITION_TYPES.find((c) => c.value === condType)?.placeholder
                }
                style={{ marginTop: 8 }}
              />
            </Field>

            {submitError && <ErrorBanner error={submitError} />}
            {create.error && <ErrorBanner error={create.error} />}

            <Button
              appearance="primary"
              onClick={onCreate}
              disabled={create.isPending}
            >
              {create.isPending ? "Saving…" : "Add rule"}
            </Button>
          </div>
        </Card>

        <Card>
          <CardHeader header={<Subtitle2>Existing rules</Subtitle2>} />
          {policies.isLoading && <LoadingState />}
          {policies.error && <ErrorBanner error={policies.error} />}
          {policies.data && policies.data.length === 0 && (
            <EmptyState
              title="No rules yet"
              body="Roles cover the common cases. Add a rule when you need finer control."
            />
          )}
          {policies.data && policies.data.length > 0 && (
            <Table aria-label="Policies">
              <TableHeader>
                <TableRow>
                  <TableHeaderCell>Effect</TableHeaderCell>
                  <TableHeaderCell>Verbs</TableHeaderCell>
                  <TableHeaderCell>Conditions</TableHeaderCell>
                  <TableHeaderCell>Actions</TableHeaderCell>
                </TableRow>
              </TableHeader>
              <TableBody>
                {policies.data.map((r) => (
                  <TableRow key={r.ID}>
                    <TableCell
                      className={
                        r.Effect === "allow" ? styles.effectAllow : styles.effectDeny
                      }
                    >
                      {r.Effect}
                    </TableCell>
                    <TableCell>{r.Verbs.join(", ")}</TableCell>
                    <TableCell>
                      {(r.Conditions ?? []).map((c) => (
                        <div key={`${c.Type}:${c.Value}`}>
                          <Text className={styles.meta}>
                            {c.Type} = {c.Value}
                          </Text>
                        </div>
                      ))}
                      {(!r.Conditions || r.Conditions.length === 0) && (
                        <Text className={styles.meta}>—</Text>
                      )}
                    </TableCell>
                    <TableCell>
                      <Button
                        size="small"
                        appearance="subtle"
                        icon={<Delete16Regular />}
                        onClick={() => remove.mutate(r.ID)}
                        disabled={remove.isPending}
                      >
                        Delete
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </Card>
      </div>
    </div>
  );
}
