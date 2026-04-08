SHELL := /usr/bin/env bash

PNPM ?= pnpm
PORT ?= 5173

.DEFAULT_GOAL := help

.PHONY: help install serve check format unit-test test e2e

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
