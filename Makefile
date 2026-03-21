SHELL := /usr/bin/env bash

CORE_DIR := core
CLI_DIR := cli
WEB_UI_DIR := web-ui

CORE_HOST ?= 127.0.0.1
CORE_PORT ?= 8000
CONTROL_PLANE_PORT ?= 8100
WEB_UI_PORT ?= 5173
CORE_BASE_URL ?= http://$(CORE_HOST):$(CORE_PORT)
SEED_CORE ?= 1
FORCE_SEED ?= 0

.DEFAULT_GOAL := help

.PHONY: help setup check serve serve-control-plane lint test format contract-gen contract-check e2e-smoke hosted-smoke hosted-ops-test hosted-ops-smoke saas-smoke saas-e2e saas-load-smoke cli-check cli-test cli-build cli-integration-test core-% web-ui-%

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"; printf "Targets:\n"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

setup: ## Install dependencies for web-ui, core, and cli
	pnpm install
	cd $(CORE_DIR) && go mod download
	cd $(CLI_DIR) && go mod download

check: ## Run checks in both core and web-ui
	$(MAKE) contract-check
	$(MAKE) -C $(CORE_DIR) check
	$(MAKE) cli-check
	$(MAKE) -C $(WEB_UI_DIR) check

lint: ## Run lint checks in both core and web-ui
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

docs-ref-audit: ## Audit agent-facing docs for broken local path references
	./scripts/docs-ref-audit

cli-check: ## Run CLI checks
	cd $(CLI_DIR) && go test ./...

cli-test: ## Run CLI tests
	cd $(CLI_DIR) && go test ./...

CLI_VERSION ?= dev

cli-build: ## Build CLI binary
	cd $(CLI_DIR) && go build -ldflags='-X organization-autorunner-cli/internal/httpclient.CLIVersion=$(CLI_VERSION)' -o oar ./cmd/oar

cli-integration-test: ## Run CLI real-binary integration tests (non-default)
	cd $(CLI_DIR) && go test -tags=integration ./integration/...

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

serve: ## Start core, seed mock dataset into core, then start web-ui
	@set -euo pipefail; \
	trap 'for pid in $$(jobs -p); do kill "$$pid" 2>/dev/null || true; done' EXIT INT TERM; \
	OAR_ALLOW_UNAUTHENTICATED_WRITES=1 $(MAKE) -C $(CORE_DIR) serve HOST="$(CORE_HOST)" PORT="$(CORE_PORT)" & \
	core_pid=$$!; \
	if [ "$(SEED_CORE)" = "1" ]; then \
		OAR_CORE_BASE_URL="$(CORE_BASE_URL)" OAR_FORCE_SEED="$(FORCE_SEED)" node "$(WEB_UI_DIR)/scripts/seed-core-from-mock.mjs"; \
	else \
		echo "Skipping core seed step (SEED_CORE=$(SEED_CORE))."; \
	fi; \
	OAR_CORE_BASE_URL="$(CORE_BASE_URL)" $(MAKE) -C $(WEB_UI_DIR) serve PORT="$(WEB_UI_PORT)" & \
	ui_pid=$$!; \
	wait $$core_pid $$ui_pid

serve-control-plane: ## Start the SaaS control-plane service locally
	$(MAKE) -C $(CORE_DIR) serve-control-plane HOST="$(CORE_HOST)" PORT="$(CONTROL_PLANE_PORT)"

core-%: ## Pass-through target to core Makefile
	$(MAKE) -C $(CORE_DIR) $*

web-ui-%: ## Pass-through target to web-ui Makefile
	$(MAKE) -C $(WEB_UI_DIR) $*
