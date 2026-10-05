SHELL := /usr/bin/env bash

CORE_DIR := core
CLI_DIR := cli
WEB_UI_DIR := web-ui
BRIDGE_DIR := adapters/agent-bridge
HTTP_RECORD_DIR := tools/anx-http-record
PYTHON ?= python3
PRE_COMMIT_BIN := $(CURDIR)/.venv/bin/pre-commit
STATIC_ARGS ?=
TEST_FAST_ARGS ?=

CORE_HOST ?= 127.0.0.1
CORE_PORT ?= 8000
WEB_UI_PORT ?= 5173
CORE_BASE_URL ?= http://$(CORE_HOST):$(CORE_PORT)
ACTIONLINT_BIN := $(CURDIR)/.bin/actionlint
# Local SQLite + artifacts for anx-core (same default as core/Makefile).
CORE_WORKSPACE_ROOT ?= $(CURDIR)/$(CORE_DIR)/.anx-workspace
# When 1 (default), `make serve` removes CORE_WORKSPACE_ROOT before starting core,
# so each dev session starts from an empty workspace. With SEED_CORE=0 this yields
# an empty core; with SEED_CORE=1 the mock seed repopulates the fresh workspace.
RESET_DEV_WORKSPACE ?= 1
SEED_CORE ?= 1
FORCE_SEED ?= 0
DEV_SEED_SCENARIO ?= game-dev-studio

.DEFAULT_GOAL := help

.PHONY: help setup install-hooks check serve kill lint test format contract-gen contract-check contract-check-committed workflow-check version-sync version-check e2e-smoke hosted-smoke hosted-smoke-script-audit hosted-ops-test hosted-ops-smoke cli-check mcp-check cli-build cli-integration-test scenario-validate pm-serve http-record-test http-record-run http-record-compile http-record-replay bridge-setup bridge-doctor bridge-test release-check release-patch platform-constraints core-% bridge-% web-ui-% web-ui-static-ci

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"; printf "Targets:\n"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

install-hooks: ## Point git at tracked path-agnostic hook wrappers (safe across worktrees/submodules)
	git config core.hooksPath scripts/git-hooks

setup: ## Install repo tooling plus dependencies for web-ui, core, and cli
	$(PYTHON) -m venv .venv
	.venv/bin/pip install --upgrade pip pre-commit
	$(PRE_COMMIT_BIN) install-hooks
	$(MAKE) install-hooks
	./scripts/install-actionlint.sh
	pnpm install
	cd $(CORE_DIR) && go mod download
	cd $(CLI_DIR) && go mod download

web-ui-static-ci: ## Same steps as CI job web-ui-static-check (frozen lockfile + lint/unit + build)
	pnpm install --frozen-lockfile
	$(MAKE) -C $(WEB_UI_DIR) check
	pnpm -C $(WEB_UI_DIR) run build

.PHONY: check-static test-fast
check-static: ## Offline static checks of staged changes and generated-contract drift (STATIC_ARGS=--all for all files)
	$(PYTHON) -B scripts/git-hooks/checks.py static $(STATIC_ARGS)

test-fast: ## Changed-module Go -short and Vitest units; TEST_FAST_ARGS=--all checks all modules
	$(PYTHON) -B scripts/git-hooks/checks.py fast $(TEST_FAST_ARGS)

check: ## Run repo, core, cli, and web-ui checks
	$(MAKE) oss-boundary-check
	$(MAKE) contract-check
	$(MAKE) workflow-check
	$(MAKE) -C $(CORE_DIR) check
	$(MAKE) cli-check
	$(MAKE) mcp-check
	$(MAKE) http-record-test
	$(MAKE) -C $(WEB_UI_DIR) check

lint: ## Run lint checks for repo, core, and web-ui
	$(MAKE) workflow-check
	$(MAKE) -C $(CORE_DIR) lint
	$(MAKE) -C $(WEB_UI_DIR) lint

.PHONY: oss-boundary-check
oss-boundary-check: ## Reject private paths/hosts and optional external patterns in tracked text
	python3 ./scripts/test-oss-boundary.py
	./scripts/check-oss-boundary.sh

test: ## Run tests in both core and web-ui
	$(MAKE) -C $(CORE_DIR) test
	$(MAKE) cli-check
	$(MAKE) http-record-test
	$(MAKE) -C $(WEB_UI_DIR) test

format: ## Apply formatting in both core and web-ui
	$(MAKE) -C $(CORE_DIR) fmt
	$(MAKE) -C $(WEB_UI_DIR) format

contract-gen: ## Regenerate OpenAPI-derived contract artifacts
	./scripts/contract-gen

.PHONY: route-inventory route-inventory-check
route-inventory: ## Generate method/path/core access class inventory from contracts and router
	cd core && go run ./cmd/route-inventory

route-inventory-check: route-inventory ## Fail if the committed route inventory is stale
	git cat-file -e HEAD:contracts/gen/meta/routes.json
	git diff --exit-code -- contracts/gen/meta/routes.json

contract-check: ## Regenerate contracts and validate working tree (Go tests + TS compile)
	./scripts/contract-check

contract-check-committed: ## Like contract-check, plus Git drift check (matches CI contract job)
	./scripts/contract-check --committed

workflow-check: ## Lint GitHub Actions workflows
	./scripts/install-actionlint.sh
	$(ACTIONLINT_BIN)

version-sync: ## Regenerate version-derived source files
	./scripts/sync-version.sh

version-check: ## Verify version-derived source files are current
	./scripts/sync-version.sh --check

docs-ref-audit: ## Audit agent-facing docs for broken local path references
	./scripts/docs-ref-audit

