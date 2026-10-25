#!/usr/bin/env bash
# Capture (or diff) qa-visual baselines inside the Playwright image CI uses,
# so committed baselines come from CI's fonts and rasterizer rather than the
# host's. Text-dense scenes drift past the diff threshold when captured on
# macOS and compared on Linux.
#
#   scripts/qa-baseline-container.sh --baseline --scene workspace-watch-lanes
#   scripts/qa-baseline-container.sh --diff
#
# The image tag is read from the web-ui-visual-check job in ci.yml. The tree
# is copied into the container (docker cp), so it works when the Docker VM
# does not share this checkout's path. With --baseline, the captured PNGs are
# copied back into tests/fixtures/qa-visual-baseline/.
#
# Docker Desktop and Colima run the image natively on Apple silicon (arm64);
# CI runs amd64. Node aborts under amd64 emulation, so arm64 is the default;
# the image's fonts and Chromium build are the same. Set QA_PLATFORM to
# override.
set -euo pipefail

web_ui="$(cd "$(dirname "$0")/.." && pwd)"
repo="$(cd "$web_ui/.." && pwd)"
image="$(grep -Eo 'mcr\.microsoft\.com/playwright:v[0-9.]+-[a-z]+' "$repo/.github/workflows/ci.yml" | head -n 1)"
[[ -n "$image" ]] || { echo "No Playwright image in ci.yml" >&2; exit 1; }
platform="${QA_PLATFORM:-linux/$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')}"
name="anx-qa-visual-$$"
base="$(basename "$repo")"
trap 'docker rm -f "$name" >/dev/null 2>&1 || true' EXIT

args=""
for arg in "$@"; do args+=" $(printf '%q' "$arg")"; done

docker create --name "$name" --platform "$platform" --ipc=host \
  -e PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
  -e ANX_TEST_REPORT_CHROMIUM_SANDBOX=false \
  -e CI=true \
  "$image" bash -c "set -euo pipefail
    cd /$base
    corepack enable >/dev/null
    pnpm install --frozen-lockfile --filter ./web-ui... >/dev/null
    cd web-ui
    node scripts/qa-visual.mjs$args" >/dev/null

# Everything but host build output and the host's own node_modules,
# extracted at /<checkout name> (docker cp needs an existing parent). No
# extended attributes: macOS adds ones Docker cannot set.
COPYFILE_DISABLE=1 tar --no-xattrs -C "$(dirname "$repo")" -cf - \
  --exclude node_modules --exclude .svelte-kit --exclude test-results \
  --exclude '.qa-*' --exclude build --exclude .git "$base" |
  docker cp - "$name:/"

docker start -a "$name"

if [[ " $* " == *" --baseline "* ]]; then
  docker cp "$name:/$base/web-ui/tests/fixtures/qa-visual-baseline/." \
    "$web_ui/tests/fixtures/qa-visual-baseline/"
  echo "Copied baselines into tests/fixtures/qa-visual-baseline/"
fi
