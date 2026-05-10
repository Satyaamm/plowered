"use client";

import Link from "next/link";
import { Suspense, useEffect, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import {
  Badge,
  Body1,
  Button,
  Card,
  CardHeader,
  Caption1,
  Spinner,
  Subtitle2,
  Switch,
  Text,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { ArrowSync20Regular } from "@fluentui/react-icons";
import { api } from "@/lib/api";
import { useReindex, useSemanticSearch } from "@/lib/hooks";
import { useJob } from "@/lib/hooks/use-jobs";
import { AssetCard } from "@/components/asset-card";
import { SearchBar } from "@/components/search-bar";

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "20px" },
  toolbar: {
    display: "flex",
    justifyContent: "space-between",
    alignItems: "center",
    flexWrap: "wrap",
    gap: "12px",
  },
  list: { display: "grid", gap: "10px" },
  hitRow: {
    display: "grid",
    gridTemplateColumns: "auto 1fr auto",
    alignItems: "center",
    gap: "12px",
    padding: "10px 14px",
    backgroundColor: tokens.colorNeutralBackground1,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
  },
  score: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: "12px",
    color: tokens.colorBrandForeground1,
    minWidth: "44px",
  },
  qn: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: "13px",
  },
  desc: {
    color: tokens.colorNeutralForeground3,
    fontSize: "12px",
    marginTop: "2px",
  },
  tagRow: { display: "flex", gap: "4px", flexWrap: "wrap", marginTop: "4px" },
});

function SearchPageInner() {
  const styles = useStyles();
  const router = useRouter();
  const params = useSearchParams();
  const q = params.get("q") ?? "";
  const initialMode = (params.get("mode") as "keyword" | "semantic") || "semantic";
  const [mode, setMode] = useState<"keyword" | "semantic">(initialMode);

  const semantic = useSemanticSearch();
  const reindex = useReindex();
  const reindexJobId =
    reindex.data && "job_id" in reindex.data ? reindex.data.job_id : null;
  const reindexJob = useJob(reindexJobId);

  const keyword = useQuery({
    queryKey: ["search-keyword", q],
    queryFn: () => api.search(q),
    enabled: mode === "keyword" && q.length > 0,
  });

  // Re-fire semantic search whenever query or mode changes.
  useEffect(() => {
    if (mode === "semantic" && q) {
      semantic.mutate({ query: q, k: 20 });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q, mode]);

  // Sync mode → URL so refreshes preserve it.
  useEffect(() => {
    const usp = new URLSearchParams(Array.from(params.entries()));
    if (mode === "keyword") usp.set("mode", "keyword");
    else usp.delete("mode");
    const next = usp.toString();
    router.replace(next ? `/search?${next}` : "/search");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode]);

  const hits = useMemo(() => semantic.data?.hits ?? [], [semantic.data]);

  return (
    <div className={styles.root}>
      <SearchBar initial={q} />

      <div className={styles.toolbar}>
        <Switch
          label={`${mode === "semantic" ? "Semantic (vector)" : "Keyword"} search`}
          checked={mode === "semantic"}
          onChange={(_, d) => setMode(d.checked ? "semantic" : "keyword")}
        />
        <Button
          appearance="subtle"
          icon={<ArrowSync20Regular />}
          onClick={() => reindex.mutate()}
          disabled={reindex.isPending}
        >
          {reindex.isPending ? "Reindexing…" : "Reindex catalog"}
        </Button>
      </div>
      {reindex.data && !("job_id" in reindex.data) && (
        <Caption1 style={{ color: tokens.colorNeutralForeground3 }}>
          Reindexed {reindex.data.reindexed} assets ({reindex.data.model}).
        </Caption1>
      )}
      {reindexJob.data && reindexJob.data.status !== "succeeded" && reindexJob.data.status !== "failed" && (
        <Caption1 style={{ color: tokens.colorNeutralForeground3 }}>
          Reindexing… {reindexJob.data.progress_pct}%
          {reindexJob.data.message ? ` — ${reindexJob.data.message}` : ""}
        </Caption1>
      )}
      {reindexJob.data?.status === "succeeded" && reindexJob.data.result && (
        <Caption1 style={{ color: tokens.colorNeutralForeground3 }}>
          Reindexed{" "}
          {(reindexJob.data.result as Record<string, number>).indexed ?? 0}{" "}
          assets.
        </Caption1>
      )}
      {reindexJob.data?.status === "failed" && (
        <Caption1 style={{ color: tokens.colorPaletteRedForeground1 }}>
          Reindex failed: {reindexJob.data.error ?? "unknown error"}
        </Caption1>
      )}

      {!q && <Body1>Type a query to search the catalog.</Body1>}

      {mode === "semantic" && (
        <Card>
          <CardHeader header={<Subtitle2>Semantic results</Subtitle2>} />
          {semantic.isPending && <Spinner size="small" label="Embedding query…" />}
          {semantic.error && <Body1>{(semantic.error as Error).message}</Body1>}
          {semantic.data && hits.length === 0 && (
            <Body1 style={{ padding: "0 16px 16px" }}>
              No matches yet. Try{" "}
              <Button size="small" appearance="subtle" onClick={() => reindex.mutate()}>
                Reindex
              </Button>{" "}
              if you've crawled new connections.
            </Body1>
          )}
          {hits.length > 0 && (
            <div className={styles.list} style={{ padding: "0 16px 16px" }}>
              {hits.map((h) => (
                <div key={h.asset_id} className={styles.hitRow}>
                  <span className={styles.score}>{h.score.toFixed(3)}</span>
                  <div>
                    <Link
                      href={`/asset/${encodeURIComponent(h.qualified_name)}`}
                      className={styles.qn}
                      style={{ color: tokens.colorBrandForeground1 }}
                    >
                      {h.qualified_name}
                    </Link>
                    {h.description && (
                      <div className={styles.desc}>{h.description}</div>
                    )}
                    {h.tags && h.tags.length > 0 && (
                      <div className={styles.tagRow}>
                        {h.tags.slice(0, 4).map((t) => (
                          <Badge
                            key={t}
                            appearance="outline"
                            color={
                              t.startsWith("class:pci")
                                ? "danger"
                                : t.startsWith("class:pii")
                                  ? "warning"
                                  : "subtle"
                            }
                          >
                            {t.replace(/^class:/, "")}
                          </Badge>
                        ))}
                      </div>
                    )}
                  </div>
                  <Badge appearance="outline" color="brand">{h.type}</Badge>
                </div>
              ))}
            </div>
          )}
        </Card>
      )}

      {mode === "keyword" && q && (
        <Card>
          <CardHeader header={<Subtitle2>Keyword results</Subtitle2>} />
          {keyword.isLoading && <Spinner size="small" label="Searching…" />}
          {keyword.error && <Body1>{(keyword.error as Error).message}</Body1>}
          {keyword.data && (
            <>
              <Text style={{ padding: "0 16px" }}>
                {keyword.data.hits.length} result(s) for {q}
              </Text>
              <div className={styles.list} style={{ padding: "12px 16px 16px" }}>
                {keyword.data.hits.map((h) => (
                  <AssetCard key={h.asset.id} asset={h.asset} />
                ))}
              </div>
            </>
          )}
        </Card>
      )}
    </div>
  );
}

export default function SearchPage() {
  return (
    <Suspense fallback={<Spinner size="small" label="Loading…" />}>
      <SearchPageInner />
    </Suspense>
  );
}
