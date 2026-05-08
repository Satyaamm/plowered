# Plowered Web

Next.js 15 (App Router) frontend for Plowered.

## Quickstart

```bash
cd web
npm install        # or bun install / pnpm install
npm run dev        # http://localhost:3000
```

The dev server proxies `/api/*` to the Plowered HTTP API. Configure with:

```bash
PLOWERED_API_BASE=http://localhost:8080 npm run dev
```

For local auth bypass, set the dev token:

```bash
NEXT_PUBLIC_PLOWERED_TOKEN=dev npm run dev
```

(The Go server reads `PLOWERED_AUTH_DEV_PRINCIPAL` to inject a fixed dev
identity without verifying the JWT — refer to `internal/api/middleware/auth.go`.)

## Stack

- Next.js 15 · App Router · React 19
- TypeScript (strict)
- Tailwind CSS
- TanStack Query (server state)
- Zod (runtime validation at IO boundaries)

## Pages

| Route | Purpose |
|---|---|
| `/` | Home — search bar + recently updated assets |
| `/search?q=…` | Global search results |
| `/asset/[qn]` | Asset detail with upstream lineage |

## Layout

```
src/
├── app/
│   ├── layout.tsx        root layout, Header, Providers
│   ├── page.tsx          home
│   ├── providers.tsx     QueryClientProvider
│   ├── globals.css       Tailwind imports
│   ├── search/page.tsx
│   └── asset/[qn]/page.tsx
├── components/
│   ├── header.tsx
│   ├── search-bar.tsx
│   └── asset-card.tsx
└── lib/
    ├── api.ts            typed fetch client
    └── types.ts          mirrors internal/core/graph/types.go
```

## Type sync

`lib/types.ts` is hand-maintained until `buf generate` produces TypeScript
bindings. Touch both files in the same PR when the Go domain types change.
