#!/usr/bin/env bash
# Local S3-compatible blob backend for OSS dev (MinIO). Sourced by serve.sh when
# ANX_DEV_BLOB_BACKEND=s3 (default). Requires Docker.
set -euo pipefail

ANX_LOCAL_MINIO_CONTAINER_NAME="${ANX_LOCAL_MINIO_CONTAINER_NAME:-anx-dev-minio}"
ANX_LOCAL_MINIO_PORT="${ANX_LOCAL_MINIO_PORT:-9000}"
ANX_LOCAL_MINIO_CONSOLE_PORT="${ANX_LOCAL_MINIO_CONSOLE_PORT:-9001}"
ANX_LOCAL_MINIO_NETWORK="${ANX_LOCAL_MINIO_NETWORK:-anx-dev-minio-net}"
ANX_LOCAL_MINIO_VOLUME="${ANX_LOCAL_MINIO_VOLUME:-anx-dev-minio-data}"
ANX_LOCAL_MINIO_ROOT_USER="${ANX_LOCAL_MINIO_ROOT_USER:-anxminio}"
ANX_LOCAL_MINIO_ROOT_PASSWORD="${ANX_LOCAL_MINIO_ROOT_PASSWORD:-anxminio-dev-local}"
ANX_BLOB_S3_BUCKET="${ANX_BLOB_S3_BUCKET:-anx-dev-workspace-blobs}"

# minio/mc image ENTRYPOINT is `mc`; do not pass `sh -lc ...` as args (that runs `mc sh`, which fails).
# MC_HOST_<alias> replaces `mc alias set` for ephemeral containers (each docker run has a fresh config).
anx_local_s3_mc_run() {
	local mc_endpoint="http://${ANX_LOCAL_MINIO_ROOT_USER}:${ANX_LOCAL_MINIO_ROOT_PASSWORD}@${ANX_LOCAL_MINIO_CONTAINER_NAME}:9000"
	docker run --rm --network "${ANX_LOCAL_MINIO_NETWORK}" \
		-e "MC_HOST_local=${mc_endpoint}" \
		minio/mc:latest "$@"
}

anx_local_s3_stop() {
	if command -v docker >/dev/null 2>&1; then
		docker rm -f "${ANX_LOCAL_MINIO_CONTAINER_NAME}" >/dev/null 2>&1 || true
	fi
}

anx_local_s3_reset_all() {
	if command -v docker >/dev/null 2>&1; then
		docker rm -f "${ANX_LOCAL_MINIO_CONTAINER_NAME}" >/dev/null 2>&1 || true
		docker volume rm "${ANX_LOCAL_MINIO_VOLUME}" >/dev/null 2>&1 || true
		docker network rm "${ANX_LOCAL_MINIO_NETWORK}" >/dev/null 2>&1 || true
	fi
}

anx_local_s3_reset_prefix() {
	local prefix="${1:-}"
	if [[ -z "${prefix}" ]] || ! command -v docker >/dev/null 2>&1; then
		return 0
	fi
	anx_local_s3_mc_run rm --recursive --force "local/${ANX_BLOB_S3_BUCKET}/${prefix}" >/dev/null 2>&1 || true
}

anx_local_s3_start() {
	if ! command -v docker >/dev/null 2>&1; then
		echo "local-dev-blob-s3: docker not found; use ANX_DEV_BLOB_BACKEND=filesystem" >&2
		return 1
	fi

	docker rm -f "${ANX_LOCAL_MINIO_CONTAINER_NAME}" >/dev/null 2>&1 || true
	docker network inspect "${ANX_LOCAL_MINIO_NETWORK}" >/dev/null 2>&1 ||
		docker network create --label anx.local_dev=s3-runtime "${ANX_LOCAL_MINIO_NETWORK}" >/dev/null
	docker volume inspect "${ANX_LOCAL_MINIO_VOLUME}" >/dev/null 2>&1 ||
		docker volume create --label anx.local_dev=s3-runtime "${ANX_LOCAL_MINIO_VOLUME}" >/dev/null

	docker run -d \
		--name "${ANX_LOCAL_MINIO_CONTAINER_NAME}" \
		--network "${ANX_LOCAL_MINIO_NETWORK}" \
		--label anx.local_dev=s3-runtime \
		-p "${ANX_LOCAL_MINIO_PORT}:9000" \
		-p "${ANX_LOCAL_MINIO_CONSOLE_PORT}:9001" \
		-e MINIO_ROOT_USER="${ANX_LOCAL_MINIO_ROOT_USER}" \
		-e MINIO_ROOT_PASSWORD="${ANX_LOCAL_MINIO_ROOT_PASSWORD}" \
		-v "${ANX_LOCAL_MINIO_VOLUME}:/data" \
		minio/minio:latest server /data --console-address ":9001" >/dev/null

	local _i
	for ((_i = 0; _i < 40; _i++)); do
		if curl -fsS "http://127.0.0.1:${ANX_LOCAL_MINIO_PORT}/minio/health/live" >/dev/null 2>&1; then
			break
		fi
		sleep 0.25
	done

	anx_local_s3_mc_run mb --ignore-existing "local/${ANX_BLOB_S3_BUCKET}"

	export ANX_BLOB_BACKEND=s3
	export ANX_BLOB_S3_BUCKET="${ANX_BLOB_S3_BUCKET}"
	export ANX_BLOB_S3_REGION="${ANX_BLOB_S3_REGION:-us-east-1}"
	export ANX_BLOB_S3_ENDPOINT="http://127.0.0.1:${ANX_LOCAL_MINIO_PORT}"
	export ANX_BLOB_S3_ACCESS_KEY_ID="${ANX_LOCAL_MINIO_ROOT_USER}"
	export ANX_BLOB_S3_SECRET_ACCESS_KEY="${ANX_LOCAL_MINIO_ROOT_PASSWORD}"
	export ANX_BLOB_S3_FORCE_PATH_STYLE="${ANX_BLOB_S3_FORCE_PATH_STYLE:-true}"
	export ANX_LOCAL_MINIO_STARTED=1
}
