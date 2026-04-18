#!/usr/bin/env bash
set -euo pipefail

# --------------------------------------------------------------------------- #
# install-anx-core.sh — install anx-core binary + assets, optionally generate
# a launchd plist for a named instance.
#
# Two modes:
#   1. Install mode (default): build (or copy) binary + schema assets to PREFIX.
#   2. Instance mode (--instance): generate a launchd plist for a named
#      instance pointing at the installed binary.
#
# Both modes can run together in a single invocation.
# --------------------------------------------------------------------------- #

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

usage() {
    cat <<'EOF'
Usage: install-anx-core.sh [options]

Install:
  --prefix DIR          Install prefix (default: ~/.anx)
  --skip-build          Don't build; use binary from --binary
  --binary PATH         Pre-built binary path (default: builds from source)

Instance (launchd plist generation):
  --instance NAME       Instance name (e.g. "team-alpha")
  --port PORT           Listen port for this instance (default: 8001)
  --workspace DIR       Workspace root (default: <prefix>/workspaces/<instance>)
  --log-dir DIR         Log directory (default: <prefix>/logs)
  --plist-dir DIR       Plist output directory (default: ~/Library/LaunchAgents)
  --load                Load the plist into launchd after writing

Miscellaneous:
  --unload INSTANCE     Unload and remove plist for the named instance
  -h, --help            Show this help

Examples:
  # Build and install binary + assets:
  ./scripts/install-anx-core.sh

  # Install + create an instance in one shot:
  ./scripts/install-anx-core.sh --instance prod-1 --port 8001 --load

  # Add a second instance (binary already installed):
  ./scripts/install-anx-core.sh --skip-build --instance prod-2 --port 8002 --load

  # Tear down an instance:
  ./scripts/install-anx-core.sh --unload prod-1
EOF
}

# ── Defaults ─────────────────────────────────────────────────────────────── #

PREFIX="${HOME}/.anx"
SKIP_BUILD=0
BINARY=""
INSTANCE=""
PORT=8001
WORKSPACE=""
LOG_DIR=""
PLIST_DIR="${HOME}/Library/LaunchAgents"
LOAD=0
UNLOAD_INSTANCE=""

# ── Parse args ───────────────────────────────────────────────────────────── #

while [[ $# -gt 0 ]]; do
    case "$1" in
        --prefix)       PREFIX="$2";           shift 2 ;;
        --skip-build)   SKIP_BUILD=1;          shift ;;
        --binary)       BINARY="$2";           shift 2 ;;
        --instance)     INSTANCE="$2";         shift 2 ;;
        --port)         PORT="$2";             shift 2 ;;
        --workspace)    WORKSPACE="$2";        shift 2 ;;
        --log-dir)      LOG_DIR="$2";          shift 2 ;;
        --plist-dir)    PLIST_DIR="$2";        shift 2 ;;
        --load)         LOAD=1;                shift ;;
        --unload)       UNLOAD_INSTANCE="$2";  shift 2 ;;
        -h|--help)      usage; exit 0 ;;
        *)
            echo >&2 "Unknown option: $1"
            usage >&2
            exit 1
            ;;
    esac
done

# ── Unload mode ──────────────────────────────────────────────────────────── #

if [[ -n "$UNLOAD_INSTANCE" ]]; then
    plist_path="${PLIST_DIR}/com.anx.core.${UNLOAD_INSTANCE}.plist"
    if [[ -f "$plist_path" ]]; then
        echo "Unloading com.anx.core.${UNLOAD_INSTANCE}…"
        launchctl bootout "gui/$(id -u)" "$plist_path" 2>/dev/null || true
        rm -f "$plist_path"
        echo "Removed $plist_path"
    else
        echo >&2 "No plist found at $plist_path"
        exit 1
    fi
    exit 0
fi

# ── Resolve paths ────────────────────────────────────────────────────────── #

BIN_DIR="${PREFIX}/bin"
SHARE_DIR="${PREFIX}/share"
INSTALLED_BIN="${BIN_DIR}/anx-core"
INSTALLED_SCHEMA="${SHARE_DIR}/anx-schema.yaml"
INSTALLED_META_CMD="${SHARE_DIR}/meta/commands.json"

[[ -z "$LOG_DIR" ]] && LOG_DIR="${PREFIX}/logs"

# ── Install binary + assets ──────────────────────────────────────────────── #

