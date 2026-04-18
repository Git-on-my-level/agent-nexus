FROM node:20-slim AS builder

RUN corepack enable && corepack prepare pnpm@latest --activate

WORKDIR /src/web-ui

COPY web-ui/package.json web-ui/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

COPY web-ui/ ./
COPY contracts/gen/ts/ /src/contracts/gen/ts/

ENV ADAPTER=node
RUN pnpm run build

FROM node:20-slim

RUN groupadd --system anx \
  && useradd --system --gid anx --create-home --home-dir /app --shell /usr/sbin/nologin anx

WORKDIR /app

COPY --from=builder /src/web-ui/build ./build
COPY --from=builder /src/web-ui/package.json ./

ENV NODE_ENV=production
ENV PORT=3000
ENV ORIGIN=http://localhost:3000

EXPOSE 3000

HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
  CMD ["node", "-e", "fetch('http://127.0.0.1:3000').then(r => r.ok ? process.exit(0) : process.exit(1)).catch(() => process.exit(1))"]

USER anx

CMD ["node", "build/index.js"]
