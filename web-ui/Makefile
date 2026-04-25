SHELL := /usr/bin/env bash

PNPM ?= pnpm
PORT ?= 5173

.DEFAULT_GOAL := help

.PHONY: help install serve check format unit-test test e2e qa qa-capture qa-baseline qa-diff qa-viewports

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"; printf "Targets:\n"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

install: ## Install dependencies
	$(PNPM) install

serve: ## Run local development server
	PORT=$(PORT) ./scripts/dev

check: ## Run cheap checks (eslint, prettier --check, unit tests)
	$(PNPM) run lint
	$(PNPM) run test:unit
	node scripts/audit-core-proxy-paths.mjs

format: ## Apply formatting
	$(PNPM) run format

unit-test: ## Run unit tests
	$(PNPM) run test:unit

e2e: ## Run Playwright e2e tests
	$(PNPM) run test:e2e

test: ## Run full test suite (lint + unit + e2e)
	$(PNPM) run test

qa: qa-capture ## Alias for qa-capture

qa-capture: ## Capture QA screenshots of all pages (requires running dev server)
	node scripts/qa-capture.mjs $(QA_CAPTURE_ARGS)

qa-baseline: ## Capture the committed QA baseline into tests/fixtures/qa-visual-baseline/
	node scripts/qa-capture.mjs --baseline

qa-diff: ## Diff current UI against tests/fixtures/qa-visual-baseline/
	$(PNPM) run qa:diff

qa-viewports: ## Dashboard + inbox screenshots at several widths (requires running dev server)
	for size in 1440x900 1280x900 1120x900 1024x768 900x900; do \
		node scripts/qa-capture.mjs --viewport $$size --routes home,inbox --no-axe --no-detail || exit 1; \
	done
