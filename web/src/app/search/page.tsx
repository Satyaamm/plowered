"use client";

import { useSearchParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { Body1, Spinner, makeStyles } from "@fluentui/react-components";
import { api } from "@/lib/api";
import { AssetCard } from "@/components/asset-card";
import { SearchBar } from "@/components/search-bar";

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "24px" },
  list: { display: "grid", gap: "12px" },
});

export default function SearchPage() {
  const styles = useStyles();
  const params = useSearchParams();
  const q = params.get("q") ?? "";

  const search = useQuery({
    queryKey: ["search", q],
    queryFn: () => api.search(q),
    enabled: q.length > 0,
  });

  return (
    <div className={styles.root}>
      <SearchBar initial={q} />
      {!q && <Body1>Type a query to search assets.</Body1>}
      {q && search.isLoading && <Spinner size="small" label="Searching…" />}
      {q && search.error && <Body1>{(search.error as Error).message}</Body1>}
      {search.data && (
        <>
          <Body1>{search.data.hits.length} result(s) for {q}</Body1>
          <div className={styles.list}>
            {search.data.hits.map((h) => (
              <AssetCard key={h.asset.id} asset={h.asset} />
            ))}
          </div>
        </>
      )}
    </div>
  );
}
