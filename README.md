# mssh

Minimal rendezvous service for reaching SSH behind NAT

This enables SSH access to machines behind NAT/firewalls using a simple rendezvous server. No complex VPN setup required.

| Command | Purpose |
|---------|---------|
| `mssh server` | Runs the rendezvous service on a public host |
| `mssh agent <node-id>` | Keeps a connection open from a NATed host back to the server |
| `mssh proxy <node-id>` / `mssh user@node` | Lets you connect from your workstation |

## Table of Contents

- [Installation](#installation)
  - [Binary only](#binary-only)
  - [Server (systemd)](#server-systemd)
  - [Agent (systemd)](#agent-systemd)
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


## Installation

### Binary only

```
curl -fsSL https://raw.githubusercontent.com/eznix86/mssh/main/install/install.sh | sudo bash
```

The script downloads the correct release tarball for linux/amd64 or linux/arm64 and drops `/usr/local/bin/mssh` (override with `BIN_DIR=/path`).

### Server (systemd)

```
curl -fsSL https://raw.githubusercontent.com/eznix86/mssh/main/install/install.sh | \
  sudo BIN_DIR=/usr/local/bin bash -s -- server --host 0.0.0.0 --port 8443
```

This writes `/etc/systemd/system/mssh-server.service` and runs `systemctl enable --now mssh-server`.

### Agent (systemd)

```
curl -fsSL https://raw.githubusercontent.com/eznix86/mssh/main/install/install.sh | \
  sudo bash -s -- agent --server rendezvous.example.com:8443 --ssh-port 22
```

If you omit flags, the script prompts for the rendezvous server, node-id (defaults to auto-detected IP), and any extra CLI flags before rendering the unit.

Set `VERSION=vX.Y.Z` to pin a specific release (default: latest).

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

## Usage

Deploy the server on a publicly reachable machine, run the agent on each host behind NAT, then connect from your workstation using either the built-in Go SSH client or the ProxyCommand approach described below.

![mssh diagram](docs/mssh.png)

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

To make this seamless, add it to `~/.ssh/config` so `ssh prod-db` works without long command lines:

```ssh-config
Host prod-db
    HostName localhost
    User alice
    ProxyCommand mssh proxy prod-db-1 --server rendezvous.example.com:8443
```

Now simply run `ssh prod-db` and the ProxyCommand will invoke `mssh proxy ...` behind the scenes.

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
