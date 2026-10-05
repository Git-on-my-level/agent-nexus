.PHONY: help serve build-prod run-prod fmt fmt-check vet test check check-ci lint ci-smoke smoke check-linux-observation

HOST ?= 127.0.0.1
PORT ?= 8000
WORKSPACE_ROOT ?= $(CURDIR)/.anx-workspace
SCHEMA_PATH ?= $(CURDIR)/../contracts/anx-schema.yaml

# For offline/air-gapped environments (e.g., agent sandboxes), set GOTOOLCHAIN=local
# to prevent Go from auto-downloading a toolchain version. Example:
#   GOTOOLCHAIN=local make check

help:
	@echo "Common targets:"
	@echo "  make serve      - run anx-core locally (development mode)"
	@echo "  make fmt        - format Go files in-place"
	@echo "  make fmt-check  - verify Go files are gofmt-clean"
	@echo "  make check      - run formatting + cheap checks locally"
	@echo "  make check-ci   - run the CI-safe check set"
	@echo "  make vet        - run go vet"
	@echo "  make test       - run go test"
	@echo "  make lint       - run repository lint script"
	@echo "  make ci-smoke   - run CI smoke test script"
	@echo "  make smoke      - run end-to-end API smoke script"
	@echo "  make build-prod - build production binary"
	@echo "  make run-prod   - build+run production-like binary locally"

build-prod:
	@./scripts/build-prod

serve:
	@HOST="$(HOST)" \
	PORT="$(PORT)" \
	WORKSPACE_ROOT="$(WORKSPACE_ROOT)" \
	SCHEMA_PATH="$(SCHEMA_PATH)" \
	./scripts/dev

run-prod: build-prod
	@WORKSPACE_ROOT="$(WORKSPACE_ROOT)" \
	SCHEMA_PATH="$(SCHEMA_PATH)" \
	PORT="$(PORT)" \
	./scripts/run-prod

fmt:
	@find . -type f -name '*.go' \
		-not -path './.git/*' \
		-not -path './.codex-autorunner/*' \
		-print0 | xargs -0 gofmt -w

fmt-check:
	@set -eu; \
	first_file="$$(find . -type f -name '*.go' \
		-not -path './.git/*' \
		-not -path './.codex-autorunner/*' \
		-print -quit)"; \
	if [ -n "$$first_file" ]; then \
		unformatted="$$(find . -type f -name '*.go' \
			-not -path './.git/*' \
			-not -path './.codex-autorunner/*' \
			-print0 | xargs -0 gofmt -l)"; \
		if [ -n "$$unformatted" ]; then \
			echo "gofmt needed for:"; \
			printf '%s\n' "$$unformatted"; \
			exit 1; \
		fi; \
	fi

vet:
	@go vet ./...

check-linux-observation:
	@echo "GOOS=linux observation build (no Linux host available to run isolation)"
	@CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /dev/null ./internal/observation
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./internal/observation

# HTTP fixtures run projection/SSE workers; bound fixture concurrency so
# those background workers do not compete across every available CPU.
# The expanded privacy matrix also needs a package budget above Go's 10m
# default. Individual request and assertion deadlines remain unchanged.
test:
	@go test -parallel=2 -timeout=12m ./...

check: fmt vet test check-offline check-linux-observation

check-ci: fmt-check vet test check-offline check-linux-observation

lint:
	@./scripts/lint

check-offline:
	@./scripts/check-offline

ci-smoke:
	@./scripts/ci-smoke

smoke:
	@./scripts/smoke
