SHELL := /usr/bin/env bash

PYTHON ?= python3.11
VENV ?= .venv
PYTHON_BIN := $(VENV)/bin/python
BRIDGE_BIN := $(VENV)/bin/anx-agent-bridge
CONFIG ?= examples/subprocess.toml

.DEFAULT_GOAL := help

.PHONY: help setup doctor test smoke bridge-run

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"; printf "Targets:\n"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

setup: ## Create the local Python 3.11 venv and install bridge deps
	$(PYTHON) -m venv $(VENV)
	$(PYTHON_BIN) -m pip install --upgrade pip
	$(PYTHON_BIN) -m pip install -e .[dev]

doctor: ## Verify the local bridge runtime and version
	$(PYTHON_BIN) -c "import sys; assert sys.version_info >= (3, 11), sys.version"
	$(BRIDGE_BIN) --version

test: ## Run bridge unit tests
	$(PYTHON_BIN) -m pytest tests

smoke: doctor ## Run a cheap bridge smoke check
	$(BRIDGE_BIN) --help >/dev/null
	$(BRIDGE_BIN) registration status --config $(CONFIG) || true

bridge-run: ## Run the agent bridge with CONFIG=<path>
	$(BRIDGE_BIN) bridge run --config $(CONFIG)
