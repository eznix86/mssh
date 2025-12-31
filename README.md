# mssh

Minimal rendezvous service for reaching SSH behind NAT

This enables SSH access to machines behind NAT/firewalls using a simple rendezvous server. No complex VPN setup required.

## Table of Contents

- [Features](#features)
- [Installation](#installation)
  - [Quick Install (Linux)](#quick-install-linux)
  - [Manual Build](#manual-build)
  - [Uninstall](#uninstall)
- [Configuration](#configuration)
- [Usage](#usage)
  - [Server](#server)
  - [Agent](#agent)
  - [Client](#client)
- [Systemd Integration](#systemd-integration)
- [Security](#security)
- [License](#license)

---

## Features

- **Auto-reconnecting agents** – Node-id defaults to the host's primary IPv4 if omitted
- **Built-in SSH client** – Passphrase prompts and ssh-agent support included
- **Flexible configuration** – `~/.mssh/config.yaml` with global defaults and per-node overrides
- **Systemd integration** – Ready-to-use units and install script for Linux (amd64/arm64)

**Three simple commands:**

| Command | Purpose |
|---------|---------|
| `mssh server` | Rendezvous server accepting agents and clients |
| `mssh agent <node-id>` | Keeps a TCP tunnel from a remote host back to the server |
| `mssh proxy <node-id>` | Connects to a node from your workstation |

---

## Installation

### Quick Install (Linux with Systemd)

**Install and enable server:**

```bash
curl -fsSL https://raw.githubusercontent.com/eznix86/mssh/main/install/install.sh | \
  sudo BIN_DIR=/usr/local/bin bash -s -- server --host 0.0.0.0 --port 8443
```

**Install and enable agent:**

```bash
curl -fsSL https://raw.githubusercontent.com/eznix86/mssh/main/install/install.sh | \
  sudo bash -s -- agent --server rendezvous.example.com:8443 --ssh-port 22
```

> **Note:** Set `VERSION=vX.Y.Z` to pin a specific release (default: latest). When running `agent` without flags, the script prompts interactively for configuration.

### Manual Build

```bash
go build ./cmd/mssh
sudo install -m 0755 mssh /usr/local/bin/mssh
```

### Uninstall

```bash
curl -fsSL https://raw.githubusercontent.com/eznix86/mssh/main/install/uninstall.sh | sudo bash
```

Removes the binary and disables all systemd units.

---

## Configuration

Only used for `mssh username@node-id`

Initialize your config file:

```bash
mssh config init
```

This creates `~/.mssh/config.yaml`:

```yaml
server: rendezvous.example.com:8443
identity: ~/.ssh/id_ed25519   # optional; leave blank to auto-detect keys / use ssh-agent
nodes:
  prod-db-1:
    server: prod-rendezvous.example.com:8443
    identity: ~/.ssh/prod_key
```

**Priority:** CLI flags → node-specific values → top-level defaults

---

## Usage

### Server

Run the rendezvous server:

```bash
mssh server --host 0.0.0.0 --port 8443
```

> **Production tip:** Deploy behind a TLS proxy (nginx, Traefik, Caddy) with Let's Encrypt for secure public exposure.

### Agent

Run on the remote host behind NAT:

```bash
# With explicit node-id
mssh agent prod-db-1 --server rendezvous.example.com:8443 --ssh-port 22

# Auto-detect node-id from primary IPv4
mssh agent --server rendezvous.example.com:8443
```

The agent automatically re-registers after each session ends.

**Node-ID rules:** May contain letters, digits, `.`, `_`, and `-`. If omitted, the primary IPv4 address is used.

### Client

**Built-in SSH client:**

```bash
mssh alice@prod-db-1

# With custom server or identity
mssh alice@prod-db-1 --server other.example.net:8443 --identity ~/.ssh/prod_key
```

The client scans `~/.ssh/id_{ed25519,rsa,ecdsa}` (with passphrase prompts) and falls back to `SSH_AUTH_SOCK`.

**ProxyCommand integration:**

```bash
ssh -o ProxyCommand="mssh proxy prod-db-1 --server rendezvous.example.com:8443" alice@localhost
```

---

## Systemd Integration

The install script automatically configures systemd units. To customize manually:

**Server:**

```bash
sudo cp install/systemd/mssh-server.service /etc/systemd/system/mssh-server.service
sudo systemctl daemon-reload && sudo systemctl enable --now mssh-server
```

**Agent:**

```bash
sudo cp install/systemd/mssh-agent.service /etc/systemd/system/mssh-agent.service
sudo systemctl daemon-reload && sudo systemctl enable --now mssh-agent
```

Edit the `ExecStart` line in each unit file to customize flags.

---

## Security

- **TLS termination:** Place the rendezvous server behind a TLS proxy (nginx/Caddy/Traefik) with Let's Encrypt
- **Optional hardening:** Add mutual TLS or IP filtering at the proxy layer

---

## License

MIT
