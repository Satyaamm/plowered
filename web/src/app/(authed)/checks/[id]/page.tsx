"use client";

import { useMemo, useState } from "react";
import { useParams } from "next/navigation";
import {
  Button,
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
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { useCheck, useCheckRuns, useRunCheck } from "@/lib/hooks";
import { CheckDesigner } from "@/components/check-designer";
import { StatusBadge } from "@/components/status-badge";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "20px" },
  metaRow: { display: "flex", gap: "16px", flexWrap: "wrap" },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  mono: { fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace" },
  chartWrap: { width: "100%", height: "240px" },
  outcomeStrip: {
    display: "flex",
    gap: "2px",
    flexWrap: "wrap",
    padding: "8px 0",
  },
  outcomeBlock: {
    width: "12px",
    height: "24px",
    borderRadius: "2px",
  },
});

export default function CheckDetailPage() {
  const styles = useStyles();
  const params = useParams<{ id: string }>();
  const id = params.id;
  const { data: check, isLoading, error } = useCheck(id);
  const { data: runs } = useCheckRuns(id, 100);
  const runCheck = useRunCheck();
  const [editOpen, setEditOpen] = useState(false);

  const chartData = useMemo(() => {
    if (!runs) return [];
    return [...runs]
      .reverse()
      .map((r) => ({
        t: new Date(r.StartedAt).getTime(),
        value: r.Value,
        threshold: r.Threshold,
        outcome: r.Outcome,
      }));
  }, [runs]);

  if (isLoading) return <LoadingState />;
  if (error) return <ErrorBanner error={error} />;
  if (!check) return <EmptyState title="Check not found" />;

  const last = runs?.[0];

  return (
    <div className={styles.root}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-end", gap: 16 }}>
        <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <Title2>{check.Name}</Title2>
          <div className={styles.metaRow}>
            <Text className={styles.mono}>{check.Type}</Text>
            <Text className={styles.meta}>{check.AssetQN || check.AssetID}</Text>
            <Text className={styles.meta}>severity: {check.Severity ?? "warning"}</Text>
            {last && <StatusBadge variant="check" status={last.Outcome} />}
          </div>
        </div>
        <div style={{ display: "flex", gap: 8 }}>
          <Button onClick={() => setEditOpen(true)}>Edit</Button>
          <Button
            appearance="primary"
            onClick={() => runCheck.mutate({ id: check.ID })}
            disabled={runCheck.isPending}
          >
            {runCheck.isPending ? "Queueing…" : "Run now"}
          </Button>
        </div>
      </div>

      <Card>
        <CardHeader header={<Subtitle2>Outcomes (last {runs?.length ?? 0})</Subtitle2>} />
        {!runs || runs.length === 0 ? (
          <Text className={styles.meta}>No runs yet.</Text>
        ) : (
          <div className={styles.outcomeStrip} title="Each block is one run, oldest left">
            {[...runs].reverse().map((r) => (
              <span
                key={r.ID}
                className={styles.outcomeBlock}
                style={{ backgroundColor: outcomeColor(r.Outcome) }}
                title={`${new Date(r.StartedAt).toLocaleString()}\n${r.Outcome} — value=${r.Value} threshold=${r.Threshold}`}
              />
            ))}
          </div>
        )}
      </Card>

      <Card>
        <CardHeader header={<Subtitle2>Recent values</Subtitle2>} />
        {chartData.length === 0 ? (
          <Text className={styles.meta}>No runs yet — trigger one to populate the history.</Text>
        ) : (
          <div className={styles.chartWrap}>
            <ResponsiveContainer>
              <LineChart data={chartData}>
                <CartesianGrid stroke="#E5E7EB" />
                <XAxis
                  dataKey="t"
                  tickFormatter={(v) =>
                    new Date(v as number).toLocaleTimeString(undefined, {
                      hour: "2-digit",
                      minute: "2-digit",
                    })
                  }
                  stroke="#7A6A55"
                />
                <YAxis stroke="#7A6A55" />
                <Tooltip
                  labelFormatter={(v) => new Date(v as number).toLocaleString()}
                  formatter={(value: number, name: string) => [value, name]}
                />
                <Line
                  type="monotone"
                  dataKey="value"
                  stroke="#7C3AED"
                  strokeWidth={2}
                  dot={false}
                  isAnimationActive={false}
                />
                <Line
                  type="monotone"
                  dataKey="threshold"
                  stroke="#7A6A55"
                  strokeDasharray="4 4"
                  dot={false}
                  isAnimationActive={false}
                />
              </LineChart>
            </ResponsiveContainer>
          </div>
        )}
      </Card>

      <Card>
        <CardHeader header={<Subtitle2>Run history</Subtitle2>} />
        {!runs || runs.length === 0 ? (
          <Text className={styles.meta}>No runs yet.</Text>
        ) : (
          <Table aria-label="Check runs">
            <TableHeader>
              <TableRow>
                <TableHeaderCell>When</TableHeaderCell>
                <TableHeaderCell>Outcome</TableHeaderCell>
                <TableHeaderCell>Value</TableHeaderCell>
                <TableHeaderCell>Threshold</TableHeaderCell>
                <TableHeaderCell>Diagnostic</TableHeaderCell>
              </TableRow>
            </TableHeader>
            <TableBody>
              {runs.map((r) => (
                <TableRow key={r.ID}>
                  <TableCell>
                    <Text className={styles.meta}>
                      {new Date(r.StartedAt).toLocaleString()}
                    </Text>
                  </TableCell>
                  <TableCell>
                    <StatusBadge variant="check" status={r.Outcome} />
                  </TableCell>
                  <TableCell className={styles.mono}>{r.Value}</TableCell>
                  <TableCell className={styles.mono}>{r.Threshold}</TableCell>
                  <TableCell>
                    <Text className={styles.meta}>{r.Diagnostic}</Text>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>

      <CheckDesigner open={editOpen} onClose={() => setEditOpen(false)} existing={check} />
    </div>
  );
}

function outcomeColor(outcome: string): string {
  if (outcome === "pass") return "#3F8C3D";
  if (outcome === "fail") return "#C03A3A";
  return "#A77B0E";
}
