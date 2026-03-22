.PHONY: help serve serve-control-plane build-prod run-prod fmt vet test check lint ci-smoke smoke smoke-control-plane smoke-control-plane-human

HOST ?= 127.0.0.1
PORT ?= 8000
WORKSPACE_ROOT ?= $(CURDIR)/.oar-workspace
SCHEMA_PATH ?= $(CURDIR)/../contracts/oar-schema.yaml

# For offline/air-gapped environments (e.g., agent sandboxes), set GOTOOLCHAIN=local
# to prevent Go from auto-downloading a toolchain version. Example:
#   GOTOOLCHAIN=local make check

help:
	@echo "Common targets:"
	@echo "  make serve      - run oar-core locally (development mode)"
	@echo "  make serve-control-plane - run oar-control-plane locally"
	@echo "  make check      - run formatting + cheap checks"
	@echo "  make fmt        - format Go files in-place"
	@echo "  make vet        - run go vet"
	@echo "  make test       - run go test"
	@echo "  make lint       - run repository lint script"
	@echo "  make ci-smoke   - run CI smoke test script"
	@echo "  make smoke      - run end-to-end API smoke script"
	@echo "  make smoke-control-plane - start the control plane and verify /health"
	@echo "  make smoke-control-plane-human - verify a signed control-plane human workspace request"
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

serve-control-plane:
	@HOST="$(HOST)" \
	PORT="$(PORT)" \
	CONTROL_PLANE_WORKSPACE_ROOT="$(WORKSPACE_ROOT)" \
	./scripts/dev-control-plane

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

vet:
	@go vet ./...

test:
	@go test ./...

check: fmt vet test check-offline

lint:
	@./scripts/lint

check-offline:
	@./scripts/check-offline

ci-smoke:
	@./scripts/ci-smoke

smoke:
	@./scripts/smoke

smoke-control-plane:
	@HOST="$(HOST)" \
	PORT="$(PORT)" \
	CONTROL_PLANE_WORKSPACE_ROOT="$(WORKSPACE_ROOT)" \
	./scripts/smoke-control-plane

smoke-control-plane-human:
	@PORT="$(PORT)" \
	WORKSPACE_ROOT="$(WORKSPACE_ROOT)" \
	./scripts/smoke-control-plane-human
