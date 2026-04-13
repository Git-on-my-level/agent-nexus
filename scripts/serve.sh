#!/usr/bin/env bash
# Start oar-core + web-ui for local development. Invoked by `make serve`.
# Stops the full process tree on EXIT/INT/TERM so Ctrl+C does not leave
# oar-core (or Vite) bound to dev ports.

set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
CORE_HOST="${CORE_HOST:-127.0.0.1}"
CORE_PORT="${CORE_PORT:-8000}"
CORE_BASE_URL="${CORE_BASE_URL:-http://${CORE_HOST}:${CORE_PORT}}"
CORE_WORKSPACE_ROOT="${CORE_WORKSPACE_ROOT:-${REPO_ROOT}/core/.oar-workspace}"
WEB_UI_PORT="${WEB_UI_PORT:-5173}"
RESET_DEV_WORKSPACE="${RESET_DEV_WORKSPACE:-1}"
SEED_CORE="${SEED_CORE:-1}"
FORCE_SEED="${FORCE_SEED:-0}"
DEV_SEED_SCENARIO="${DEV_SEED_SCENARIO:-default}"

CORE_PID=""
UI_PID=""
CLEANUP_DONE=0
# After SIGTERM, how long to wait for oar-core (HTTP Shutdown + SQLite) and Vite before SIGKILL.
CORE_TERM_WAIT_SEC="${CORE_TERM_WAIT_SEC:-5}"
UI_TERM_WAIT_SEC="${UI_TERM_WAIT_SEC:-3}"

# Post-order listing: children before parent (safe kill order).
snapshot_tree() {
	local pid=$1
	local c
	for c in $(pgrep -P "$pid" 2>/dev/null); do
		snapshot_tree "$c"
	done
	echo "$pid"
}

# Return 0 if pid is still running.
pid_alive() {
	[ -n "$1" ] && kill -0 "$1" 2>/dev/null
}

# Send SIGTERM to pid and its descendants (deepest first).
term_tree() {
	local root=$1
	local snap p
	if ! pid_alive "$root"; then
		return 0
	fi
	snap=$(snapshot_tree "$root")
	for p in $snap; do
		[ -n "$p" ] || continue
		kill -TERM "$p" 2>/dev/null || true
	done
}

# Send SIGKILL to pid and its descendants (deepest first).
kill_tree() {
	local root=$1
	local snap p
	if ! pid_alive "$root"; then
		return 0
	fi
	snap=$(snapshot_tree "$root")
	for p in $snap; do
		[ -n "$p" ] || continue
		kill -KILL "$p" 2>/dev/null || true
	done
}

# Wait up to $2 seconds for $1 to exit (after SIGTERM).
wait_pid_exit() {
	local pid=$1
	local max_sec=$2
	local _i
	[ -n "$pid" ] || return 0
	for ((_i = 0; _i < max_sec * 10; _i++)); do
		pid_alive "$pid" || return 0
		sleep 0.1
	done
	return 1
}

cleanup() {
	if [ "$CLEANUP_DONE" = "1" ]; then
		return
	fi
	CLEANUP_DONE=1
	# Avoid a second signal aborting teardown mid-flight (orphan processes / stuck port).
	trap '' INT TERM
	trap - EXIT

	if [ -n "${CORE_PID}" ] || [ -n "${UI_PID}" ]; then
		echo "Stopping dev servers..." >&2
	fi

	term_tree "${CORE_PID}"
	term_tree "${UI_PID}"

	# Give oar-core time for graceful HTTP shutdown and DB close; Vite usually exits quickly.
	wait_pid_exit "${CORE_PID}" "${CORE_TERM_WAIT_SEC}" || true
	wait_pid_exit "${UI_PID}" "${UI_TERM_WAIT_SEC}" || true

	kill_tree "${CORE_PID}"
	kill_tree "${UI_PID}"

	if [ -n "${CORE_PID}" ]; then
		wait "${CORE_PID}" 2>/dev/null || true
	fi
	if [ -n "${UI_PID}" ]; then
		wait "${UI_PID}" 2>/dev/null || true
	fi
}

trap cleanup EXIT INT TERM

# If CORE_PORT is already bound (often another oar-core from a different terminal),
# `go run` exits after failing to listen while this script still seeds against the
# existing listener — producing confusing errors (e.g. actor_id is not registered).
if command -v lsof >/dev/null 2>&1; then
	if lsof -nP -iTCP:"${CORE_PORT}" -sTCP:LISTEN >/dev/null 2>&1; then
		echo "Port ${CORE_PORT} is already in use. Stop the process listening there (often: oar-core) or set CORE_PORT to a free port." >&2
		exit 1
	fi
fi

if [ "$RESET_DEV_WORKSPACE" = "1" ] && [ "$SEED_CORE" = "1" ]; then
	echo "Clearing dev workspace (mock seed will repopulate): ${CORE_WORKSPACE_ROOT}"
	rm -rf "${CORE_WORKSPACE_ROOT}"
fi

