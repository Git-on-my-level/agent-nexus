SHELL := /usr/bin/env bash

CORE_DIR := core
CLI_DIR := cli
WEB_UI_DIR := web-ui
BRIDGE_DIR := adapters/agent-bridge
HTTP_RECORD_DIR := tools/oar-http-record

CORE_HOST ?= 127.0.0.1
CORE_PORT ?= 8000
CONTROL_PLANE_PORT ?= 8100
CONTROL_PLANE_WORKSPACE_ROOT ?= $(CURDIR)/$(CORE_DIR)/.oar-control-plane
RESET_CONTROL_PLANE_WORKSPACE ?= 1
SYNC_WORKSPACE_CORES ?= 1
SAAS_DEV_PACKED_LISTEN_START ?= 18000
SAAS_DEV_PACKED_LISTEN_END ?= 19990
WEB_UI_PORT ?= 5173
CORE_BASE_URL ?= http://$(CORE_HOST):$(CORE_PORT)
ACTIONLINT_BIN := $(CURDIR)/.bin/actionlint
# Local SQLite + artifacts for oar-core (same default as core/Makefile).
CORE_WORKSPACE_ROOT ?= $(CURDIR)/$(CORE_DIR)/.oar-workspace
# When 1 (default), `make serve` removes CORE_WORKSPACE_ROOT before starting core if SEED_CORE=1,
# so each dev session starts from an empty workspace and mock seed does not stack on old data.
RESET_DEV_WORKSPACE ?= 1
SEED_CORE ?= 1
FORCE_SEED ?= 0
DEV_SEED_SCENARIO ?= default

.DEFAULT_GOAL := help

.PHONY: help setup check serve serve-control-plane lint test format contract-gen contract-check contract-check-committed workflow-check version-sync version-check e2e-smoke hosted-smoke hosted-ops-test hosted-ops-smoke saas-smoke saas-e2e saas-load-smoke packed-host-smoke cli-check cli-build cli-integration-test http-record-test http-record-run http-record-compile http-record-replay bridge-setup bridge-doctor bridge-test release-check release-patch platform-constraints core-% bridge-% web-ui-%

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"; printf "Targets:\n"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

setup: ## Install repo tooling plus dependencies for web-ui, core, and cli
	./scripts/install-actionlint.sh
	pnpm install
	cd $(CORE_DIR) && go mod download
	cd $(CLI_DIR) && go mod download

check: ## Run repo, core, cli, and web-ui checks
	$(MAKE) contract-check
	$(MAKE) workflow-check
	$(MAKE) -C $(CORE_DIR) check
	$(MAKE) cli-check
	$(MAKE) http-record-test
	$(MAKE) -C $(WEB_UI_DIR) check

lint: ## Run lint checks for repo, core, and web-ui
	$(MAKE) workflow-check
	$(MAKE) -C $(CORE_DIR) lint
	$(MAKE) -C $(WEB_UI_DIR) lint

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

cli-check: ## Run CLI checks
	$(MAKE) version-check
	cd $(CLI_DIR) && go test ./...

CLI_VERSION ?= $(shell ./scripts/read-version.sh)

cli-build: ## Build CLI binary
	cd $(CLI_DIR) && go build -ldflags='-X organization-autorunner-cli/internal/buildinfo.Current=$(CLI_VERSION)' -o oar ./cmd/oar

cli-integration-test: ## Run CLI real-binary integration tests (non-default)
	cd $(CLI_DIR) && go test -tags=integration ./integration/...

http-record-test: ## Run tests for the local HTTP recording proxy
	cd $(HTTP_RECORD_DIR) && go test ./...

http-record-run: ## Run the local HTTP recording proxy (set ARGS='...')
	./scripts/oar-http-record $(ARGS)

http-record-compile: ## Compile a JSONL recording to replay JSON (set ARGS='...')
	./scripts/oar-http-compile $(ARGS)

http-record-replay: ## Replay a compiled seed JSON against a core (set ARGS='...')
	./scripts/oar-http-replay $(ARGS)

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

release-check: ## Validate release readiness (check + e2e + cross-platform build)
	$(MAKE) check
	$(MAKE) e2e-smoke
	./scripts/build-cli-release-artifacts.sh "$(./scripts/read-version.sh)" /tmp/release-test

release-patch: ## Cut and publish a patch release from origin/main (optional: VERSION=vX.Y.Z RELEASE_ARGS='...')
	./scripts/release-patch.sh $(if $(VERSION),--version $(VERSION),) $(RELEASE_ARGS)

hosted-smoke: ## Run hosted-v1 production smoke suite (auth gate, onboarding, workspace access, staleness)
	./scripts/hosted-smoke

hosted-ops-test: ## Run hosted provisioning/backup/restore verification tests
	./scripts/hosted/test-hosted-ops.sh

hosted-ops-smoke: ## Run one hosted provisioning/backup/restore smoke flow
	./scripts/hosted/smoke-test.sh

saas-smoke: ## Run SaaS control-plane multi-workspace smoke test (account, org, workspaces, invite, launch, session-exchange, workspace-rw)
	./scripts/saas-smoke

saas-e2e: ## Run extended SaaS e2e flow (multi-workspace isolation, backup, session revocation)
	./scripts/saas-e2e

saas-load-smoke: ## Run SaaS load smoke test (multiple workspaces with concurrent reads/writes)
	./scripts/saas-load-smoke

packed-host-smoke: ## Run packed-host PMF deployment smoke (control-plane, web-ui, multi-workspace, heartbeat, backup/restore)
	./scripts/packed-host-smoke

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

serve-control-plane: ## Local SaaS stack: control plane + web UI + workspace cores (control-plane-only: make -C core serve-control-plane)
	@CONTROL_PLANE_PORT="$(CONTROL_PLANE_PORT)" \
	WEB_UI_PORT="$(WEB_UI_PORT)" \
	CONTROL_PLANE_WORKSPACE_ROOT="$(CONTROL_PLANE_WORKSPACE_ROOT)" \
	RESET_CONTROL_PLANE_WORKSPACE="$(RESET_CONTROL_PLANE_WORKSPACE)" \
	SYNC_WORKSPACE_CORES="$(SYNC_WORKSPACE_CORES)" \
	SAAS_DEV_PACKED_LISTEN_START="$(SAAS_DEV_PACKED_LISTEN_START)" \
	SAAS_DEV_PACKED_LISTEN_END="$(SAAS_DEV_PACKED_LISTEN_END)" \
	./scripts/serve-control-plane.sh

core-%: ## Pass-through target to core Makefile
	$(MAKE) -C $(CORE_DIR) $*

bridge-%: ## Pass-through target to adapter bridge Makefile
	$(MAKE) -C $(BRIDGE_DIR) $*

web-ui-%: ## Pass-through target to web-ui Makefile
	$(MAKE) -C $(WEB_UI_DIR) $*
