#!/usr/bin/env bash
# Local SaaS packed-host dev stack: control plane + web UI + auto-started workspace cores.
# Invoked by `make serve-control-plane` from the repo root.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck disable=SC1091
source "${REPO_ROOT}/scripts/hosted/common.sh"

CONTROL_PLANE_PORT="${CONTROL_PLANE_PORT:-8100}"
WEB_UI_PORT="${WEB_UI_PORT:-5173}"
CONTROL_PLANE_WORKSPACE_ROOT="${CONTROL_PLANE_WORKSPACE_ROOT:-$REPO_ROOT/core/.oar-control-plane}"
RESET_CONTROL_PLANE_WORKSPACE="${RESET_CONTROL_PLANE_WORKSPACE:-1}"
SYNC_WORKSPACE_CORES="${SYNC_WORKSPACE_CORES:-1}"
SAAS_DEV_PACKED_LISTEN_START="${SAAS_DEV_PACKED_LISTEN_START:-18000}"
SAAS_DEV_PACKED_LISTEN_END="${SAAS_DEV_PACKED_LISTEN_END:-19990}"

UI_ORIGIN="${SAAS_DEV_UI_ORIGIN:-http://localhost:${WEB_UI_PORT}}"
CONTROL_PLANE_BASE_URL="${SAAS_DEV_CONTROL_PLANE_URL:-http://127.0.0.1:${CONTROL_PLANE_PORT}}"

CP_PID=""
UI_PID=""
SYNC_PID=""
CLEANUP_DONE=0

term_pid() {
	local pid="${1:-}"
	[[ -n "$pid" ]] || return 0
	kill -TERM "$pid" 2>/dev/null || true
}

wait_pid() {
	local pid="${1:-}"
	local max="${2:-5}"
	local i
	[[ -n "$pid" ]] || return 0
	for ((i = 0; i < max * 10; i++)); do
		kill -0 "$pid" 2>/dev/null || return 0
		sleep 0.1
	done
	kill -KILL "$pid" 2>/dev/null || true
}

