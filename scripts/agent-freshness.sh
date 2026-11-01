#!/bin/bash
# Read-only freshness check for session start: is this checkout behind what
# its upstream has, and does a parent repo record a different submodule
# revision? Prints only when something is stale; always exits 0. The fetch
# updates remote-tracking refs and nothing else, so the working tree never
# changes.

set -u

git rev-parse --git-dir >/dev/null 2>&1 || exit 0

# Submodule drift is a local comparison; it needs no network. `git submodule
# status` marks a submodule whose checkout differs from the recorded revision
# with a leading +, but the SHA on that line is the checkout's, so the recorded
# one comes from the parent's index.
superproject=$(git rev-parse --show-superproject-working-tree 2>/dev/null)
if [ -n "$superproject" ]; then
    while read -r entry path _rest; do
        case "$entry" in
            +*) recorded=$(git -C "$superproject" ls-files -s -- "$path" | cut -d' ' -f2)
                checked=${entry#+}
                if [ -n "$checked" ]; then
                    echo "agent-freshness: submodule $path is at ${checked:0:12} but the" \
                         "parent records ${recorded:0:12}; run git submodule update in the" \
                         "parent to match, or commit the new revision there if this bump is" \
                         "intentional"
                fi ;;
        esac
    done <<<"$(git -C "$superproject" submodule status 2>/dev/null)"
fi

# Fetch with a hard 10s ceiling: a SessionStart hook must not stall a session
# on a slow or absent network.
git fetch --quiet origin >/dev/null 2>&1 &
fetch_pid=$!
(sleep 10 && kill "$fetch_pid" 2>/dev/null) &
watchdog=$!
if wait "$fetch_pid"; then
    kill "$watchdog" 2>/dev/null
    wait "$watchdog" 2>/dev/null
else
    kill "$watchdog" 2>/dev/null
    wait "$watchdog" 2>/dev/null
    echo "agent-freshness: could not check for updates (git fetch failed or timed out)."
    exit 0
fi

upstream=$(git rev-parse --abbrev-ref --symbolic-full-name @{upstream} 2>/dev/null)
reported=""
if [ -n "$upstream" ]; then
    behind=$(git rev-list --count "HEAD..$upstream" 2>/dev/null)
    if [ "${behind:-0}" -gt 0 ]; then
        branch=$(git symbolic-ref --quiet --short HEAD 2>/dev/null || echo "(detached)")
        echo "agent-freshness: branch $branch is $behind commit(s) behind $upstream;" \
             "update with: git pull --ff-only"
        reported="$upstream"
    fi
fi

# On the default branch with no upstream report yet, compare with the remote
# default directly; detached and feature checkouts without an upstream stay
# silent.
default_remote=$(git symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>/dev/null)
if [ -z "$reported" ] && [ -n "$default_remote" ]; then
    current=$(git symbolic-ref --quiet HEAD 2>/dev/null)
    if [ "$current" = "refs/heads/${default_remote#origin/}" ]; then
        behind=$(git rev-list --count "HEAD..$default_remote" 2>/dev/null)
        if [ "${behind:-0}" -gt 0 ]; then
            echo "agent-freshness: on ${default_remote#origin/}, $behind commit(s) behind" \
                 "$default_remote; update with: git pull --ff-only"
        fi
    fi
fi

exit 0
