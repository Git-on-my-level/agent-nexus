#!/usr/bin/env bash
# Compile the dogfood C transform, then stage → validate → canary → activate
# it under the managed JIT root used by make serve.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

STATE_ROOT="${ANX_JIT_STATE_ROOT:-$ROOT_DIR/.anx-workspace-jit}"
DEV="$ROOT_DIR/dev"
mkdir -p "$STATE_ROOT"
chmod 700 "$STATE_ROOT"
STATE_ROOT="$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$STATE_ROOT")"
chmod 700 "$STATE_ROOT"

echo "dogfood JIT: compiling $DEV/github-transform.c"
go run ./cmd/anx-observe \
	--state-root "$STATE_ROOT" \
	--policy "$DEV/jit-policy.json" \
	--manifest "$DEV/github-jit-manifest.json" \
	--source "$DEV/github-transform.c" \
	jit-generate | tee /tmp/anx-jit-stage.json

REV="$(python3 -c 'import json,sys; print(json.load(open("/tmp/anx-jit-stage.json"))["revision"])')"
echo "dogfood JIT: revision $REV"

STATUS="$(go run ./cmd/anx-observe --state-root "$STATE_ROOT" --policy "$DEV/jit-policy.json" --adapter github-issue-transform jit-status)"
STATE="$(python3 -c 'import json,sys; print(json.loads(sys.argv[1]).get("versions",{}).get(sys.argv[2],{}).get("state",""))' "$STATUS" "$REV")"

if [ "$STATE" = "active" ]; then
	echo "dogfood JIT: already active"
	exit 0
fi

if [ "$STATE" = "staged" ]; then
	go run ./cmd/anx-observe \
		--state-root "$STATE_ROOT" \
		--policy "$DEV/jit-policy.json" \
		--adapter github-issue-transform \
		--revision "$REV" \
		--fixtures "$DEV/github-jit-fixtures.json" \
		jit-validate
	STATE=validated
fi

if [ "$STATE" = "validated" ]; then
	go run ./cmd/anx-observe \
		--state-root "$STATE_ROOT" \
		--policy "$DEV/jit-policy.json" \
		--adapter github-issue-transform \
		--revision "$REV" \
		--config "$DEV/github-source.json" \
		jit-canary
	STATE=canaried
fi

if [ "$STATE" = "canaried" ]; then
	go run ./cmd/anx-observe \
		--state-root "$STATE_ROOT" \
		--policy "$DEV/jit-policy.json" \
		--adapter github-issue-transform \
		--revision "$REV" \
		jit-activate
fi

go run ./cmd/anx-observe --state-root "$STATE_ROOT" --policy "$DEV/jit-policy.json" --adapter github-issue-transform jit-status
echo "dogfood JIT: activated $REV"
