#!/usr/bin/env bash
set -euo pipefail

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "[install] missing required command: $1" >&2
    exit 1
  fi
}

need_cmd curl
need_cmd tar

if [ "$(uname -s)" != "Linux" ]; then
  echo "[install] only Linux is supported" >&2
  exit 1
fi

ARCH_RAW=$(uname -m)
case "$ARCH_RAW" in
  x86_64)
    ARCH=amd64
    ;;
  aarch64|arm64)
    ARCH=arm64
    ;;
  *)
    echo "[install] unsupported architecture: $ARCH_RAW" >&2
    exit 1
    ;;
esac

if [ "${SUDO:-unset}" = "unset" ]; then
  if [ "$(id -u)" -eq 0 ]; then
    SUDO=""
  else
    need_cmd sudo
    SUDO="sudo"
  fi
fi

BIN_DIR="${BIN_DIR:-/usr/local/bin}"
VERSION="${VERSION:-latest}"

MODE=""
SERVICE_ARGS=()
if [ "$#" -gt 0 ]; then
  MODE="$1"
  shift
  SERVICE_ARGS=("$@")
fi

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

fetch() {
  local url="$1"
  local dest="$2"
  echo "[install] fetching $url"
  curl -fsSL "$url" -o "$dest"
}

install_binary() {
  local asset_url
  if [ "$VERSION" = "latest" ]; then
    asset_url="https://github.com/eznix86/mssh/releases/latest/download/mssh-linux-$ARCH.tar.gz"
  else
    asset_url="https://github.com/eznix86/mssh/releases/download/$VERSION/mssh-linux-$ARCH.tar.gz"
  fi

  local archive="$TMPDIR/mssh.tar.gz"
  fetch "$asset_url" "$archive"
  tar -xzf "$archive" -C "$TMPDIR"
  if [ ! -f "$TMPDIR/mssh" ]; then
    echo "[install] archive did not contain mssh binary" >&2
    exit 1
  fi
  $SUDO install -m 0755 "$TMPDIR/mssh" "$BIN_DIR/mssh"
  echo "[install] installed $BIN_DIR/mssh"
}

quote_args() {
  local result=""
  local arg
  for arg in "$@"; do
    local quoted
    printf -v quoted '%q' "$arg"
    result+=" $quoted"
  done
  printf '%s' "$result"
}

need_envsubst() {
  if ! command -v envsubst >/dev/null 2>&1; then
    echo "[install] envsubst is required to render systemd units" >&2
    exit 1
  fi
}

install_server_unit() {
  local args_suffix
  if [ "$#" -gt 0 ]; then
    args_suffix="$(quote_args "$@")"
  else
    args_suffix=" --host 0.0.0.0 --port 8443"
  fi
  local tmp="$TMPDIR/mssh-server.service"
  cat <<EOF > "$tmp"
[Unit]
Description=mssh rendezvous server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=$BIN_DIR/mssh server$args_suffix
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF
  $SUDO install -m 0644 "$tmp" /etc/systemd/system/mssh-server.service
  $SUDO systemctl daemon-reload
  $SUDO systemctl enable --now mssh-server
  echo "[install] server unit enabled"
}

prompt_agent_settings() {
  local prompt_server=""
  while [ -z "$prompt_server" ]; do
    read -rp "Enter rendezvous server (host:port): " prompt_server
  done
  read -rp "Enter node-id (leave blank for auto-detected IP): " prompt_node
  read -rp "Extra flags for mssh agent (optional): " prompt_extra
  AGENT_PROMPT_SERVER="$prompt_server"
  AGENT_PROMPT_NODE="$prompt_node"
  AGENT_PROMPT_EXTRA="$prompt_extra"
}

install_agent_unit() {
  if [ "$#" -gt 0 ]; then
    local args_suffix="$(quote_args "$@")"
    local tmp="$TMPDIR/mssh-agent.service"
    cat <<EOF > "$tmp"
[Unit]
Description=mssh agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=$BIN_DIR/mssh agent$args_suffix
Restart=always
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF
    $SUDO install -m 0644 "$tmp" /etc/systemd/system/mssh-agent.service
  else
    need_envsubst
    prompt_agent_settings
    local template="$TMPDIR/mssh-agent.service.tmpl"
    cat <<'EOF' > "$template"
[Unit]
Description=mssh agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${MSSH_BIN} agent${NODE_PART} --server ${SERVER_ADDR}${EXTRA_PART}
Restart=always
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF
    local output="$TMPDIR/mssh-agent.service"
    local node_part=""
    if [ -n "$AGENT_PROMPT_NODE" ]; then
      node_part=" $AGENT_PROMPT_NODE"
    fi
    local extra_part=""
    if [ -n "$AGENT_PROMPT_EXTRA" ]; then
      extra_part=" $AGENT_PROMPT_EXTRA"
    fi
    MSSH_BIN="$BIN_DIR/mssh" NODE_PART="$node_part" SERVER_ADDR="$AGENT_PROMPT_SERVER" EXTRA_PART="$extra_part" envsubst '${MSSH_BIN} ${NODE_PART} ${SERVER_ADDR} ${EXTRA_PART}' < "$template" > "$output"
    $SUDO install -m 0644 "$output" /etc/systemd/system/mssh-agent.service
  fi
  $SUDO systemctl daemon-reload
  $SUDO systemctl enable --now mssh-agent
  echo "[install] agent unit enabled"
}

install_binary

case "$MODE" in
  server)
    install_server_unit "${SERVICE_ARGS[@]}"
    ;;
  agent)
    install_agent_unit "${SERVICE_ARGS[@]}"
    ;;
  "")
    echo "[install] binary installed. Pass 'server' or 'agent' plus optional flags to install a systemd service."
    ;;
  *)
    echo "[install] unknown mode '$MODE'" >&2
    exit 1
    ;;
esac
