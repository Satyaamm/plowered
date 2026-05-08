import Link from "next/link";

export function Header() {
  return (
    <header className="border-b border-ink-200 bg-white">
      <div className="mx-auto flex max-w-5xl items-center justify-between px-6 py-4">
        <Link href="/" className="font-mono text-lg font-bold text-ink-900">
          plowered
        </Link>
        <nav className="flex gap-6 text-sm text-ink-600">
          <Link href="/" className="hover:text-ink-900">Home</Link>
          <Link href="/search" className="hover:text-ink-900">Search</Link>
        </nav>
      </div>
    </header>
  );
}
