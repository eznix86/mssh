#!/usr/bin/env bash
set -euo pipefail

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "[install] missing required command: $1" >&2
    exit 1
  fi
}

quote_args() {
  local result="" arg
  for arg in "$@"; do
    case "$arg" in
      *$'\n'*|*$'\r'*)
        echo "[install] arguments must not contain newlines" >&2
        return 1
        ;;
    esac
    arg=${arg//\\/\\\\}
    arg=${arg//\"/\\\"}
    arg=${arg//\$/\$\$}
    arg=${arg//%/%%}
    arg=${arg//$'\t'/\\t}
    if [ "$arg" = ";" ]; then arg='\;'; fi
    result+=" \"$arg\""
  done
  printf '%s' "${result# }"
}

MODE="${1:-}"
case "$MODE" in
  server|agent|"") ;;
  *) echo "[install] unknown mode '$MODE'" >&2; exit 1 ;;
esac
if [ "$#" -gt 0 ]; then shift; fi
SERVICE_ARGS=("$@")
BIN_DIR="${BIN_DIR:-/usr/local/bin}"
VERSION="${VERSION:-latest}"
case "$BIN_DIR" in
  /*) ;;
  *) echo "[install] BIN_DIR must be an absolute path" >&2; exit 1 ;;
esac
quote_args "$BIN_DIR/mssh" "$MODE" "${SERVICE_ARGS[@]}" >/dev/null

need_cmd curl
need_cmd tar
need_cmd sha256sum
if [ -n "$MODE" ]; then need_cmd systemctl; fi
if [ "$(uname -s)" != "Linux" ]; then
  echo "[install] only Linux is supported" >&2
  exit 1
fi
case "$(uname -m)" in
  x86_64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "[install] unsupported architecture" >&2; exit 1 ;;
esac

if [ "${SUDO:-unset}" = "unset" ]; then
  if [ "$(id -u)" -eq 0 ]; then SUDO=""; else need_cmd sudo; SUDO=sudo; fi
fi

prompt_agent_settings() {
  if ! exec 3</dev/tty; then
    echo "[install] pass agent flags when no terminal is available" >&2
    exit 1
  fi
  local address="" node="" token="" ca="" use_tls=""
  while [ -z "$address" ]; do
    read -r -u 3 -p "Rendezvous server (host:port): " address || return 1
  done
  read -r -u 3 -p "Node ID (blank for auto-detection): " node || return 1
  read -r -u 3 -p "Use TLS? [Y/n]: " use_tls || return 1
  read -r -u 3 -p "Token file (blank for local testing): " token || return 1
  read -r -u 3 -p "Private CA file (blank for system roots): " ca || return 1
  exec 3<&-
  SERVICE_ARGS=(--server "$address")
  if [ -n "$node" ]; then SERVICE_ARGS=("$node" "${SERVICE_ARGS[@]}"); fi
  case "$use_tls" in
    n|N) SERVICE_ARGS+=(--no-tls) ;;
    ""|y|Y) SERVICE_ARGS+=(--tls) ;;
    *) echo "[install] TLS answer must be y or n" >&2; return 1 ;;
  esac
  if [ -n "$token" ]; then SERVICE_ARGS+=(--token-file "$token"); fi
  if [ -n "$ca" ]; then SERVICE_ARGS+=(--tls-ca "$ca"); fi
}

if [ "$MODE" = "agent" ] && [ "${#SERVICE_ARGS[@]}" -eq 0 ]; then
  prompt_agent_settings
fi
quote_args "$BIN_DIR/mssh" "$MODE" "${SERVICE_ARGS[@]}" >/dev/null

INSTALL_TMP=$(mktemp -d)
trap 'rm -rf "$INSTALL_TMP"' EXIT
ASSET="mssh-linux-$ARCH.tar.gz"
if [ "$VERSION" = "latest" ]; then
  ASSET_URL="https://github.com/eznix86/mssh/releases/latest/download/$ASSET"
else
  ASSET_URL="https://github.com/eznix86/mssh/releases/download/$VERSION/$ASSET"
fi
curl -fsSL "$ASSET_URL" -o "$INSTALL_TMP/$ASSET"
curl -fsSL "$ASSET_URL.sha256" -o "$INSTALL_TMP/$ASSET.sha256"
read -r EXPECTED_SHA _ < "$INSTALL_TMP/$ASSET.sha256"
if [[ ! "$EXPECTED_SHA" =~ ^[a-fA-F0-9]{64}$ ]]; then
  echo "[install] invalid release checksum" >&2
  exit 1
fi
ACTUAL_SHA=$(sha256sum "$INSTALL_TMP/$ASSET")
if [ "${ACTUAL_SHA%% *}" != "$EXPECTED_SHA" ]; then
  echo "[install] release checksum mismatch" >&2
  exit 1
fi
tar -xzf "$INSTALL_TMP/$ASSET" -C "$INSTALL_TMP" mssh
$SUDO install -d "$BIN_DIR"
$SUDO install -m 0755 "$INSTALL_TMP/mssh" "$BIN_DIR/mssh"
echo "[install] installed $BIN_DIR/mssh"

if [ -n "$MODE" ]; then
  EXEC_START=$(quote_args "$BIN_DIR/mssh" "$MODE" "${SERVICE_ARGS[@]}")
  UNIT="$INSTALL_TMP/mssh-$MODE.service"
  cat > "$UNIT" <<UNIT_FILE
[Unit]
Description=mssh $MODE
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=$EXEC_START
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
UNIT_FILE
  $SUDO install -m 0644 "$UNIT" "/etc/systemd/system/mssh-$MODE.service"
  $SUDO systemctl daemon-reload
  $SUDO systemctl enable --now "mssh-$MODE"
  echo "[install] $MODE unit enabled"
fi
