# Rendezvous protocol

Upgrade the server, agents and clients together. This release uses `MSSH/2` and does not accept the old protocol.

Public listeners require a TLS certificate, its private key and a shared token file. The default listener is `127.0.0.1:8443`. Plain TCP is available only on loopback listeners.

Generate a token with `openssl rand -hex 32` and save it in a file with mode `0600`. Give the same token to trusted agents and clients. Each token holder can register or connect to any node on that server. This is one trust group. Use separate servers and tokens for groups that must be isolated. Restart the server and agents after token rotation.

```bash
mssh server --host 0.0.0.0 --tls-cert /etc/mssh/cert.pem --tls-key /etc/mssh/key.pem --token-file /etc/mssh/token
mssh agent prod-db-1 --server rendezvous.example.com:8443 --tls --token-file ~/.mssh/token
mssh alice@prod-db-1 --server rendezvous.example.com:8443 --tls --token-file ~/.mssh/token
```

The TLS certificate must cover the server hostname. Clients verify it against system roots. Use `--tls-ca /path/to/ca.pem` for a private CA. This flag enables TLS. There is no option to skip certificate verification. Do not connect a plain TCP client directly to a TLS proxy. An external proxy can pass TLS through to mssh, or terminate TLS and forward to a loopback mssh listener. Clients still use `--tls` when the proxy terminates TLS. Configure the loopback backend with `--token-file`.

SSH host keys remain a separate check. The built-in client requires a trusted key under the node ID in `~/.ssh/known_hosts`, using logical port 22 even when the agent uses another local SSH port. Get the key from the node through a trusted channel. For example, prepend `prod-db-1` to the contents of `/etc/ssh/ssh_host_ed25519_key.pub` obtained through that channel. Do not trust an unverified key from the rendezvous server.

Client settings can also be stored in `~/.mssh/config.yaml`:

```yaml
server: rendezvous.example.com:8443
tls: true
token_file: ~/.mssh/token
identity: ~/.ssh/id_ed25519
nodes:
  prod-db-1:
    tls_ca: ~/.mssh/private-ca.pem
```

The agent, proxy and built-in SSH client read config. Flags override node values, which override global values. `--tls` enables TLS; `--tls-ca` also enables it. An agent with no server flag or config uses `localhost:8443`.

Control messages have a 4096-byte limit. Initial protocol and SSH handshakes have deadlines. Idle agents answer heartbeats every 10 seconds. A failed heartbeat is removed within at most 40 seconds. The agent opens local SSH only when the server sends `CONNECT`.

Each agent supports one connection at a time. Other clients receive `agent busy` while a session is active. The agent registers again two seconds after a session ends. The server accepts at most 1024 open connections. Established tunnels do not have a session time limit. Shutdown closes all connections and waits for handlers.