mcp-check: ## Run MCP tool policy coverage and MCP tests
	node mcp/scripts/check-tool-policy.mjs
	cd mcp && go test ./...

cli-check: ## Run CLI checks
	$(MAKE) version-check
	cd $(CLI_DIR) && go test ./...

CLI_VERSION ?= $(shell ./scripts/read-version.sh)
CLI_SOURCE_REVISION ?= $(shell git rev-parse --verify HEAD 2>/dev/null || echo unknown)

cli-build: ## Build CLI binary
	cd $(CLI_DIR) && go build -ldflags='-X agent-nexus-cli/internal/buildinfo.Current=$(CLI_VERSION) -X agent-nexus-cli/internal/buildinfo.SourceRevision=$(CLI_SOURCE_REVISION)' -o anx ./cmd/anx

cli-integration-test: ## Run CLI real-binary integration tests (non-default)
	cd $(CLI_DIR) && go test -tags=integration ./integration/...

scenario-validate: ## Validate seeded scenario counts against a running core
	ANX_CORE_BASE_URL="$(CORE_BASE_URL)" ANX_DEV_SEED_SCENARIO="$(DEV_SEED_SCENARIO)" ./scripts/anx-scenario-validate

PM_WORK_DIR ?= $(CURDIR)/.tmp/pm-runner
PM_RUNNER ?= omp -p --mode json --model zai/glm-5.3 --auto-approve

pm-serve: cli-build ## Run anx pm serve as the derived PM agent (requires an enrolled host)
	$(CURDIR)/$(CLI_DIR)/anx --base-url "$(CORE_BASE_URL)" --as pm pm serve --work-dir "$(PM_WORK_DIR)" --runner '$(PM_RUNNER)'

http-record-test: ## Run tests for the local HTTP recording proxy
	cd $(HTTP_RECORD_DIR) && go test ./...

http-record-run: ## Run the local HTTP recording proxy (set ARGS='...')
	./scripts/anx-http-record $(ARGS)

http-record-compile: ## Compile a JSONL recording to replay JSON (set ARGS='...')
	./scripts/anx-http-compile $(ARGS)

http-record-replay: ## Replay a compiled seed JSON against a core (set ARGS='...')
	./scripts/anx-http-replay $(ARGS)

bridge-setup: ## Set up the bridge-local Python 3.11 virtualenv and deps
	$(MAKE) -C $(BRIDGE_DIR) setup

bridge-doctor: ## Verify the bridge-local Python/runtime setup
	$(MAKE) -C $(BRIDGE_DIR) doctor

bridge-test: ## Run bridge unit tests
	$(MAKE) -C $(BRIDGE_DIR) test

e2e-smoke: ## Run end-to-end core + CLI + web-ui smoke flow
	./scripts/e2e-smoke

platform-constraints: ## Check for Unix-only syscalls without build constraints
	./scripts/check-platform-constraints.sh
	./scripts/test-platform-constraints.sh

release-check: ## Validate release readiness (check + e2e + cross-platform build)
	$(MAKE) check
	$(MAKE) e2e-smoke
	$(MAKE) hosted-smoke
	$(MAKE) hosted-ops-test
	./scripts/build-cli-release-artifacts.sh "$(./scripts/read-version.sh)" /tmp/release-test

release-patch: ## Cut and publish a patch release from origin/main (optional: VERSION=vX.Y.Z RELEASE_ARGS='...')
	./scripts/release-patch.sh $(if $(VERSION),--version $(VERSION),) $(RELEASE_ARGS)

hosted-smoke: ## Run hosted-v1 production smoke suite (auth gate, onboarding, workspace access, staleness)
	./scripts/hosted-smoke

hosted-smoke-script-audit: ## Fast audit for stale hosted smoke script write routes
	./scripts/hosted/check-smoke-scripts.sh

hosted-ops-test: ## Run hosted provisioning/backup/restore verification tests
	./scripts/hosted/test-hosted-ops.sh

hosted-ops-smoke: ## Run one hosted provisioning/backup/restore smoke flow
	./scripts/hosted/smoke-test.sh

serve: ## Start core, seed mock dataset into core, then start web-ui
	@REPO_ROOT="$(CURDIR)" \
	CORE_HOST="$(CORE_HOST)" \
	CORE_PORT="$(CORE_PORT)" \
	CORE_BASE_URL="$(CORE_BASE_URL)" \
	CORE_WORKSPACE_ROOT="$(CORE_WORKSPACE_ROOT)" \
	WEB_UI_PORT="$(WEB_UI_PORT)" \
	RESET_DEV_WORKSPACE="$(RESET_DEV_WORKSPACE)" \
	SEED_CORE="$(SEED_CORE)" \
	DEV_SEED_SCENARIO="$(DEV_SEED_SCENARIO)" \
	FORCE_SEED="$(FORCE_SEED)" \
	./scripts/serve.sh

kill: ## Stop stale dev listeners (CORE_PORT + WEB_UI_PORT) and local MinIO container
	@CORE_PORT="$(CORE_PORT)" \
	WEB_UI_PORT="$(WEB_UI_PORT)" \
	./scripts/serve-kill.sh

core-%: ## Pass-through target to core Makefile
	$(MAKE) -C $(CORE_DIR) $*

bridge-%: ## Pass-through target to adapter bridge Makefile
	$(MAKE) -C $(BRIDGE_DIR) $*

web-ui-%: ## Pass-through target to web-ui Makefile
	$(MAKE) -C $(WEB_UI_DIR) $*

.PHONY: visualreport-check
visualreport-check: ## Verify the shared Go report contract and browser conformance
	@test -z "$$(gofmt -l contracts/visualreport)"
	cd contracts/visualreport && go vet ./... && go test ./...
	node scripts/check-visual-report-conformance.mjs
