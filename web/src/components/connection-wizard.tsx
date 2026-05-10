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
  Database24Filled,
  Dismiss24Regular,
} from "@fluentui/react-icons";
import { useCreateConnection } from "@/lib/hooks";

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
    gridTemplateColumns: "repeat(auto-fill, minmax(160px, 1fr))",
    gap: "8px",
  },
  pickCard: {
    padding: "12px",
    borderRadius: "6px",
    cursor: "pointer",
    backgroundColor: tokens.colorNeutralBackground2,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    display: "flex",
    flexDirection: "column",
    gap: "4px",
    border: "none",
    textAlign: "left",
    color: tokens.colorNeutralForeground1,
    ":hover:not(:disabled)": {
      boxShadow: `0 0 0 1px ${tokens.colorBrandStroke2}`,
    },
    ":disabled": { opacity: 0.5, cursor: "not-allowed" },
  },
  pickActive: {
    boxShadow: `0 0 0 2px ${tokens.colorBrandStroke1}`,
    backgroundColor: "#FBF1EB",
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
    create.reset();
  };

  const submit = async () => {
    let config: Record<string, unknown> = {};
    let credential = password;
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
        // For BigQuery, the "password" textarea holds the service-
        // account JSON; the API stores it sealed.
        credential = password;
        break;
    }
    try {
      await create.mutateAsync({ name, type, config, password: credential });
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
              {TYPES.map((t) => (
                <button
                  key={t.value}
                  type="button"
                  className={`${styles.pickCard} ${
                    type === t.value ? styles.pickActive : ""
                  }`}
                  disabled={!!t.disabled}
                  onClick={() => setType(t.value)}
                >
                  <Database24Filled style={{ color: "#B8521B" }} />
                  <span style={{ fontWeight: 600, fontSize: 13 }}>{t.label}</span>
                </button>
              ))}
            </div>
          </div>

          <div className={styles.section}>
            <Title3>Connection details</Title3>
            <Field label="Display name" required hint="What this datasource is called inside Plowered.">
              <Input
                value={name}
                onChange={(_, d) => setName(d.value)}
                placeholder="Production warehouse"
              />
            </Field>

            {type === "postgres" && (
              <>
                <div className={styles.rowGrid}>
                  <Field label="Host" required>
                    <Input
                      value={host}
                      onChange={(_, d) => setHost(d.value)}
                      placeholder="db.acme.com"
                    />
                  </Field>
                  <Field label="Port" required>
                    <Input
                      value={port}
                      onChange={(_, d) => setPort(d.value)}
                      placeholder="5432"
                    />
                  </Field>
                </div>
                <Field label="Database" required>
                  <Input
                    value={database}
                    onChange={(_, d) => setDatabase(d.value)}
                    placeholder="warehouse"
                  />
                </Field>
                <Field label="Username" required>
                  <Input
                    value={user}
                    onChange={(_, d) => setUser(d.value)}
                    placeholder="plowered_reader"
                  />
                </Field>
                <Field
                  label="Password"
                  required
                  hint="Encrypted with AES-256-GCM. Plowered never logs or returns this value."
                >
                  <Input
                    type="password"
                    value={password}
                    onChange={(_, d) => setPassword(d.value)}
                  />
                </Field>
                <Field label="SSL mode">
                  <Dropdown
                    value={sslmode}
                    selectedOptions={[sslmode]}
                    onOptionSelect={(_, d) => setSslmode(d.optionValue ?? "require")}
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
                  label="Account"
                  required
                  hint="Snowflake locator: <orgname>-<account>, e.g. xy12345.us-east-1."
                >
                  <Input
                    value={sfAccount}
                    onChange={(_, d) => setSfAccount(d.value)}
                    placeholder="xy12345.us-east-1"
                  />
                </Field>
                <Field label="Username" required>
                  <Input
                    value={user}
                    onChange={(_, d) => setUser(d.value)}
                    placeholder="PLOWERED_READER"
                  />
                </Field>
                <Field
                  label="Password"
                  required
                  hint="Encrypted at rest. For SSO/OAuth, contact us — coming soon."
                >
                  <Input
                    type="password"
                    value={password}
                    onChange={(_, d) => setPassword(d.value)}
                  />
                </Field>
                <div className={styles.rowGrid}>
                  <Field label="Warehouse" hint="Compute warehouse for crawls + samples.">
                    <Input
                      value={sfWarehouse}
                      onChange={(_, d) => setSfWarehouse(d.value)}
                      placeholder="ANALYTICS_WH"
                    />
                  </Field>
                  <Field label="Role">
                    <Input
                      value={sfRole}
                      onChange={(_, d) => setSfRole(d.value)}
                      placeholder="PUBLIC"
                    />
                  </Field>
                </div>
                <div className={styles.rowGrid}>
                  <Field label="Database" hint="Optional; scopes the crawl.">
                    <Input
                      value={database}
                      onChange={(_, d) => setDatabase(d.value)}
                      placeholder="RAW"
                    />
                  </Field>
                  <Field label="Schema" hint="Optional; further scopes the crawl.">
                    <Input
                      value={sfSchema}
                      onChange={(_, d) => setSfSchema(d.value)}
                      placeholder="PUBLIC"
                    />
                  </Field>
                </div>
              </>
            )}

            {type === "bigquery" && (
              <>
                <Field
                  label="Project ID"
                  required
                  hint="The GCP project that owns the datasets you want catalogued."
                >
                  <Input
                    value={bqProjectID}
                    onChange={(_, d) => setBqProjectID(d.value)}
                    placeholder="acme-warehouse"
                  />
                </Field>
                <div className={styles.rowGrid}>
                  <Field label="Dataset" hint="Optional; scopes the crawl.">
                    <Input
                      value={bqDataset}
                      onChange={(_, d) => setBqDataset(d.value)}
                      placeholder="analytics"
                    />
                  </Field>
                  <Field label="Location">
                    <Input
                      value={bqLocation}
                      onChange={(_, d) => setBqLocation(d.value)}
                      placeholder="US"
                    />
                  </Field>
                </div>
                <Field label="Auth method">
                  <Dropdown
                    value={bqAuthMethod === "service_account" ? "Service account JSON" : "Workload identity"}
                    selectedOptions={[bqAuthMethod]}
                    onOptionSelect={(_, d) => setBqAuthMethod(d.optionValue ?? "service_account")}
                  >
                    <Option value="service_account">Service account JSON</Option>
                    <Option value="workload_identity">Workload identity</Option>
                  </Dropdown>
                </Field>
                {bqAuthMethod === "service_account" && (
                  <Field
                    label="Service account JSON"
                    required
                    hint="Paste the entire JSON key. Encrypted at rest; only the worker can read it."
                  >
                    <Input
                      type="password"
                      value={password}
                      onChange={(_, d) => setPassword(d.value)}
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

          <div className={styles.footer}>
            <Button appearance="secondary" onClick={onClose} disabled={create.isPending}>
              Cancel
            </Button>
            <Button
              appearance="primary"
              onClick={submit}
              disabled={!valid || create.isPending}
            >
              {create.isPending ? <Spinner size="tiny" /> : "Create connection"}
            </Button>
          </div>
        </div>
      </DrawerBody>
    </Drawer>
  );
}
