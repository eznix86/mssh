# mssh

Minimal rendezvous service that lets you reach SSH daemons running behind NAT. It ships with three commands:

- `mssh server` – rendezvous server accepting agents and clients
- `mssh agent <node-id>` – keep a TCP tunnel from a remote host back to the server
- `mssh proxy <node-id>` / `mssh user@node-id` – connect to a node from your workstation (ProxyCommand or built-in Go SSH client)

## Features

- Agents auto-reconnect (node-id defaults to the host's primary IPv4 if omitted)
- Built-in SSH client with passphrase prompts and ssh-agent support
- Configurable via `~/.mssh/config.yaml` (global defaults + per-node overrides)
- Systemd units and install script for Linux (amd64/arm64)

## Installation

### Using the install script (Linux amd64/arm64)

Install just the binary:

```bash
curl -fsSL https://raw.githubusercontent.com/eznix86/mssh/main/install/install.sh | sudo bash -s --
```

Install and auto-enable the server unit (flags passed after `server` go straight to `mssh server`):

```bash
curl -fsSL https://raw.githubusercontent.com/eznix86/mssh/main/install/install.sh | \
  sudo BIN_DIR=/usr/local/bin bash -s -- server --host 0.0.0.0 --port 8443
```

Install and auto-enable an agent (omit flags to receive interactive prompts for server/node-id):

```bash
curl -fsSL https://raw.githubusercontent.com/eznix86/mssh/main/install/install.sh | \
  sudo bash -s -- agent --server rendezvous.example.com:8443 --ssh-port 22
```

Notes:

- Set `VERSION=vX.Y.Z` to pin a release (default: latest)
- Supported architectures: linux/amd64, linux/arm64
- When you run `... | bash -s -- agent` without additional flags, the script prompts for the rendezvous server, node-id, and extra flags, then renders the unit via `envsubst`
- The script downloads the release tarball (`mssh-linux-$ARCH`), installs it to `$BIN_DIR` (default `/usr/local/bin`), writes a systemd unit, runs `sudo systemctl daemon-reload && sudo systemctl enable --now ...`, and reloads systemd automatically

### Manual build

```bash
go build ./cmd/mssh
sudo install -m 0755 mssh /usr/local/bin/mssh
```

## Configuration

Run `mssh config init` once to create `~/.mssh/config.yaml`:

```yaml
server: rendezvous.example.com:8443
identity: ~/.ssh/id_ed25519   # optional; leave blank to auto-detect keys / use ssh-agent
nodes:
  prod-db-1:
    server: prod-rendezvous.example.com:8443
    identity: ~/.ssh/prod_key
```

CLI flags override node-specific values, which override top-level defaults.

## Usage

### Rendezvous server

```bash
mssh server --host 0.0.0.0 --port 8443
```

Deploy behind a TLS proxy (nginx, Traefik, Caddy, etc.) with a Let's Encrypt certificate for secure public exposure.

### Agent

```bash
mssh agent prod-db-1 --server rendezvous.example.com:8443 --ssh-port 22
# or omit the node-id to use the machine's primary IPv4 address
mssh agent --server rendezvous.example.com:8443
```

The agent logs "client disconnected" and immediately re-registers after each session.

### Client (built-in Go SSH)

```bash
mssh alice@prod-db-1
# optionally specify a different server or identity
mssh alice@prod-db-1 --server other.example.net:8443 --identity ~/.ssh/prod_key
```

The client scans `~/.ssh/id_{ed25519,rsa,ecdsa}` (with passphrase prompts) and falls back to `SSH_AUTH_SOCK`.

### Client (ProxyCommand)

```bash
ssh -o ProxyCommand="mssh proxy prod-db-1 --server rendezvous.example.com:8443" alice@localhost
```

## Systemd units

The install script already writes and enables the units for you. To customize manually:

```bash
# Server (edit ExecStart line with your preferred flags)
sudo cp install/systemd/mssh-server.service /etc/systemd/system/mssh-server.service
sudo systemctl daemon-reload && sudo systemctl enable --now mssh-server

# Agent (edit node-id / server flags)
sudo cp install/systemd/mssh-agent.service /etc/systemd/system/mssh-agent.service
sudo systemctl daemon-reload && sudo systemctl enable --now mssh-agent
```

## Default node-id behavior

- If `mssh agent` is invoked without `<node-id>`, it auto-detects the primary IPv4 and uses it as the node identifier
- Node-ids may include letters, digits, `.`, `_`, and `-` (matching the rendezvous server's validation rules)

## Security

- Place the rendezvous server behind a TLS proxy (e.g., nginx/Caddy/Traefik) with Let's Encrypt to secure the public endpoint
- Optionally require mutual TLS or IP filtering at the proxy for further hardening

## License

MIT
