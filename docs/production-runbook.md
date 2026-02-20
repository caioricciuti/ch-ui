# CH-UI Production Runbook (VM2 Server + VM1 Connector)

This runbook covers a production topology where:

- **VM2** runs `ch-ui server` (UI, API, tunnel gateway)
- **VM1** runs `ch-ui connect` (agent next to ClickHouse)

## 1. VM2 Server Hardening

1. Create server config at `/etc/ch-ui/server.yaml`:

```yaml
port: 3488
app_url: https://ch-ui.example.com
app_secret_key: "replace-with-long-random-secret"
allowed_origins:
  - https://ch-ui.example.com
database_path: /var/lib/ch-ui/ch-ui.db
```

2. Keep runtime state in writable directories:

```bash
sudo mkdir -p /var/lib/ch-ui/run
sudo mkdir -p /var/lib/ch-ui
sudo chown -R chui:chui /var/lib/ch-ui
```

3. Use lifecycle commands with explicit PID file:

```bash
ch-ui server start -c /etc/ch-ui/server.yaml --detach --pid-file /var/lib/ch-ui/run/ch-ui-server.pid
ch-ui server status -c /etc/ch-ui/server.yaml --pid-file /var/lib/ch-ui/run/ch-ui-server.pid
ch-ui server stop -c /etc/ch-ui/server.yaml --pid-file /var/lib/ch-ui/run/ch-ui-server.pid
```

## 2. VM2 systemd Service (recommended)

Create `/etc/systemd/system/ch-ui-server.service`:

```ini
[Unit]
Description=CH-UI Server
After=network.target

[Service]
Type=simple
User=chui
Group=chui
WorkingDirectory=/var/lib/ch-ui
ExecStart=/usr/local/bin/ch-ui server start -c /etc/ch-ui/server.yaml --pid-file /var/lib/ch-ui/run/ch-ui-server.pid
ExecStop=/usr/local/bin/ch-ui server stop -c /etc/ch-ui/server.yaml --pid-file /var/lib/ch-ui/run/ch-ui-server.pid
Restart=always
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
```

Then:

```bash
sudo systemctl daemon-reload
sudo systemctl enable ch-ui-server
sudo systemctl start ch-ui-server
sudo systemctl status ch-ui-server
```

## 3. VM2 Reverse Proxy (TLS + WebSocket)

Your proxy must:

- route app traffic to `127.0.0.1:3488`
- support WebSocket upgrades on `/connect`
- keep long-enough timeouts for tunnel traffic

Use the repo example: `ch-ui.conf`.

## 4. VM1 Connector Setup

1. On VM2, create a tunnel key for VM1:

```bash
ch-ui tunnel create --name "vm1-clickhouse" -c /etc/ch-ui/server.yaml --url wss://ch-ui.example.com/connect
```

Copy the generated `cht_...` token.

2. Install connector service on VM1:

```bash
sudo /usr/local/bin/ch-ui service install \
  --url wss://ch-ui.example.com/connect \
  --key cht_your_tunnel_token \
  --clickhouse-url http://127.0.0.1:8123
```

3. Verify:

```bash
ch-ui service status
ch-ui service logs -f
```

4. (Optional) Rotate compromised/old token from VM2:

```bash
ch-ui tunnel list -c /etc/ch-ui/server.yaml
ch-ui tunnel rotate <connection-id> -c /etc/ch-ui/server.yaml --url wss://ch-ui.example.com/connect
```

## 5. Network Policy

- VM2 inbound: `443` (or your TLS port)
- VM2 inbound: `3488` only from localhost/reverse-proxy path
- VM1 outbound: allow to `wss://ch-ui.example.com/connect`
- VM1 ClickHouse can stay local-only (`127.0.0.1:8123`)

## 6. Monitoring and Backups

1. Health endpoint:

```bash
curl -fsS http://127.0.0.1:3488/health
```

2. Back up SQLite:

- file: `/var/lib/ch-ui/ch-ui.db`
- schedule daily snapshot + retention policy
- verify restore procedure quarterly

3. Log collection:

- VM2: `journalctl -u ch-ui-server`
- VM1: `ch-ui service logs` or platform service logs

## 7. Upgrade Procedure

1. Replace binaries on VM2 and VM1.
2. Restart services:

```bash
sudo systemctl restart ch-ui-server
ch-ui service restart
```

3. Validate:

```bash
ch-ui version
ch-ui server status -c /etc/ch-ui/server.yaml --pid-file /var/lib/ch-ui/run/ch-ui-server.pid
ch-ui service status
```

## 8. Notes on Older Binaries

Older builds did not support server lifecycle subcommands (`status/stop/restart`).
If `ch-ui server status` starts the server, replace the binary with a newer build and retry.
