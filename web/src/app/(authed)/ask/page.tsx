"use client";

import Link from "next/link";
import { useState } from "react";
import {
  Badge,
  Body1,
  Button,
  Caption1,
  Card,
  Dropdown,
  Field,
  MessageBar,
  MessageBarBody,
  Option,
  Spinner,
  Subtitle1,
  Subtitle2,
  Textarea,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  Play20Regular,
  Sparkle20Regular,
} from "@fluentui/react-icons";
import { PageHeader } from "@/components/page-header";
import { PageIntro } from "@/components/page-intro";
import {
  useAIProviders,
  useAskGenerate,
  useAskRun,
  useConnections,
} from "@/lib/hooks";

// QUESTION_MAX caps the natural-language prompt sent to the LLM. Above
// this the model becomes both expensive (tokens) and noisy (worse
// schema grounding). The hard backend limit matches.
const QUESTION_MAX = 500;

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "16px" },
  panel: {
    backgroundColor: tokens.colorNeutralBackground1,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
    padding: "16px",
    display: "flex",
    flexDirection: "column",
    gap: "14px",
  },
  // Vertical stack — each labeled field sits on its own row. Fluent's
  // Field already renders label + control vertically; we just make sure
  // the parent doesn't reflow horizontally.
  fieldStack: {
    display: "flex",
    flexDirection: "column",
    gap: "14px",
  },
  actionRow: {
    display: "flex",
    justifyContent: "flex-end",
    gap: "8px",
  },
  counterRow: {
    display: "flex",
    justifyContent: "space-between",
    alignItems: "center",
    gap: "8px",
  },
  counter: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  counterWarn: { color: tokens.colorPaletteRedForeground1, fontSize: "12px" },
  sqlBox: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: "13px",
    backgroundColor: tokens.colorNeutralBackground3,
    padding: "12px 14px",
    borderRadius: "6px",
    whiteSpace: "pre-wrap",
    overflowX: "auto",
    color: tokens.colorNeutralForeground1,
  },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  resultMeta: {
    display: "flex",
    alignItems: "center",
    gap: "12px",
    flexWrap: "wrap",
  },
  table: {
    width: "100%",
    borderCollapse: "collapse",
    fontSize: "13px",
  },
  th: {
    textAlign: "left",
    padding: "8px 10px",
    backgroundColor: tokens.colorNeutralBackground2,
    borderBottom: `1px solid ${tokens.colorNeutralStroke2}`,
    fontWeight: 600,
    whiteSpace: "nowrap",
  },
  td: {
    padding: "8px 10px",
    borderBottom: `1px solid ${tokens.colorNeutralStroke3}`,
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    whiteSpace: "nowrap",
    maxWidth: "320px",
    overflow: "hidden",
    textOverflow: "ellipsis",
  },
  scrollX: { overflowX: "auto" },
});