if [[ "$SKIP_BUILD" -eq 0 || -n "$BINARY" ]]; then
    mkdir -p "$BIN_DIR" "$SHARE_DIR" "${SHARE_DIR}/meta" "$LOG_DIR"

    if [[ "$SKIP_BUILD" -eq 0 ]]; then
        echo "Building anx-core…"
        ANX_CORE_BIN="$INSTALLED_BIN" "${REPO_ROOT}/core/scripts/build-prod"
    else
        if [[ ! -f "$BINARY" ]]; then
            echo >&2 "Binary not found: $BINARY"
            exit 1
        fi
        echo "Installing binary from $BINARY"
        cp "$BINARY" "$INSTALLED_BIN"
        chmod +x "$INSTALLED_BIN"
    fi

    # Schema asset
    schema_src="${REPO_ROOT}/contracts/anx-schema.yaml"
    if [[ -f "$schema_src" ]]; then
        cp "$schema_src" "$INSTALLED_SCHEMA"
        echo "Installed schema → $INSTALLED_SCHEMA"
    else
        echo >&2 "Warning: schema not found at $schema_src"
    fi

    # Meta commands asset
    meta_src="${REPO_ROOT}/contracts/gen/meta/commands.json"
    if [[ -f "$meta_src" ]]; then
        cp "$meta_src" "$INSTALLED_META_CMD"
        echo "Installed meta commands → $INSTALLED_META_CMD"
    else
        echo >&2 "Warning: meta commands not found at $meta_src"
    fi

    echo ""
    echo "Install complete:"
    echo "  Binary:    $INSTALLED_BIN"
    echo "  Schema:    $INSTALLED_SCHEMA"
    echo "  Meta cmds: $INSTALLED_META_CMD"
    echo ""
fi

# ── Instance plist generation ────────────────────────────────────────────── #

if [[ -n "$INSTANCE" ]]; then
    if [[ ! -x "$INSTALLED_BIN" ]]; then
        echo >&2 "Binary not found at $INSTALLED_BIN — install first (omit --skip-build)"
        exit 1
    fi

    [[ -z "$WORKSPACE" ]] && WORKSPACE="${PREFIX}/workspaces/${INSTANCE}"
    mkdir -p "$WORKSPACE" "$LOG_DIR" "$PLIST_DIR"

    template="${REPO_ROOT}/deploy/launchd/com.anx.core.plist.template"
    if [[ ! -f "$template" ]]; then
        echo >&2 "Plist template not found at $template"
        exit 1
    fi

    plist_path="${PLIST_DIR}/com.anx.core.${INSTANCE}.plist"

    sed \
        -e "s|__INSTANCE__|${INSTANCE}|g" \
        -e "s|__BIN_PATH__|${INSTALLED_BIN}|g" \
        -e "s|__PORT__|${PORT}|g" \
        -e "s|__WORKSPACE_ROOT__|${WORKSPACE}|g" \
        -e "s|__SCHEMA_PATH__|${INSTALLED_SCHEMA}|g" \
        -e "s|__META_CMD_PATH__|${INSTALLED_META_CMD}|g" \
        -e "s|__LOG_DIR__|${LOG_DIR}|g" \
        "$template" > "$plist_path"

    echo "Generated plist → $plist_path"
    echo "  Instance:   $INSTANCE"
    echo "  Port:       $PORT"
    echo "  Workspace:  $WORKSPACE"
    echo "  Logs:       $LOG_DIR/anx-core-${INSTANCE}.{out,err}.log"

    if [[ "$LOAD" -eq 1 ]]; then
        echo ""
        echo "Loading into launchd…"
        launchctl bootout "gui/$(id -u)" "$plist_path" 2>/dev/null || true
        launchctl bootstrap "gui/$(id -u)" "$plist_path"
        echo "Loaded com.anx.core.${INSTANCE}"
        echo ""
        echo "Verify:"
        echo "  curl -fsS http://127.0.0.1:${PORT}/readyz"
        echo "  tail -f ${LOG_DIR}/anx-core-${INSTANCE}.err.log"
    else
        echo ""
        echo "To load:"
        echo "  launchctl bootstrap gui/\$(id -u) $plist_path"
        echo ""
        echo "To unload:"
        echo "  launchctl bootout gui/\$(id -u) $plist_path"
        echo "  # or: ./scripts/install-anx-core.sh --unload $INSTANCE"
    fi
fi

# ── No mode selected ─────────────────────────────────────────────────────── #

if [[ "$SKIP_BUILD" -eq 1 && -z "$BINARY" && -z "$INSTANCE" && -z "$UNLOAD_INSTANCE" ]]; then
    echo >&2 "Nothing to do. Specify --instance or omit --skip-build."
    echo >&2 "Run with --help for usage."
    exit 1
fi
