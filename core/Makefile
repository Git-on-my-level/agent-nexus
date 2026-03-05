.PHONY: help serve run-prod fmt vet test check lint ci-smoke smoke

HOST ?= 127.0.0.1
PORT ?= 8000
WORKSPACE_ROOT ?= $(CURDIR)/.oar-workspace
SCHEMA_PATH ?= $(CURDIR)/contracts/oar-schema.yaml

help:
	@echo "Common targets:"
	@echo "  make serve      - run oar-core locally (development mode)"
	@echo "  make check      - run formatting + cheap checks"
	@echo "  make fmt        - format Go files in-place"
	@echo "  make vet        - run go vet"
	@echo "  make test       - run go test"
	@echo "  make lint       - run repository lint script"
	@echo "  make ci-smoke   - run CI smoke test script"
	@echo "  make smoke      - run end-to-end API smoke script"
	@echo "  make run-prod   - build+run production-like binary locally"

serve:
	@HOST="$(HOST)" \
	PORT="$(PORT)" \
	WORKSPACE_ROOT="$(WORKSPACE_ROOT)" \
	SCHEMA_PATH="$(SCHEMA_PATH)" \
	./scripts/dev

run-prod:
	@WORKSPACE_ROOT="$(WORKSPACE_ROOT)" \
	SCHEMA_PATH="$(SCHEMA_PATH)" \
	PORT="$(PORT)" \
	./scripts/run-prod

fmt:
	@find . -type f -name '*.go' \
		-not -path './.git/*' \
		-not -path './.codex-autorunner/*' \
		-print0 | xargs -0 gofmt -w

vet:
	@go vet ./...

test:
	@go test ./...

check: fmt vet test

lint:
	@./scripts/lint

ci-smoke:
	@./scripts/ci-smoke

smoke:
	@./scripts/smoke