# Workspace secrets (/secrets) need OAR_SECRETS_KEY (32 bytes as 64 hex chars).
# If unset, load or create a stable repo-local dev key so restarts keep decrypting
# the same SQLite workspace. Override with OAR_SECRETS_KEY or OAR_SECRETS_KEY_FILE.
if [ -z "${OAR_SECRETS_KEY:-}" ]; then
	OAR_SECRETS_KEY_FILE="${OAR_SECRETS_KEY_FILE:-${REPO_ROOT}/core/.oar-dev-secrets-key}"
	if [ -f "${OAR_SECRETS_KEY_FILE}" ]; then
		OAR_SECRETS_KEY="$(tr -d '[:space:]' <"${OAR_SECRETS_KEY_FILE}")"
	fi
	if [ -z "${OAR_SECRETS_KEY:-}" ] || [[ ! "${OAR_SECRETS_KEY:-}" =~ ^[0-9a-fA-F]{64}$ ]]; then
		if ! command -v openssl >/dev/null 2>&1; then
			echo "OAR_SECRETS_KEY is unset and openssl is not available to generate one." >&2
			exit 1
		fi
		OAR_SECRETS_KEY="$(openssl rand -hex 32)"
		umask 077
		printf '%s\n' "${OAR_SECRETS_KEY}" >"${OAR_SECRETS_KEY_FILE}"
	fi
	export OAR_SECRETS_KEY
fi

OAR_ALLOW_UNAUTHENTICATED_WRITES="${OAR_ALLOW_UNAUTHENTICATED_WRITES:-1}"
export OAR_ALLOW_UNAUTHENTICATED_WRITES
OAR_ALLOW_PASSKEY_DEV_BYPASS="${OAR_ALLOW_PASSKEY_DEV_BYPASS:-1}"
export OAR_ALLOW_PASSKEY_DEV_BYPASS

OAR_BOOTSTRAP_TOKEN="${OAR_BOOTSTRAP_TOKEN:-oar-dev-bootstrap-token}"
export OAR_BOOTSTRAP_TOKEN
OAR_WORKSPACE_ID="${OAR_WORKSPACE_ID:-ws_main}"
export OAR_WORKSPACE_ID
OAR_DEV_REGISTER_LINKED_ACTORS="${OAR_DEV_REGISTER_LINKED_ACTORS:-1}"
export OAR_DEV_REGISTER_LINKED_ACTORS

HOST="${CORE_HOST}" \
	PORT="${CORE_PORT}" \
	WORKSPACE_ROOT="${CORE_WORKSPACE_ROOT}" \
	"${REPO_ROOT}/core/scripts/dev" &
CORE_PID=$!

# CLI dogfood: drop stale invite bundles before seed repopulates them.
CLI_DOGFOOD_DIR="${REPO_ROOT}/cli/dogfood-resources"
mkdir -p "${CLI_DOGFOOD_DIR}"
rm -f "${CLI_DOGFOOD_DIR}"/*.generated.json

if [ "$SEED_CORE" = "1" ]; then
	OAR_BOOTSTRAP_TOKEN="${OAR_BOOTSTRAP_TOKEN}" \
		OAR_CLI_DOGFOOD_RESOURCES_DIR="${CLI_DOGFOOD_DIR}" \
		OAR_CORE_BASE_URL="${CORE_BASE_URL}" \
		OAR_DEV_SEED_SCENARIO="${DEV_SEED_SCENARIO}" \
		OAR_DEV_SEED_IDENTITIES="${OAR_DEV_SEED_IDENTITIES:-1}" \
		OAR_FORCE_SEED="${FORCE_SEED}" \
		node "${REPO_ROOT}/web-ui/scripts/seed-core-from-mock.mjs"
else
	echo "Skipping core seed step (SEED_CORE=${SEED_CORE})."
fi

# Explicit `local` workspace for web-ui: `serve.sh` historically set only
# OAR_CORE_BASE_URL, which triggers a synthetic default workspace when
# OAR_WORKSPACES is unset. That breaks common dev cases:
# - Shell / CI exports OAR_WORKSPACES without a `local` entry; the URL slug
#   still defaults to /local/... → "not configured".
# - A control-plane session merges in SaaS workspaces and clears synthetic
#   static entries, removing `local` from the catalog.
# Override with SERVE_UI_OAR_WORKSPACES='[...]' for custom multi-core dev.
SERVE_UI_OAR_WORKSPACES="${SERVE_UI_OAR_WORKSPACES:-[{\"slug\":\"local\",\"label\":\"Local\",\"coreBaseUrl\":\"${CORE_BASE_URL}\"}]}"

(
	cd "${REPO_ROOT}/web-ui"
	OAR_DEV_ACTOR_MODE="${OAR_DEV_ACTOR_MODE:-1}" \
		OAR_CORE_BASE_URL="${CORE_BASE_URL}" \
		OAR_WORKSPACES="${SERVE_UI_OAR_WORKSPACES}" \
		PORT="${WEB_UI_PORT}" \
		./scripts/dev
) &
UI_PID=$!

wait "${CORE_PID}" "${UI_PID}"
