"use client";

import { useSearchParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { AssetCard } from "@/components/asset-card";
import { SearchBar } from "@/components/search-bar";

export default function SearchPage() {
  const params = useSearchParams();
  const q = params.get("q") ?? "";

  const search = useQuery({
    queryKey: ["search", q],
    queryFn: () => api.search(q),
    enabled: q.length > 0,
  });

  return (
    <div className="space-y-6">
      <SearchBar initial={q} />

      {!q && <p className="text-sm text-ink-400">Type a query to search assets.</p>}

      {q && search.isLoading && <p className="text-sm text-ink-400">Searching…</p>}
      {q && search.error && (
        <p className="text-sm text-red-600">{(search.error as Error).message}</p>
      )}

      {search.data && (
        <>
          <p className="text-sm text-ink-600">
            {search.data.hits.length} result(s) for{" "}
            <span className="font-mono">{q}</span>
          </p>
          <div className="grid gap-3">
            {search.data.hits.map((h) => (
              <AssetCard key={h.asset.id} asset={h.asset} />
            ))}
          </div>
        </>
      )}
    </div>
  );
}
