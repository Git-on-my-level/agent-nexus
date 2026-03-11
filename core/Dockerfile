FROM golang:1.24-bookworm AS builder

WORKDIR /src/core

COPY core/go.mod core/go.sum ./
RUN go mod download

COPY core/ ./
COPY contracts/oar-schema.yaml /src/contracts/oar-schema.yaml
COPY contracts/gen/meta/commands.json /src/contracts/gen/meta/commands.json

RUN CGO_ENABLED=1 go build -trimpath -ldflags='-s -w' -o /out/oar-core ./cmd/oar-core

FROM debian:bookworm-slim

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl \
  && rm -rf /var/lib/apt/lists/*

RUN groupadd --system oar \
  && useradd --system --gid oar --create-home --home-dir /app --shell /usr/sbin/nologin oar

WORKDIR /app

COPY --from=builder /out/oar-core /usr/local/bin/oar-core
COPY --from=builder /src/contracts/oar-schema.yaml /app/contracts/oar-schema.yaml
COPY --from=builder /src/contracts/gen/meta/commands.json /app/contracts/gen/meta/commands.json

RUN mkdir -p /var/lib/oar/workspace && chown -R oar:oar /var/lib/oar

ENV OAR_WORKSPACE_ROOT=/var/lib/oar/workspace
ENV OAR_SCHEMA_PATH=/app/contracts/oar-schema.yaml
ENV OAR_META_COMMANDS_PATH=/app/contracts/gen/meta/commands.json
ENV OAR_LISTEN_ADDR=0.0.0.0:8000
ENV OAR_ALLOW_UNAUTHENTICATED_WRITES=false

VOLUME ["/var/lib/oar/workspace"]

EXPOSE 8000

HEALTHCHECK --interval=15s --timeout=5s --start-period=5s --retries=3 \
  CMD ["curl", "-sf", "http://127.0.0.1:8000/health"]

USER oar

ENTRYPOINT ["/usr/local/bin/oar-core"]
