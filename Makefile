SHELL := /usr/bin/env bash

CORE_DIR := core
CLI_DIR := cli
WEB_UI_DIR := web-ui
BRIDGE_DIR := adapters/agent-bridge

CORE_HOST ?= 127.0.0.1
CORE_PORT ?= 8000
CONTROL_PLANE_PORT ?= 8100
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

.DEFAULT_GOAL := help

.PHONY: help setup check serve serve-control-plane lint test format contract-gen contract-check workflow-check version-sync version-check e2e-smoke hosted-smoke hosted-ops-test hosted-ops-smoke saas-smoke saas-e2e saas-load-smoke packed-host-smoke cli-check cli-test cli-build cli-integration-test bridge-setup bridge-doctor bridge-test core-% bridge-% web-ui-%

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
	$(MAKE) -C $(WEB_UI_DIR) check

lint: ## Run lint checks for repo, core, and web-ui
	$(MAKE) workflow-check
	$(MAKE) -C $(CORE_DIR) lint
	$(MAKE) -C $(WEB_UI_DIR) lint

test: ## Run tests in both core and web-ui
	$(MAKE) -C $(CORE_DIR) test
	$(MAKE) cli-test
	$(MAKE) -C $(WEB_UI_DIR) test

format: ## Apply formatting in both core and web-ui
	$(MAKE) -C $(CORE_DIR) fmt
	$(MAKE) -C $(WEB_UI_DIR) format

contract-gen: ## Regenerate OpenAPI-derived contract artifacts
	./scripts/contract-gen

contract-check: ## Verify generated contract artifacts are committed
	./scripts/contract-check

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

cli-test: ## Run CLI tests
	cd $(CLI_DIR) && go test ./...

CLI_VERSION ?= $(shell ./scripts/read-version.sh)

cli-build: ## Build CLI binary
	cd $(CLI_DIR) && go build -ldflags='-X organization-autorunner-cli/internal/buildinfo.Current=$(CLI_VERSION)' -o oar ./cmd/oar

cli-integration-test: ## Run CLI real-binary integration tests (non-default)
	cd $(CLI_DIR) && go test -tags=integration ./integration/...

bridge-setup: ## Set up the bridge-local Python 3.11 virtualenv and deps
	$(MAKE) -C $(BRIDGE_DIR) setup

bridge-doctor: ## Verify the bridge-local Python/runtime setup
	$(MAKE) -C $(BRIDGE_DIR) doctor

bridge-test: ## Run bridge unit tests
	$(MAKE) -C $(BRIDGE_DIR) test

e2e-smoke: ## Run end-to-end core + CLI + web-ui smoke flow
	./scripts/e2e-smoke

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
	FORCE_SEED="$(FORCE_SEED)" \
	./scripts/serve.sh

serve-control-plane: ## Start the SaaS control-plane service locally
	$(MAKE) -C $(CORE_DIR) serve-control-plane HOST="$(CORE_HOST)" PORT="$(CONTROL_PLANE_PORT)"

core-%: ## Pass-through target to core Makefile
	$(MAKE) -C $(CORE_DIR) $*

bridge-%: ## Pass-through target to adapter bridge Makefile
	$(MAKE) -C $(BRIDGE_DIR) $*

web-ui-%: ## Pass-through target to web-ui Makefile
	$(MAKE) -C $(WEB_UI_DIR) $*
