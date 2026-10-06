#!/usr/bin/env bash
set -euo pipefail

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "[uninstall] missing required command: $1" >&2
    exit 1
  fi
}

need_cmd systemctl

if [ "${SUDO:-unset}" = "unset" ]; then
  if [ "$(id -u)" -eq 0 ]; then
    SUDO=""
  else
    need_cmd sudo
    SUDO="sudo"
  fi
fi

BIN_DIR="${BIN_DIR:-/usr/local/bin}"
case "$BIN_DIR" in
  /*) ;;
  *) echo "[uninstall] BIN_DIR must be an absolute path" >&2; exit 1 ;;
esac

remove_unit() {
  local unit="$1"
  if [ -f "/etc/systemd/system/$unit" ]; then
    echo "[uninstall] disabling $unit"
    $SUDO systemctl disable --now "$unit" >/dev/null 2>&1 || true
    echo "[uninstall] removing /etc/systemd/system/$unit"
    $SUDO rm -f "/etc/systemd/system/$unit"
  fi
}

remove_unit mssh-server.service
remove_unit mssh-agent.service

$SUDO systemctl daemon-reload

if [ -x "$BIN_DIR/mssh" ]; then
  echo "[uninstall] removing $BIN_DIR/mssh"
  $SUDO rm -f "$BIN_DIR/mssh"
fi

echo "[uninstall] done"
