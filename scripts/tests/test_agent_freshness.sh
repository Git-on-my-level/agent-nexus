#!/usr/bin/env bash
# Behavioral checks for the session-start freshness script: silent when fresh,
# one plain line when behind, one when the network is unusable, always exit 0.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
FRESHNESS="$ROOT/scripts/agent-freshness.sh"

# The host's git wrappers must not reach the fixture repositories.
while IFS='=' read -r name _; do
    case "$name" in GIT_*) unset "$name" ;; esac
done < <(env)
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null

tmp="$(mktemp -d "${TMPDIR:-/tmp}/anx-freshness.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

g() { git -C "$1" "${@:2}"; }

fixture() {  # fixture <dir>: a repo with one commit and a committed identity
    git init -q -b main "$1"
    git -C "$1" config user.name "freshness tests"
    # A resolvable domain: managed-git hosts refuse reserved-TLD identities in
    # repos that carry remotes, and the clone fixture below has one.
    git -C "$1" config user.email "freshness@example.com"
    echo base > "$1/file"
    git -C "$1" add .
    git -C "$1" commit -qm base
}

failed=0
expect() {  # expect <description> <want-output?> <substring>... ; runs on cwd
    local description="$1" want_output="$2"
    shift 2
    local output status
    output="$(bash "$FRESHNESS")" && status=0 || status=$?
    if [ "$status" -ne 0 ]; then
        echo "FAIL: $description exited $status (must always be 0)"; failed=1; return
    fi
    if [ "$want_output" = silent ] && [ -n "$output" ]; then
        echo "FAIL: $description printed output while up to date: $output"; failed=1; return
    fi
    local substring
    for substring in "$@"; do
        if ! grep -q "$substring" <<<"$output"; then
            echo "FAIL: $description output missing '$substring': $output"; failed=1; return
        fi
    done
    echo "ok: $description"
}

git init -q --bare -b main "$tmp/origin.git"
fixture "$tmp/work"
git -C "$tmp/work" remote add origin "$tmp/origin.git"
git -C "$tmp/work" push -q -u origin main

git clone -q "$tmp/origin.git" "$tmp/clone"
git -C "$tmp/clone" config user.name "freshness tests"
git -C "$tmp/clone" config user.email "freshness@example.com"

# Up to date: silent.
cd "$tmp/clone"
expect "up to date" silent

# Ahead of upstream only: still silent.
echo local > new-file
git add new-file
git commit -qm local
expect "ahead of upstream" silent

# Behind by one: one line naming the branch, the count and the fix.
git -C "$tmp/work" push -q origin main:main 2>/dev/null || true
echo upstream > "$tmp/work/file2"
git -C "$tmp/work" add file2
git -C "$tmp/work" commit -qm upstream
git -C "$tmp/work" push -q origin main
expect "behind upstream" output "behind origin/main" "git pull --ff-only"
# Read-only: the fetch must not touch the working tree or local branches.
[ -z "$(git status --porcelain)" ] || { echo "FAIL: freshness modified the working tree"; failed=1; }

# No network (unreachable remote path): a single could-not-check line.
git remote set-url origin "$tmp/does-not-exist.git"
expect "fetch failure" output "could not check"
git remote set-url origin "$tmp/origin.git"

# Submodule drift: the parent records an older revision than is checked out.
# The clone's local-ahead commit has served its purpose; drop it so the
# fixture history stays linear.
git -C "$tmp/clone" fetch -q origin
git -C "$tmp/clone" reset -q --hard origin/main
fixture "$tmp/submodule"
git init -q -b main "$tmp/parent"
git -C "$tmp/parent" config user.name "freshness tests"
git -C "$tmp/parent" config user.email "freshness@example.com"
git -C "$tmp/parent" -c protocol.file.allow=always submodule add -q "$tmp/submodule" sub
git -C "$tmp/parent" commit -qm "add submodule"
recorded=$(git -C "$tmp/parent" rev-parse HEAD:sub | cut -c1-12)
# Advance the submodule checkout past what the parent has recorded.
cd "$tmp/parent/sub"
git config user.name "freshness tests"
git config user.email "freshness@example.com"
echo drift > drift
git add drift
git commit -qm drift
checked=$(git rev-parse HEAD | cut -c1-12)
expect "submodule drift" output "submodule sub is at $checked" "parent records $recorded"

if [ "$failed" -ne 0 ]; then
    echo "freshness checks FAILED"
    exit 1
fi
echo "all freshness checks passed"
