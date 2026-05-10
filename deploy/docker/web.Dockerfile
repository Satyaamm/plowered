# syntax=docker/dockerfile:1.7

# Build the Next.js app and serve it from a slim Node runtime.
# Multi-stage so the final image carries only the standalone bundle.

FROM node:20-alpine AS deps
WORKDIR /app
COPY web/package.json web/package-lock.json* web/pnpm-lock.yaml* web/bun.lockb* ./
RUN if [ -f package-lock.json ]; then npm ci; \
    elif [ -f pnpm-lock.yaml ]; then corepack enable && pnpm install --frozen-lockfile; \
    else npm install --no-audit --no-fund; fi

FROM node:20-alpine AS build
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY web/ .
# Next.js bakes the rewrite destination from next.config.mjs at build
# time. Pass the upstream API base in via build-arg so the resulting
# image points at the right hostname for its environment (compose
# defaults to http://plowered:8080).
ARG PLOWERED_API_BASE=http://plowered:8080
ENV PLOWERED_API_BASE=${PLOWERED_API_BASE}
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

FROM node:20-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV PORT=3000

# Run as non-root.
RUN addgroup -g 1001 -S nodejs && adduser -S nextjs -u 1001 -G nodejs

COPY --from=build --chown=nextjs:nodejs /app/.next ./.next
COPY --from=build --chown=nextjs:nodejs /app/public ./public
COPY --from=build --chown=nextjs:nodejs /app/package.json ./package.json
COPY --from=build --chown=nextjs:nodejs /app/node_modules ./node_modules

USER nextjs
EXPOSE 3000
CMD ["node_modules/.bin/next", "start"]
