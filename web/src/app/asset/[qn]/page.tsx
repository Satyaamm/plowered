"use client";

import { useQuery } from "@tanstack/react-query";
import { use } from "react";
import { api } from "@/lib/api";

export default function AssetPage({ params }: { params: Promise<{ qn: string }> }) {
  const { qn: encoded } = use(params);
  const qn = decodeURIComponent(encoded);

  const asset = useQuery({
    queryKey: ["asset", qn],
    queryFn: () => api.getAssetByQualifiedName(qn),
  });

  const lineage = useQuery({
    queryKey: ["lineage", asset.data?.id, "upstream"],
    queryFn: () => api.lineage(asset.data!.id, "upstream", 1),
    enabled: !!asset.data?.id,
  });

  if (asset.isLoading) return <p className="text-sm text-ink-400">Loading…</p>;
  if (asset.error)
    return <p className="text-sm text-red-600">{(asset.error as Error).message}</p>;
  if (!asset.data) return null;

  const a = asset.data;

  return (
    <div className="space-y-8">
      <header>
        <div className="font-mono text-sm text-ink-600">{a.qualified_name}</div>
        <h1 className="text-2xl font-bold text-ink-900">{a.name}</h1>
        <div className="mt-1 flex items-center gap-3 text-xs text-ink-600">
          <span className="rounded bg-ink-100 px-2 py-0.5 uppercase tracking-wide">
            {a.type || "asset"}
          </span>
          <span>trust: {a.trust}</span>
          <span>updated {new Date(a.updated_at).toLocaleDateString()}</span>
        </div>
      </header>

      {a.description && (
        <section>
          <h2 className="mb-1 text-sm font-semibold uppercase tracking-wide text-ink-600">
            Description
          </h2>
          <p className="text-ink-900">{a.description}</p>
        </section>
      )}

      {a.tags && a.tags.length > 0 && (
        <section>
          <h2 className="mb-2 text-sm font-semibold uppercase tracking-wide text-ink-600">
            Tags
          </h2>
          <div className="flex flex-wrap gap-2">
            {a.tags.map((t) => (
              <span key={t} className="rounded-full border border-ink-200 bg-white px-2 py-0.5 text-xs">
                {t}
              </span>
            ))}
          </div>
        </section>
      )}

      <section>
        <h2 className="mb-2 text-sm font-semibold uppercase tracking-wide text-ink-600">
          Upstream lineage
        </h2>
        {lineage.isLoading && <p className="text-sm text-ink-400">Loading…</p>}
        {lineage.data && lineage.data.edges.length === 0 && (
          <p className="text-sm text-ink-400">No upstream edges.</p>
        )}
        {lineage.data && lineage.data.edges.length > 0 && (
          <ul className="space-y-1 font-mono text-sm">
            {lineage.data.edges.map((e) => (
              <li key={e.id} className="text-ink-600">
                ← <span className="text-ink-900">{e.source_id}</span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
