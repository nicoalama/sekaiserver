#!/bin/sh
set -eu

BIN_NAME="sekai-server"
CONFIG_DIR="${HOME}/.sekai-server"
CONFIG_FILE="${CONFIG_DIR}/config.json"

# ── Parse flags ──────────────────────────────────────────────────────────────
AUTO_YES=false

while [ $# -gt 0 ]; do
  case "$1" in
    --yes|-y) AUTO_YES=true ;;
    *)
      echo "Unknown option: $1"
      echo "Usage: uninstall.sh [--yes]"
      exit 1
      ;;
  esac
  shift
done

# ── Find installed binary ────────────────────────────────────────────────────
INSTALLED_PATH=""
for candidate in "/usr/local/bin/${BIN_NAME}" "${HOME}/.local/bin/${BIN_NAME}"; do
  if [ -f "$candidate" ]; then
    INSTALLED_PATH="$candidate"
    break
  fi
done

# ── Notify if nothing found ──────────────────────────────────────────────────
if [ -z "$INSTALLED_PATH" ] && [ ! -f "$CONFIG_FILE" ]; then
  echo "sekai-server does not appear to be installed."
  echo "  Checked binary: /usr/local/bin/${BIN_NAME}, ~/.local/bin/${BIN_NAME}"
  echo "  Checked config: ${CONFIG_FILE}"
  exit 0
fi

echo "This will remove sekai-server and its configuration."
echo ""

# ── Confirm ──────────────────────────────────────────────────────────────────
if [ "$AUTO_YES" = false ]; then
  printf "Are you sure? [y/N] "
  read -r REPLY
  case "$REPLY" in
    y|Y|yes|YES) ;;
    *) echo "Aborted."; exit 0 ;;
  esac
fi

# ── Kill running process ─────────────────────────────────────────────────────
if command -v pkill >/dev/null 2>&1; then
  pkill -x "${BIN_NAME}" 2>/dev/null && echo "Stopped running sekai-server" || true
else
  # Fallback: find PID via pgrep or ps
  PID=""
  if command -v pgrep >/dev/null 2>&1; then
    PID="$(pgrep -x "${BIN_NAME}" 2>/dev/null || true)"
  else
    PID="$(ps aux 2>/dev/null | grep "[${BIN_NAME%?}]${BIN_NAME#?}" | awk '{print $2}' || true)"
  fi
  if [ -n "$PID" ]; then
    kill "$PID" 2>/dev/null && echo "Stopped running sekai-server (PID: ${PID})" || true
  fi
fi

# ── Remove binary ────────────────────────────────────────────────────────────
if [ -n "$INSTALLED_PATH" ]; then
  rm -f "$INSTALLED_PATH"
  echo "Removed binary: ${INSTALLED_PATH}"
fi

# ── Remove config ────────────────────────────────────────────────────────────
if [ -d "$CONFIG_DIR" ]; then
  rm -rf "$CONFIG_DIR"
  echo "Removed config: ${CONFIG_DIR}"
fi

echo ""
echo "sekai-server uninstalled."
