# syntax=docker/dockerfile:1
# Build from agent-nexus repository root:
#   docker build -f core/Dockerfile -t anx-core:local .
FROM golang:1.23-bookworm AS builder
WORKDIR /src
COPY core/go.mod core/go.sum ./core/
RUN cd core && go mod download
COPY contracts ./contracts
COPY core ./core
WORKDIR /src/core
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /anx-core ./cmd/anx-core

FROM gcr.io/distroless/static-debian12
COPY --from=builder /anx-core /usr/local/bin/anx-core
COPY --from=builder /src/contracts/anx-schema.yaml /etc/anx/anx-schema.yaml
COPY --from=builder /src/contracts/gen/meta/commands.json /etc/anx/meta/commands.json
ENV ANX_SCHEMA_PATH=/etc/anx/anx-schema.yaml
ENV ANX_META_COMMANDS_PATH=/etc/anx/meta/commands.json
VOLUME ["/state"]
USER 10001:10001
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/usr/local/bin/anx-core", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/anx-core"]
