"use client";

import { useState } from "react";
import {
  Body1,
  Button,
  Caption1,
  Drawer,
  DrawerBody,
  DrawerHeader,
  DrawerHeaderTitle,
  Dropdown,
  Field,
  InfoLabel,
  Input,
  MessageBar,
  MessageBarBody,
  Option,
  Spinner,
  Title3,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  CheckmarkCircle20Filled,
  Database24Filled,
  Dismiss24Regular,
  ErrorCircle20Filled,
  Play20Regular,
} from "@fluentui/react-icons";
import { useCreateConnection, useTestDraftConnection } from "@/lib/hooks";

const TYPES = [
  { value: "postgres",   label: "PostgreSQL" },
  { value: "snowflake",  label: "Snowflake" },
  { value: "bigquery",   label: "BigQuery" },
  { value: "redshift",   label: "Redshift (coming soon)",   disabled: true },
  { value: "databricks", label: "Databricks (coming soon)", disabled: true },
  { value: "mysql",      label: "MySQL (coming soon)",      disabled: true },
];

const useStyles = makeStyles({
  body: { display: "flex", flexDirection: "column", gap: "20px", padding: "16px 0" },
  section: { display: "flex", flexDirection: "column", gap: "10px" },
  hint: { color: tokens.colorNeutralForeground3 },
  rowGrid: {
    display: "grid",
    gridTemplateColumns: "1fr 100px",
    gap: "12px",
  },
  footer: {
    display: "flex",
    gap: "8px",
    justifyContent: "flex-end",
    paddingTop: "12px",
    borderTop: `1px solid ${tokens.colorNeutralStroke2}`,
  },
  picker: {
    display: "grid",
    gridTemplateColumns: "repeat(auto-fill, minmax(150px, 1fr))",
    gap: "10px",
  },
  pickCard: {
    position: "relative",
    padding: "14px 14px 12px",
    borderRadius: "8px",
    cursor: "pointer",
    backgroundColor: tokens.colorNeutralBackground1,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke1}`,
    display: "flex",
    flexDirection: "column",
    gap: "8px",
    border: "none",
    textAlign: "left",
    color: tokens.colorNeutralForeground1,
    transitionProperty: "box-shadow, transform, background-color",
    transitionDuration: "120ms",
    transitionTimingFunction: "ease",
    ":hover:not(:disabled)": {
      boxShadow: `0 0 0 1px ${tokens.colorBrandStroke1}, 0 2px 6px rgba(0,0,0,0.04)`,
      transform: "translateY(-1px)",
    },
    ":disabled": { opacity: 0.55, cursor: "not-allowed" },
  },
  pickActive: {
    boxShadow: `0 0 0 2px ${tokens.colorBrandBackground}, 0 4px 12px rgba(243,128,32,0.12)`,
    backgroundColor: "#F3EEFE",
  },
  pickIcon: {
    width: "28px",
    height: "28px",
    color: tokens.colorBrandForeground1,
  },
  pickLabel: {
    fontWeight: 600,
    fontSize: "13px",
    color: tokens.colorNeutralForeground1,
  },
  pickCheck: {
    position: "absolute",
    top: "8px",
    right: "8px",
    color: tokens.colorBrandBackground,
  },
  testRow: {
    display: "flex",
    alignItems: "center",
    gap: "8px",
    fontSize: "12px",
    color: tokens.colorNeutralForeground3,
  },
  testPass: {
    display: "inline-flex",
    alignItems: "center",
    gap: "6px",
    color: tokens.colorPaletteGreenForeground1,
    fontWeight: 600,
  },
  testFail: {
    display: "inline-flex",
    alignItems: "center",
    gap: "6px",
    color: tokens.colorPaletteRedForeground1,
    fontWeight: 600,
  },
});

export function ConnectionWizard({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const styles = useStyles();
  const create = useCreateConnection();
  const testDraft = useTestDraftConnection();
  // Test state is a small machine: idle → testing → passed | failed.
  // Any form-field edit knocks us back to "idle" so a stale green
  // never gates a Create on a payload that hasn't been re-validated.
  const [testStatus, setTestStatus] = useState<"idle" | "passed" | "failed">("idle");
  const [testError, setTestError] = useState<string>("");

  const [type, setType] = useState("postgres");
  const [name, setName] = useState("");
  // Shared SQL-DB fields (used by Postgres)
  const [host, setHost] = useState("");
  const [port, setPort] = useState("5432");
  const [database, setDatabase] = useState("");
  const [user, setUser] = useState("");
  const [password, setPassword] = useState("");
  const [sslmode, setSslmode] = useState("require");
  // Snowflake-specific
  const [sfAccount, setSfAccount] = useState("");
  const [sfWarehouse, setSfWarehouse] = useState("");
  const [sfRole, setSfRole] = useState("");
  const [sfSchema, setSfSchema] = useState("");
  // BigQuery-specific
  const [bqProjectID, setBqProjectID] = useState("");
  const [bqDataset, setBqDataset] = useState("");
  const [bqLocation, setBqLocation] = useState("US");
  const [bqAuthMethod, setBqAuthMethod] = useState("service_account");

  const reset = () => {
    setType("postgres");
    setName("");
    setHost("");
    setPort("5432");
    setDatabase("");
    setUser("");
    setPassword("");
    setSslmode("require");
    setSfAccount("");
    setSfWarehouse("");
    setSfRole("");
    setSfSchema("");
    setBqProjectID("");
    setBqDataset("");
    setBqLocation("US");
    setBqAuthMethod("service_account");
    setTestStatus("idle");
    setTestError("");
    create.reset();
    testDraft.reset();
  };

  // Each form-field setter is wrapped so a successful test gets
  // invalidated the moment the payload changes — we never let "passed"
  // gate a Create on a payload the server hasn't seen yet.
  const dirty = <T,>(setter: (v: T) => void) => (v: T) => {
    setter(v);
    if (testStatus !== "idle") setTestStatus("idle");
  };

  const buildPayload = () => {
    let config: Record<string, unknown> = {};
    const credential = password;
    switch (type) {
      case "postgres":
        config = {
          host,
          port: Number(port) || 5432,
          database,
          user,
          sslmode,
        };
        break;
      case "snowflake":
        config = {
          account: sfAccount,
          user,
          warehouse: sfWarehouse,
          role: sfRole,
          database,
          schema: sfSchema,
          authenticator: "snowflake",
        };
        break;
      case "bigquery":
        config = {
          project_id: bqProjectID,
          dataset: bqDataset,
          location: bqLocation,
          auth_method: bqAuthMethod,
        };
        break;
    }
    return { name, type, config, password: credential };
  };

  const runTest = async () => {
    setTestStatus("idle");
    setTestError("");
    try {
      const r = await testDraft.mutateAsync(buildPayload());
      if (r.ok) {
        setTestStatus("passed");
      } else {
        setTestStatus("failed");
        setTestError(r.error || "Test failed");
      }
    } catch (e) {
      setTestStatus("failed");
      setTestError(e instanceof Error ? e.message : String(e));
    }
  };

  const submit = async () => {
    try {
      await create.mutateAsync(buildPayload());
      reset();
      onClose();
    } catch {
      /* error renders inline */
    }
  };

  const valid = (() => {
    if (!name) return false;
    switch (type) {
      case "postgres":
        return !!host && !!database && !!user && !!password;
      case "snowflake":
        return !!sfAccount && !!user && !!password;
      case "bigquery":
        return !!bqProjectID && (bqAuthMethod === "workload_identity" || !!password);
      default:
        return false;
    }
  })();

  const err = create.error as Error | null;

  return (
    <Drawer
      open={open}
      onOpenChange={(_, d) => {
        if (!d.open) {
          reset();
          onClose();
        }
      }}
      position="end"
      separator
      size="medium"
    >
      <DrawerHeader>
        <DrawerHeaderTitle
          action={
            <Button
              appearance="subtle"
              icon={<Dismiss24Regular />}
              onClick={onClose}
              aria-label="Close"
            />
          }
        >
          New connection
        </DrawerHeaderTitle>
      </DrawerHeader>
      <DrawerBody>
        <div className={styles.body}>
          <div className={styles.section}>
            <Title3>Source type</Title3>
            <div className={styles.picker}>
              {TYPES.map((t) => {
                const selected = type === t.value;
                return (
                  <button
                    key={t.value}
                    type="button"
                    className={`${styles.pickCard} ${selected ? styles.pickActive : ""}`}
                    disabled={!!t.disabled}
                    onClick={() => {
                      setType(t.value);
                      setTestStatus("idle");
                    }}
                  >
                    {selected && (
                      <CheckmarkCircle20Filled className={styles.pickCheck} />
                    )}
                    <Database24Filled className={styles.pickIcon} />
                    <span className={styles.pickLabel}>{t.label}</span>
                  </button>
                );
              })}
            </div>
          </div>

          <div className={styles.section}>
            <Title3>Connection details</Title3>
            <Field
              label={
                <InfoLabel info="A human-readable name for this datasource inside Plowered. Shown in connection lists, lineage graphs, and audit events. Pick something a teammate can recognise at a glance.">
                  Display name
                </InfoLabel>
              }
              required
            >
              <Input
                value={name}
                onChange={(_, d) => dirty(setName)(d.value)}
                placeholder="My production warehouse"
              />
            </Field>

            {type === "postgres" && (
              <>
                <div className={styles.rowGrid}>
                  <Field
                    label={
                      <InfoLabel info="Hostname or IP of the Postgres server reachable from the Plowered workers. For managed databases, this is the endpoint shown in your cloud console.">
                        Host
                      </InfoLabel>
                    }
                    required
                  >
                    <Input
                      value={host}
                      onChange={(_, d) => dirty(setHost)(d.value)}
                      placeholder="db.example.com"
                    />
                  </Field>
                  <Field
                    label={
                      <InfoLabel info="TCP port the Postgres server listens on. Defaults to 5432.">
                        Port
                      </InfoLabel>
                    }
                    required
                  >
                    <Input
                      value={port}
                      onChange={(_, d) => dirty(setPort)(d.value)}
                      placeholder="5432"
                    />
                  </Field>
                </div>
                <Field
                  label={
                    <InfoLabel info="The specific database (catalog) to crawl. Plowered enumerates schemas, tables, and columns within this database only.">
                      Database
                    </InfoLabel>
                  }
                  required
                >
                  <Input
                    value={database}
                    onChange={(_, d) => dirty(setDatabase)(d.value)}
                    placeholder="warehouse"
                  />
                </Field>
                <Field
                  label={
                    <InfoLabel info="Role used by Plowered to read metadata, sample rows, and run quality checks. Read-only is sufficient; we recommend a dedicated 'plowered' role with SELECT and USAGE only.">
                      Username
                    </InfoLabel>
                  }
                  required
                >
                  <Input
                    value={user}
                    onChange={(_, d) => dirty(setUser)(d.value)}
                    placeholder="db_user"
                  />
                </Field>
                <Field
                  label={
                    <InfoLabel info="Encrypted at rest with AES-256-GCM and decrypted in-memory only on the worker. Plowered never logs or returns this value through the API.">
                      Password
                    </InfoLabel>
                  }
                  required
                >
                  <Input
                    type="password"
                    value={password}
                    onChange={(_, d) => dirty(setPassword)(d.value)}
                  />
                </Field>
                <Field
                  label={
                    <InfoLabel info="TLS strictness for the Postgres connection. 'require' enforces TLS without cert verification; 'verify-full' is the strongest. Pick 'disable' only for localhost dev.">
                      SSL mode
                    </InfoLabel>
                  }
                >
                  <Dropdown
                    value={sslmode}
                    selectedOptions={[sslmode]}
                    onOptionSelect={(_, d) => dirty(setSslmode)(d.optionValue ?? "require")}
                  >
                    {["require", "verify-full", "verify-ca", "prefer", "disable"].map((v) => (
                      <Option key={v} value={v}>{v}</Option>
                    ))}
                  </Dropdown>
                </Field>
              </>
            )}

            {type === "snowflake" && (
              <>
                <Field
                  label={
                    <InfoLabel info="Snowflake account locator in the form <orgname>-<account> or <account>.<region>.<cloud>. Example: xy12345.us-east-1. Find it in the URL of your Snowflake console.">
                      Account
                    </InfoLabel>
                  }
                  required
                >
                  <Input
                    value={sfAccount}
                    onChange={(_, d) => dirty(setSfAccount)(d.value)}
                    placeholder="xy12345.us-east-1"
                  />
                </Field>
                <Field
                  label={
                    <InfoLabel info="Snowflake user that Plowered authenticates as. Recommend a dedicated PLOWERED user granted USAGE on warehouse + read access to the target schemas.">
                      Username
                    </InfoLabel>
                  }
                  required
                >
                  <Input
                    value={user}
                    onChange={(_, d) => dirty(setUser)(d.value)}
                    placeholder="DB_USER"
                  />
                </Field>
                <Field
                  label={
                    <InfoLabel info="Encrypted at rest with AES-256-GCM. For SSO/OAuth or key-pair auth, contact us — coming soon.">
                      Password
                    </InfoLabel>
                  }
                  required
                >
                  <Input
                    type="password"
                    value={password}
                    onChange={(_, d) => dirty(setPassword)(d.value)}
                  />
                </Field>
                <div className={styles.rowGrid}>
                  <Field
                    label={
                      <InfoLabel info="Compute warehouse used for crawls, samples, and quality checks. Pick a small or X-small warehouse — PurpleCube's workload is metadata-heavy, not compute-heavy.">
                        Warehouse
                      </InfoLabel>
                    }
                  >
                    <Input
                      value={sfWarehouse}
                      onChange={(_, d) => dirty(setSfWarehouse)(d.value)}
                      placeholder="ANALYTICS_WH"
                    />
                  </Field>
                  <Field
                    label={
                      <InfoLabel info="Snowflake role activated for this connection. Determines which databases and schemas the crawler can see. Defaults to the user's default role.">
                        Role
                      </InfoLabel>
                    }
                  >
                    <Input
                      value={sfRole}
                      onChange={(_, d) => dirty(setSfRole)(d.value)}
                      placeholder="PUBLIC"
                    />
                  </Field>
                </div>
                <div className={styles.rowGrid}>
                  <Field
                    label={
                      <InfoLabel info="Optional. Scopes the crawl to this database. Leave empty to crawl every database the role can read.">
                        Database
                      </InfoLabel>
                    }
                  >
                    <Input
                      value={database}
                      onChange={(_, d) => dirty(setDatabase)(d.value)}
                      placeholder="RAW"
                    />
                  </Field>
                  <Field
                    label={
                      <InfoLabel info="Optional. Further scopes the crawl to this schema within the database above. Leave empty for all schemas.">
                        Schema
                      </InfoLabel>
                    }
                  >
                    <Input
                      value={sfSchema}
                      onChange={(_, d) => dirty(setSfSchema)(d.value)}
                      placeholder="PUBLIC"
                    />
                  </Field>
                </div>
              </>
            )}

            {type === "bigquery" && (
              <>
                <Field
                  label={
                    <InfoLabel info="The GCP project that owns the datasets you want catalogued. PurpleCube's service account needs roles/bigquery.metadataViewer and roles/bigquery.dataViewer on this project.">
                      Project ID
                    </InfoLabel>
                  }
                  required
                >
                  <Input
                    value={bqProjectID}
                    onChange={(_, d) => dirty(setBqProjectID)(d.value)}
                    placeholder="my-gcp-project"
                  />
                </Field>
                <div className={styles.rowGrid}>
                  <Field
                    label={
                      <InfoLabel info="Optional. Scopes the crawl to this dataset. Leave empty to crawl every dataset visible to the service account.">
                        Dataset
                      </InfoLabel>
                    }
                  >
                    <Input
                      value={bqDataset}
                      onChange={(_, d) => dirty(setBqDataset)(d.value)}
                      placeholder="analytics"
                    />
                  </Field>
                  <Field
                    label={
                      <InfoLabel info="BigQuery dataset region (e.g. US, EU, asia-northeast1). Datasets in other regions are skipped — keep one location per connection.">
                        Location
                      </InfoLabel>
                    }
                  >
                    <Input
                      value={bqLocation}
                      onChange={(_, d) => dirty(setBqLocation)(d.value)}
                      placeholder="US"
                    />
                  </Field>
                </div>
                <Field
                  label={
                    <InfoLabel info="Service account JSON: paste a key with bigquery.metadataViewer + dataViewer. Workload identity: bind PurpleCube's GKE/Cloud Run SA to a GCP service account (no key material stored).">
                      Auth method
                    </InfoLabel>
                  }
                >
                  <Dropdown
                    value={bqAuthMethod === "service_account" ? "Service account JSON" : "Workload identity"}
                    selectedOptions={[bqAuthMethod]}
                    onOptionSelect={(_, d) => dirty(setBqAuthMethod)(d.optionValue ?? "service_account")}
                  >
                    <Option value="service_account">Service account JSON</Option>
                    <Option value="workload_identity">Workload identity</Option>
                  </Dropdown>
                </Field>
                {bqAuthMethod === "service_account" && (
                  <Field
                    label={
                      <InfoLabel info="Paste the entire service-account JSON key including the private_key field. Encrypted at rest with AES-256-GCM; only the worker decrypts it in memory.">
                        Service account JSON
                      </InfoLabel>
                    }
                    required
                  >
                    <Input
                      type="password"
                      value={password}
                      onChange={(_, d) => dirty(setPassword)(d.value)}
                      placeholder='{ "type": "service_account", "project_id": "...", ... }'
                    />
                  </Field>
                )}
                <Caption1 className={styles.hint}>
                  Heads up: the BigQuery driver ships in scaffold mode in this build.
                  Connect + test will succeed once the cmd binary is rebuilt with
                  the official cloud.google.com/go/bigquery client.
                </Caption1>
              </>
            )}
          </div>

          {err && (
            <MessageBar intent="error">
              <MessageBarBody>{err.message}</MessageBarBody>
            </MessageBar>
          )}

          <Caption1 className={styles.hint}>
            Plowered will store these credentials in the secrets vault and use
            them to crawl schemas, run quality checks, and back lineage. You
            can rotate or revoke access at any time from this drawer.
          </Caption1>

          {testStatus === "passed" && (
            <div className={styles.testRow}>
              <span className={styles.testPass}>
                <CheckmarkCircle20Filled /> Connection verified — ready to create
              </span>
            </div>
          )}
          {testStatus === "failed" && (
            <MessageBar intent="error">
              <MessageBarBody>
                <span className={styles.testFail}>
                  <ErrorCircle20Filled /> Test failed
                </span>{" "}
                {testError}
              </MessageBarBody>
            </MessageBar>
          )}

          <div className={styles.footer}>
            <Button appearance="secondary" onClick={onClose} disabled={create.isPending || testDraft.isPending}>
              Cancel
            </Button>
            <Button
              appearance="secondary"
              icon={testDraft.isPending ? <Spinner size="tiny" /> : <Play20Regular />}
              onClick={runTest}
              disabled={!valid || testDraft.isPending || create.isPending}
            >
              {testDraft.isPending ? "Testing…" : "Test connection"}
            </Button>
            <Button
              appearance="primary"
              onClick={submit}
              disabled={!valid || testStatus !== "passed" || create.isPending}
              title={
                testStatus !== "passed"
                  ? "Run Test connection first — Create is enabled once the credentials pass"
                  : undefined
              }
            >
              {create.isPending ? <Spinner size="tiny" /> : "Create connection"}
            </Button>
          </div>
        </div>
      </DrawerBody>
    </Drawer>
  );
}
