.PHONY: help serve build-prod run-prod fmt fmt-check vet test test-perf check check-ci lint ci-smoke smoke check-linux-observation

HOST ?= 127.0.0.1
PORT ?= 8000
WORKSPACE_ROOT ?= $(CURDIR)/.anx-workspace
SCHEMA_PATH ?= $(CURDIR)/../contracts/anx-schema.yaml
GO_TEST_TIMEOUT ?= 13m

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
	@echo "  make test-perf  - run advisory scale and latency tests"
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

# Each fixture owns its database and workers. Bound in-package concurrency
# while allowing four packages to execute together on CI runners.
test:
	@go test -p=4 -parallel=4 -timeout=$(GO_TEST_TIMEOUT) ./...

# Advisory only: keep scale/latency tests out of default and short runs.
# The separately sharded TestPerformanceRoutes lane stays outside this target.
# Callers can preserve route shard/worker/report environment variables.
PERF_TEST_PATTERN ?= ^(TestPerformanceAskDeliveryReadBudgets|TestPerformanceAskCompatibilityBoardLookupBudget|TestPerformancePlanAndReportReadsHaveBoundedQueryCounts|TestPerformanceReceiptStreamTickBudgetIndependentOfHistory|TestPerformanceReceiptStreamHiddenHistoryTickTiming|TestPerformanceSourceURLResolutionBoundsTotalReturnedRows|TestPerformanceOverviewSmallWorkspaceLatency|TestPerformanceOverviewPersonalSizedWorkspaceLatency|TestPerformanceOverviewDenseAccessWorkspaceLatency|TestPerformanceInboxStreamTickHiddenVolumeBudget|TestPerformancePlanReadsStayBoundedAtMaximumFanOut|TestResourceAccessCommonReadPerformance|TestResourceAccessLargeDenialPrepareAndPMRoutes)$$
PERF_TEST_ARGS ?=
PERF_TEST_PACKAGES ?= ./internal/primitives ./internal/server ./internal/storage

test-perf:
	@ANX_PERFORMANCE_TEST=1 go test -p=1 -parallel=1 -timeout=30m -count=1 $(PERF_TEST_PACKAGES) -run '$(PERF_TEST_PATTERN)' $(PERF_TEST_ARGS)

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
