#!/usr/bin/env bash
# Stop stray processes left by `make serve`: listeners on core/web-ui dev ports,
# plus the optional local MinIO container used when ANX_DEV_BLOB_BACKEND=s3.
#
# Uses the same CORE_PORT / WEB_UI_PORT defaults as scripts/serve.sh. Override
# with env or `CORE_PORT=... WEB_UI_PORT=... make kill`.
set -uo pipefail

CORE_PORT="${CORE_PORT:-8000}"
WEB_UI_PORT="${WEB_UI_PORT:-5173}"
ANX_LOCAL_MINIO_CONTAINER_NAME="${ANX_LOCAL_MINIO_CONTAINER_NAME:-anx-dev-minio}"

_kill_port_watchers() {
	local port="$1"
	local label="$2"
	local pids p

	if ! command -v lsof >/dev/null 2>&1; then
		echo "serve-kill: lsof not found; cannot resolve listeners on port ${port} (${label}). Install lsof or stop those processes manually." >&2
		return 0
	fi

	pids=$(lsof -nP -iTCP:"${port}" -sTCP:LISTEN -t 2>/dev/null || true)
	if [[ -z "${pids}" ]]; then
		echo "serve-kill: no TCP listener on port ${port} (${label})."
		return 0
	fi

	p=$(echo "${pids}" | sort -u | tr '\n' ' ')
	echo "serve-kill: SIGTERM listener(s) on port ${port} (${label}): ${p}"

	while read -r pid; do
		[[ -n "${pid}" ]] || continue
		kill -TERM "${pid}" 2>/dev/null || true
	done < <(echo "${pids}" | sort -u)

	sleep 0.6

	pids=$(lsof -nP -iTCP:"${port}" -sTCP:LISTEN -t 2>/dev/null || true)
	if [[ -n "${pids}" ]]; then
		p=$(echo "${pids}" | sort -u | tr '\n' ' ')
		echo "serve-kill: SIGKILL stray listener(s) on port ${port} (${label}): ${p}"
		while read -r pid; do
			[[ -n "${pid}" ]] || continue
			kill -KILL "${pid}" 2>/dev/null || true
		done < <(echo "${pids}" | sort -u)
	fi
}

_kill_port_watchers "${CORE_PORT}" "anx-core"
_kill_port_watchers "${WEB_UI_PORT}" "web-ui (vite)"

if command -v docker >/dev/null 2>&1; then
	# Mirrors serve.sh cleanup: remove dev MinIO if present (ignore daemon errors).
	if docker rm -f "${ANX_LOCAL_MINIO_CONTAINER_NAME}" >/dev/null 2>&1; then
		echo "serve-kill: removed docker container ${ANX_LOCAL_MINIO_CONTAINER_NAME}."
	else
		echo "serve-kill: MinIO container ${ANX_LOCAL_MINIO_CONTAINER_NAME} not running (or docker unreachable)."
	fi
else
	echo "serve-kill: docker not installed; skipping MinIO container cleanup."
fi

echo "serve-kill: done."
