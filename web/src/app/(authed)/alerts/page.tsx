"use client";

import {
  Body1,
  Card,
  CardHeader,
  Subtitle2,
  Table,
  TableBody,
  TableCell,
  TableHeader,
  TableHeaderCell,
  TableRow,
  Text,
  Title2,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { useChannels, useDeliveries, useNotifyRules } from "@/lib/hooks";
import { StatusBadge } from "@/components/status-badge";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "24px" },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  mono: { fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace" },
  twoCol: {
    display: "grid",
    gridTemplateColumns: "1fr 1fr",
    gap: "16px",
  },
});

export default function AlertsPage() {
  const styles = useStyles();
  const channels = useChannels();
  const rules = useNotifyRules();
  const deliveries = useDeliveries(100);

  return (
    <div className={styles.root}>
      <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
        <Title2>Alerts</Title2>
        <Body1>
          Notification deliveries and the channels + rules that route them.
          Polls every 10 seconds.
        </Body1>
      </div>

      <div className={styles.twoCol}>
        <Card>
          <CardHeader header={<Subtitle2>Channels</Subtitle2>} />
          {channels.isLoading && <LoadingState />}
          {channels.error && <ErrorBanner error={channels.error} />}
          {channels.data && channels.data.length === 0 && (
            <Text className={styles.meta}>No channels configured.</Text>
          )}
          {channels.data && channels.data.length > 0 && (
            <Table aria-label="Channels" size="small">
              <TableHeader>
                <TableRow>
                  <TableHeaderCell>Name</TableHeaderCell>
                  <TableHeaderCell>Kind</TableHeaderCell>
                </TableRow>
              </TableHeader>
              <TableBody>
                {channels.data.map((c) => (
                  <TableRow key={c.ID}>
                    <TableCell>{c.Name}</TableCell>
                    <TableCell className={styles.mono}>{c.Kind}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </Card>

        <Card>
          <CardHeader header={<Subtitle2>Rules</Subtitle2>} />
          {rules.isLoading && <LoadingState />}
          {rules.error && <ErrorBanner error={rules.error} />}
          {rules.data && rules.data.length === 0 && (
            <Text className={styles.meta}>No rules configured.</Text>
          )}
          {rules.data && rules.data.length > 0 && (
            <Table aria-label="Rules" size="small">
              <TableHeader>
                <TableRow>
                  <TableHeaderCell>Channel</TableHeaderCell>
                  <TableHeaderCell>Min severity</TableHeaderCell>
                  <TableHeaderCell>Enabled</TableHeaderCell>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rules.data.map((r) => (
                  <TableRow key={r.ID}>
                    <TableCell className={styles.mono}>
                      {r.ChannelID.slice(0, 8)}
                    </TableCell>
                    <TableCell>{r.MinSeverity ?? "—"}</TableCell>
                    <TableCell>{r.Enabled ? "yes" : "no"}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </Card>
      </div>

      <Card>
        <CardHeader header={<Subtitle2>Recent deliveries</Subtitle2>} />
        {deliveries.isLoading && <LoadingState />}
        {deliveries.error && <ErrorBanner error={deliveries.error} />}
        {deliveries.data && deliveries.data.length === 0 && (
          <EmptyState
            title="No deliveries yet"
            body="Notifications will appear here once a rule fires."
          />
        )}
        {deliveries.data && deliveries.data.length > 0 && (
          <Table aria-label="Deliveries">
            <TableHeader>
              <TableRow>
                <TableHeaderCell>When</TableHeaderCell>
                <TableHeaderCell>Status</TableHeaderCell>
                <TableHeaderCell>Subject</TableHeaderCell>
                <TableHeaderCell>Attempts</TableHeaderCell>
                <TableHeaderCell>Last error</TableHeaderCell>
              </TableRow>
            </TableHeader>
            <TableBody>
              {deliveries.data.map((d) => (
                <TableRow key={d.ID}>
                  <TableCell>
                    <Text className={styles.meta}>
                      {new Date(d.CreatedAt).toLocaleString()}
                    </Text>
                  </TableCell>
                  <TableCell>
                    <StatusBadge variant="delivery" status={d.Status} />
                  </TableCell>
                  <TableCell>{d.Subject}</TableCell>
                  <TableCell>{d.Attempts}</TableCell>
                  <TableCell>
                    {d.LastError && (
                      <Text className={styles.meta}>{d.LastError}</Text>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>
    </div>
  );
}