export default function AskPage() {
  const styles = useStyles();
  const conns = useConnections();
  const providers = useAIProviders();
  const [connectionId, setConnectionId] = useState<string>("");
  const [question, setQuestion] = useState<string>("");

  const generate = useAskGenerate();
  const executionId = generate.data?.execution_id ?? null;
  const run = useAskRun(executionId);

  // Preflight: /ask is useless without a primary chat provider. Detect
  // up-front so the user gets a clear "set this up first" message
  // instead of clicking Draft SQL and being told the request failed.
  const chatProvider = (providers.data ?? []).find(
    (p) => p.capability === "chat" && p.is_primary,
  );
  const llmReady = !!chatProvider;
  const llmLoading = providers.isLoading;

  // Only SQL-capable connections are valid targets. The backend will
  // reject document sources with a 400 — we filter client-side so the
  // dropdown only shows real options.
  const sqlConnections = (conns.data ?? []).filter((c) =>
    ["postgres", "snowflake", "mysql", "redshift", "bigquery", "athena", "databricks"]
      .includes(c.type),
  );

  const trimmed = question.trim();
  const charsUsed = question.length;
  const charsOver = charsUsed > QUESTION_MAX;
  const questionInvalid = trimmed.length === 0 || charsOver;

  const onGenerate = () => {
    if (!llmReady || !connectionId || questionInvalid) return;
    generate.mutate({ connection_id: connectionId, question: trimmed });
  };

  return (
    <div className={styles.root}>
      <PageHeader
        title="Ask"
        subtitle="Ask a question in plain English. Plowered finds the relevant tables, drafts a SELECT, and waits for your go-ahead before running it."
        crumbs={[{ label: "Home", href: "/" }, { label: "Ask" }]}
        actions={
          <>
            <PageIntro
              title="What does Ask do?"
              body="Ask is the platform's Text-to-SQL surface. Pick a connection, type a question in plain English, and the model drafts a read-only SELECT against the catalog of that source. Nothing runs on the warehouse until you click Run."
              bullets={[
                "Powered by your tenant's BYOM key — costs roll into /cost and your budget. No shared LLM.",
                "SELECT-only guardrail: the platform refuses to execute INSERT / UPDATE / DELETE / DROP even if the model produces one.",
                "Every question, generated SQL, token usage, and result row count is logged at /ask/history and audited.",
              ]}
              cta="Get started: configure a primary chat provider under Settings → AI providers, then pick a SQL connection here and ask a question."
            />
            <Link href="/ask/history">
              <Button>History</Button>
            </Link>
          </>
        }
      />

      {/* Preflight: no LLM provider configured. Block the form and
          point at the setup page. Skipped while we're still loading the
          providers list to avoid a flash. */}
      {!llmLoading && !llmReady && (
        <MessageBar intent="warning">
          <MessageBarBody>
            No primary chat provider is configured. Set one up under{" "}
            <Link href="/settings/ai" style={{ textDecoration: "underline" }}>
              Settings → AI providers
            </Link>{" "}
            before using Ask — the page generates SQL via your tenant's
            BYOM key.
          </MessageBarBody>
        </MessageBar>
      )}

      <Card className={styles.panel}>
        <Subtitle1>Question</Subtitle1>
        <Caption1 className={styles.meta}>
          The model writes a read-only SELECT. Nothing is executed until you
          click Run.
        </Caption1>

        <div className={styles.fieldStack}>
          <Field label="Connection" required>
            <Dropdown
              value={
                sqlConnections.find((c) => c.id === connectionId)?.name ?? ""
              }
              selectedOptions={connectionId ? [connectionId] : []}
              onOptionSelect={(_, d) => setConnectionId(d.optionValue ?? "")}
              placeholder={
                conns.isLoading
                  ? "Loading…"
                  : sqlConnections.length === 0
                    ? "No SQL connections yet"
                    : "Pick a connection"
              }
              disabled={
                conns.isLoading || sqlConnections.length === 0 || !llmReady
              }
            >
              {sqlConnections.map((c) => (
                <Option key={c.id} value={c.id} text={c.name}>
                  <span>
                    {c.name}{" "}
                    <Caption1 className={styles.meta}>· {c.type}</Caption1>
                  </span>
                </Option>
              ))}
            </Dropdown>
          </Field>

          <Field
            label="Question"
            required
            validationState={charsOver ? "error" : "none"}
            validationMessage={
              charsOver
                ? `Trim to ${QUESTION_MAX} characters or fewer.`
                : undefined
            }
          >
            <Textarea
              value={question}
              onChange={(_, d) => {
                // Hard-cap at the limit so paste of a giant prompt
                // can't sneak through. The validation message still
                // fires while the user is mid-edit at the cap.
                const v = d.value.slice(0, QUESTION_MAX + 1);
                setQuestion(v);
              }}
              placeholder="How many active customers signed up last month?"
              rows={4}
              maxLength={QUESTION_MAX + 1}
              disabled={!llmReady}
            />
          </Field>

          <div className={styles.counterRow}>
            <Caption1 className={styles.meta}>
              {llmReady && chatProvider
                ? `Using ${chatProvider.name} (${chatProvider.model})`
                : ""}
            </Caption1>
            <span className={charsOver ? styles.counterWarn : styles.counter}>
              {charsUsed} / {QUESTION_MAX}
            </span>
          </div>
        </div>

        <div className={styles.actionRow}>
          <Button
            appearance="primary"
            icon={
              generate.isPending ? (
                <Spinner size="extra-tiny" />
              ) : (
                <Sparkle20Regular />
              )
            }
            onClick={onGenerate}
            disabled={
              !llmReady ||
              !connectionId ||
              questionInvalid ||
              generate.isPending
            }
            data-tour="ask-draft"
          >
            {generate.isPending ? "Drafting…" : "Draft SQL"}
          </Button>
        </div>
      </Card>

      {generate.error && (
        <MessageBar intent="error">
          <MessageBarBody>{(generate.error as Error).message}</MessageBarBody>
        </MessageBar>
      )}

      {generate.data && (
        <Card className={styles.panel}>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
              flexWrap: "wrap",
              gap: "8px",
            }}
          >
            <Subtitle2>Draft SQL</Subtitle2>
            <div className={styles.resultMeta}>
              {generate.data.tables_used.map((t) => (
                <Badge key={t} appearance="tint" color="informative">
                  {t}
                </Badge>
              ))}
              <Caption1 className={styles.meta}>
                {generate.data.model} ·{" "}
                {generate.data.input_tokens + generate.data.output_tokens} tokens
              </Caption1>
            </div>
          </div>
          <pre className={styles.sqlBox}>{generate.data.generated_sql}</pre>
          <div className={styles.actionRow}>
            <Button
              appearance="primary"
              icon={
                run.isPending ? (
                  <Spinner size="extra-tiny" />
                ) : (
                  <Play20Regular />
                )
              }
              onClick={() => run.mutate()}
              disabled={run.isPending}
            >
              {run.isPending ? "Running…" : "Run on warehouse"}
            </Button>
          </div>
        </Card>
      )}

      {run.error && (
        <MessageBar intent="error">
          <MessageBarBody>{(run.error as Error).message}</MessageBarBody>
        </MessageBar>
      )}

      {run.data && (
        <Card className={styles.panel}>
          <div className={styles.resultMeta}>
            <Subtitle2>Results</Subtitle2>
            <Caption1 className={styles.meta}>
              {run.data.row_count} row{run.data.row_count === 1 ? "" : "s"}{" "}
              · {run.data.elapsed_ms} ms
              {run.data.truncated && " · truncated at 1000"}
            </Caption1>
          </div>
          <div className={styles.scrollX}>
            <table className={styles.table}>
              <thead>
                <tr>
                  {run.data.columns.map((c) => (
                    <th key={c} className={styles.th}>{c}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {run.data.rows.map((row, i) => (
                  <tr key={i}>
                    {row.map((v, j) => (
                      <td key={j} className={styles.td}>
                        {formatCell(v)}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {run.data.rows.length === 0 && (
            <Body1 className={styles.meta}>Query returned 0 rows.</Body1>
          )}
        </Card>
      )}
    </div>
  );
}

// formatCell renders an arbitrary JSON-decoded value for the table.
// null, booleans, numbers, strings render plainly; objects/arrays
// JSON-stringify so the cell stays single-line.
function formatCell(v: unknown): string {
  if (v === null || v === undefined) return "—";
  if (typeof v === "object") return JSON.stringify(v);
  return String(v);
}
