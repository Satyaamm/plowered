"use client";

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { AssetCard } from "@/components/asset-card";
import { SearchBar } from "@/components/search-bar";

export default function Home() {
  const recent = useQuery({
    queryKey: ["assets", "recent"],
    queryFn: () => api.listAssets({ pageSize: 12 }),
  });

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <h1 className="text-2xl font-bold text-ink-900">Find what you need.</h1>
        <SearchBar />
      </section>

      <section className="space-y-3">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-ink-600">
          Recently updated
        </h2>
        {recent.isLoading && <p className="text-sm text-ink-400">Loading…</p>}
        {recent.error && (
          <p className="text-sm text-red-600">
            {String((recent.error as Error).message)}
          </p>
        )}
        {recent.data && recent.data.assets?.length > 0 && (
          <div className="grid gap-3">
            {recent.data.assets.map((a) => (
              <AssetCard key={a.id} asset={a} />
            ))}
          </div>
        )}
        {recent.data && (!recent.data.assets || recent.data.assets.length === 0) && (
          <p className="text-sm text-ink-400">
            No assets yet. Connect a data source to start populating the catalog.
          </p>
        )}
      </section>
    </div>
  );
}