cleanup() {
	if [[ "$CLEANUP_DONE" == "1" ]]; then
		return
	fi
	CLEANUP_DONE=1
	trap '' INT TERM
	trap - EXIT

	echo "Stopping SaaS dev stack..." >&2
	term_pid "${SYNC_PID:-}"
	wait_pid "${SYNC_PID:-}" 2

	local pid_dir="${CONTROL_PLANE_WORKSPACE_ROOT}/.dev-synced-cores"
	if [[ -d "$pid_dir" ]]; then
		shopt -s nullglob
		local f pid
		for f in "$pid_dir"/*.pid; do
			pid="$(tr -d '[:space:]' <"$f" 2>/dev/null || true)"
			[[ -n "$pid" ]] || continue
			term_pid "$pid"
			wait_pid "$pid" 3
		done
		shopt -u nullglob
	fi

	term_pid "${UI_PID:-}"
	term_pid "${CP_PID:-}"
	wait_pid "${UI_PID:-}" 5
	wait_pid "${CP_PID:-}" 5
}

trap cleanup EXIT INT TERM

require_command curl go pnpm node sqlite3 openssl

if command -v lsof >/dev/null 2>&1; then
	if lsof -nP -iTCP:"${CONTROL_PLANE_PORT}" -sTCP:LISTEN >/dev/null 2>&1; then
		die "port ${CONTROL_PLANE_PORT} is already in use (control plane)"
	fi
	if lsof -nP -iTCP:"${WEB_UI_PORT}" -sTCP:LISTEN >/dev/null 2>&1; then
		die "port ${WEB_UI_PORT} is already in use (web UI)"
	fi
fi

write_saas_dev_keypair_file() {
	local dest="$REPO_ROOT/core/.oar-dev-saas-ed25519"
	local tmpgo
	tmpgo="$(mktemp "${TMPDIR:-/tmp}/oar-ed25519.XXXXXX.go")"
	cat >"$tmpgo" <<'EOF'
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
)

func main() {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	fmt.Println(base64.StdEncoding.EncodeToString(publicKey))
	fmt.Println(base64.StdEncoding.EncodeToString(privateKey))
}
EOF
	umask 077
	(cd "$REPO_ROOT/core" && go run "$tmpgo") >"$dest"
	rm -f "$tmpgo"
	chmod 600 "$dest"
}

load_saas_dev_keypair_file() {
	local dest="$REPO_ROOT/core/.oar-dev-saas-ed25519"
	SERVICE_PUBLIC_KEY="$(sed -n '1p' "$dest" | tr -d '[:space:]')"
	SERVICE_PRIVATE_KEY="$(sed -n '2p' "$dest" | tr -d '[:space:]')"
}

SERVICE_PUBLIC_KEY=""
SERVICE_PRIVATE_KEY=""
if [[ ! -f "$REPO_ROOT/core/.oar-dev-saas-ed25519" ]]; then
	write_saas_dev_keypair_file
fi
load_saas_dev_keypair_file

if [[ -z "${OAR_SECRETS_KEY:-}" ]]; then
	OAR_SECRETS_KEY_FILE="${OAR_SECRETS_KEY_FILE:-$REPO_ROOT/core/.oar-dev-secrets-key}"
	if [[ -f "${OAR_SECRETS_KEY_FILE}" ]]; then
		OAR_SECRETS_KEY="$(tr -d '[:space:]' <"${OAR_SECRETS_KEY_FILE}")"
	fi
	if [[ -z "${OAR_SECRETS_KEY:-}" ]] || [[ ! "${OAR_SECRETS_KEY:-}" =~ ^[0-9a-fA-F]{64}$ ]]; then
		OAR_SECRETS_KEY="$(openssl rand -hex 32)"
		umask 077
		printf '%s\n' "${OAR_SECRETS_KEY}" >"${OAR_SECRETS_KEY_FILE}"
	fi
	export OAR_SECRETS_KEY
fi

if [[ "$RESET_CONTROL_PLANE_WORKSPACE" == "1" ]]; then
	echo "Clearing control-plane workspace (RESET_CONTROL_PLANE_WORKSPACE=0 to keep): ${CONTROL_PLANE_WORKSPACE_ROOT}" >&2
	rm -rf "${CONTROL_PLANE_WORKSPACE_ROOT}"
fi

mkdir -p "${CONTROL_PLANE_WORKSPACE_ROOT}"

CORE_BIN="$(build_core_binary "$REPO_ROOT/core/.bin/oar-core")"
SCHEMA_PATH="$(resolve_schema_path)"

echo "Starting oar-control-plane on ${CONTROL_PLANE_BASE_URL} ..." >&2
(
	cd "$REPO_ROOT/core"
	export OAR_CONTROL_PLANE_WEBAUTHN_RPID="localhost"
	export OAR_CONTROL_PLANE_WEBAUTHN_ORIGIN="$UI_ORIGIN"
	export OAR_CONTROL_PLANE_WORKSPACE_GRANT_ISSUER="$CONTROL_PLANE_BASE_URL"
	export OAR_CONTROL_PLANE_WORKSPACE_GRANT_AUDIENCE="oar-core"
	export OAR_CONTROL_PLANE_WORKSPACE_GRANT_SIGNING_KEY="$SERVICE_PRIVATE_KEY"
	export OAR_CONTROL_PLANE_WORKSPACE_URL_TEMPLATE="${UI_ORIGIN}/%s"
	export OAR_CONTROL_PLANE_INVITE_URL_TEMPLATE="${CONTROL_PLANE_BASE_URL}/invites/%s"
	export OAR_CONTROL_PLANE_DEV_PACKED_LISTEN_START="$SAAS_DEV_PACKED_LISTEN_START"
	export OAR_CONTROL_PLANE_DEV_PACKED_LISTEN_END="$SAAS_DEV_PACKED_LISTEN_END"
	exec go run ./cmd/oar-control-plane \
		--listen-addr "127.0.0.1:${CONTROL_PLANE_PORT}" \
		--workspace-root "$CONTROL_PLANE_WORKSPACE_ROOT"
) &
CP_PID=$!

if ! wait_for_http_ok "${CONTROL_PLANE_BASE_URL}/readyz" 90; then
	die "control plane did not become ready; check logs (PID ${CP_PID})"
fi

echo "" >&2
echo "SaaS dev workspace signing (paste into dashboard when creating a workspace):" >&2
echo "  Service identity public key: ${SERVICE_PUBLIC_KEY}" >&2
echo "  Pick any unique service identity id per workspace (example: dev-local-1)." >&2
echo "Open the UI at ${UI_ORIGIN} (use http://localhost — not 127.0.0.1 — so passkeys match RP id localhost)." >&2
echo "" >&2

workspace_core_sync_loop() {
	local db="$CONTROL_PLANE_WORKSPACE_ROOT/state.sqlite"
	local pid_dir="$CONTROL_PLANE_WORKSPACE_ROOT/.dev-synced-cores"
	mkdir -p "$pid_dir" "${CONTROL_PLANE_WORKSPACE_ROOT}/.dev-core-logs"
	while true; do
		if [[ -f "$db" ]]; then
			local row
			while IFS='|' read -r ws_id deployment_root listen_port instance_id service_id pub_key; do
				[[ -n "$ws_id" ]] || continue
				pub_key="$(echo "$pub_key" | tr -d '[:space:]')"
				if [[ "$pub_key" != "$SERVICE_PUBLIC_KEY" ]]; then
					continue
				fi
				local ws_root="${deployment_root}/workspace"
				[[ -d "$ws_root" ]] || continue
				if curl -fsS "http://127.0.0.1:${listen_port}/readyz" >/dev/null 2>&1; then
					continue
				fi
				local pid_file="${pid_dir}/${ws_id}.pid"
				if [[ -f "$pid_file" ]]; then
					local oldpid
					oldpid="$(tr -d '[:space:]' <"$pid_file" || true)"
					if [[ -n "$oldpid" ]] && kill -0 "$oldpid" 2>/dev/null; then
						continue
					fi
					rm -f "$pid_file"
				fi
				local envf="${deployment_root}/config/env.production"
				local bootstrap
				if [[ -f "$envf" ]]; then
					bootstrap="$(grep -E '^OAR_BOOTSTRAP_TOKEN=' "$envf" | head -1 | cut -d= -f2- | tr -d '\r' || true)"
				fi
				if [[ -z "$bootstrap" ]] || [[ "$bootstrap" == *REPLACE* ]]; then
					bootstrap="$(openssl rand -hex 24)"
				fi
				local logf="${CONTROL_PLANE_WORKSPACE_ROOT}/.dev-core-logs/${ws_id}.log"
				(
					export OAR_HUMAN_AUTH_MODE="control_plane"
					export OAR_CONTROL_PLANE_TOKEN_ISSUER="$CONTROL_PLANE_BASE_URL"
					export OAR_CONTROL_PLANE_TOKEN_AUDIENCE="oar-core"
					export OAR_CONTROL_PLANE_TOKEN_PUBLIC_KEY="$SERVICE_PUBLIC_KEY"
					export OAR_CONTROL_PLANE_WORKSPACE_ID="$ws_id"
					export OAR_WORKSPACE_SERVICE_ID="$service_id"
					export OAR_WORKSPACE_SERVICE_PRIVATE_KEY="$SERVICE_PRIVATE_KEY"
					export OAR_BOOTSTRAP_TOKEN="$bootstrap"
					export OAR_SECRETS_KEY
					exec "$CORE_BIN" \
						--listen-addr "127.0.0.1:${listen_port}" \
						--schema-path "$SCHEMA_PATH" \
						--workspace-root "$ws_root" \
						--core-instance-id "$instance_id"
				) >"$logf" 2>&1 &
				echo $! >"$pid_file"
			done < <(sqlite3 -separator '|' "$db" "SELECT id, deployment_root, listen_port, COALESCE(NULLIF(trim(instance_id),''), id), service_identity_id, service_identity_public_key FROM workspaces WHERE status='ready' AND trim(coalesce(deployment_root,'')) != '' AND listen_port > 0;" 2>/dev/null || true)
		fi
		sleep 3
	done
}

if [[ "$SYNC_WORKSPACE_CORES" == "1" ]]; then
	workspace_core_sync_loop &
	SYNC_PID=$!
fi

echo "Starting web-ui (Vite) on ${UI_ORIGIN} ..." >&2
(
	cd "$REPO_ROOT/web-ui"
	unset OAR_CORE_BASE_URL 2>/dev/null || true
	unset OAR_WORKSPACES 2>/dev/null || true
	export HOST="${HOST:-127.0.0.1}"
	export PORT="$WEB_UI_PORT"
	export ORIGIN="$UI_ORIGIN"
	export OAR_CONTROL_BASE_URL="$CONTROL_PLANE_BASE_URL"
	export OAR_SAAS_PACKED_HOST_DEV=1
	exec ./scripts/dev
) &
UI_PID=$!

wait "$CP_PID" "$UI_PID"
