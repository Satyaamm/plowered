import Link from "next/link";
import type { Asset } from "@/lib/types";

export function AssetCard({ asset }: { asset: Asset }) {
  return (
    <Link
      href={`/asset/${encodeURIComponent(asset.qualified_name)}`}
      className="block rounded-md border border-ink-200 bg-white p-4 hover:border-ink-400"
    >
      <div className="flex items-start justify-between gap-4">
        <div>
          <div className="font-mono text-sm text-ink-600">{asset.qualified_name}</div>
          <div className="mt-1 text-base font-medium text-ink-900">{asset.name}</div>
          {asset.description && (
            <p className="mt-2 text-sm text-ink-600">{asset.description}</p>
          )}
        </div>
        <span className="rounded bg-ink-100 px-2 py-1 text-xs uppercase tracking-wide text-ink-600">
          {asset.type || "asset"}
        </span>
      </div>
      {asset.tags && asset.tags.length > 0 && (
        <div className="mt-3 flex flex-wrap gap-2">
          {asset.tags.map((t) => (
            <span key={t} className="rounded-full border border-ink-200 px-2 py-0.5 text-xs text-ink-600">
              {t}
            </span>
          ))}
        </div>
      )}
    </Link>
  );
}
