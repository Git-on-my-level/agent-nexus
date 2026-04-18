FROM golang:1.24-bookworm AS builder

WORKDIR /src/core

COPY core/go.mod core/go.sum ./
RUN go mod download

COPY core/ ./
COPY contracts/anx-schema.yaml /src/contracts/anx-schema.yaml
COPY contracts/gen/meta/commands.json /src/contracts/gen/meta/commands.json

RUN CGO_ENABLED=1 go build -trimpath -ldflags='-s -w' -o /out/anx-core ./cmd/anx-core

FROM debian:bookworm-slim

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl \
  && rm -rf /var/lib/apt/lists/*

RUN groupadd --system anx \
  && useradd --system --gid anx --create-home --home-dir /app --shell /usr/sbin/nologin anx

WORKDIR /app

COPY --from=builder /out/anx-core /usr/local/bin/anx-core
COPY --from=builder /src/contracts/anx-schema.yaml /app/contracts/anx-schema.yaml
COPY --from=builder /src/contracts/gen/meta/commands.json /app/contracts/gen/meta/commands.json

RUN mkdir -p /var/lib/anx/workspace && chown -R anx:anx /var/lib/anx

ENV ANX_WORKSPACE_ROOT=/var/lib/anx/workspace
ENV ANX_SCHEMA_PATH=/app/contracts/anx-schema.yaml
ENV ANX_META_COMMANDS_PATH=/app/contracts/gen/meta/commands.json
ENV ANX_LISTEN_ADDR=0.0.0.0:8000

VOLUME ["/var/lib/anx/workspace"]

EXPOSE 8000

HEALTHCHECK --interval=15s --timeout=5s --start-period=5s --retries=3 \
  CMD ["curl", "-sf", "http://127.0.0.1:8000/readyz"]

USER anx

ENTRYPOINT ["/usr/local/bin/anx-core"]
