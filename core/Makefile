.PHONY: help serve build-prod run-prod fmt fmt-check vet test check check-ci lint ci-smoke smoke

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

test:
	@go test ./...

check: fmt vet test check-offline

check-ci: fmt-check vet test check-offline

lint:
	@./scripts/lint

check-offline:
	@./scripts/check-offline

ci-smoke:
	@./scripts/ci-smoke

smoke:
	@./scripts/smoke
