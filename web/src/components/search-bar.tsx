"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { useState, type FormEvent } from "react";

export function SearchBar({ initial = "" }: { initial?: string }) {
  const router = useRouter();
  const params = useSearchParams();
  const [q, setQ] = useState(initial || params.get("q") || "");

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (!q.trim()) return;
    router.push(`/search?q=${encodeURIComponent(q.trim())}`);
  }

  return (
    <form onSubmit={onSubmit} className="flex w-full max-w-2xl gap-2">
      <input
        value={q}
        onChange={(e) => setQ(e.target.value)}
        placeholder="Search assets — table name, qualified name, or keyword"
        className="flex-1 rounded-md border border-ink-200 bg-white px-3 py-2 text-sm focus:border-ink-400 focus:outline-none"
        autoFocus
      />
      <button
        type="submit"
        className="rounded-md bg-ink-900 px-4 py-2 text-sm text-white hover:bg-ink-600"
      >
        Search
      </button>
    </form>
  );
}
