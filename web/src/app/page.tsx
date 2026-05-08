"use client";

import { useQuery } from "@tanstack/react-query";
import { Title2, Subtitle2, Body1, Spinner, makeStyles } from "@fluentui/react-components";
import { api } from "@/lib/api";
import { AssetCard } from "@/components/asset-card";
import { SearchBar } from "@/components/search-bar";

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "32px" },
  hero: { display: "flex", flexDirection: "column", gap: "16px" },
  section: { display: "flex", flexDirection: "column", gap: "12px" },
  list: { display: "grid", gap: "12px" },
});

export default function Home() {
  const styles = useStyles();
  const recent = useQuery({
    queryKey: ["assets", "recent"],
    queryFn: () => api.listAssets({ pageSize: 12 }),
  });

  return (
    <div className={styles.root}>
      <section className={styles.hero}>
        <Title2>Find what you need.</Title2>
        <SearchBar />
      </section>

      <section className={styles.section}>
        <Subtitle2>Recently updated</Subtitle2>
        {recent.isLoading && <Spinner size="small" label="Loading…" />}
        {recent.error && <Body1>{(recent.error as Error).message}</Body1>}
        {recent.data && recent.data.assets?.length > 0 && (
          <div className={styles.list}>
            {recent.data.assets.map((a) => (
              <AssetCard key={a.id} asset={a} />
            ))}
          </div>
        )}
        {recent.data && (!recent.data.assets || recent.data.assets.length === 0) && (
          <Body1>No assets yet. Connect a data source to start populating the catalog.</Body1>
        )}
      </section>
    </div>
  );
}
