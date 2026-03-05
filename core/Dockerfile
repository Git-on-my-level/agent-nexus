FROM golang:1.24-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -trimpath -ldflags='-s -w' -o /out/oar-core ./cmd/oar-core

FROM debian:bookworm-slim

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates \
  && rm -rf /var/lib/apt/lists/*

RUN useradd --create-home --home-dir /app --shell /usr/sbin/nologin oar

WORKDIR /app

COPY --from=builder /out/oar-core /usr/local/bin/oar-core
COPY contracts/oar-schema.yaml /app/contracts/oar-schema.yaml

ENV OAR_WORKSPACE_ROOT=/var/lib/oar/workspace
ENV OAR_SCHEMA_PATH=/app/contracts/oar-schema.yaml
ENV OAR_LISTEN_ADDR=0.0.0.0:8000

VOLUME ["/var/lib/oar/workspace"]

EXPOSE 8000

USER oar

ENTRYPOINT ["/usr/local/bin/oar-core"]
