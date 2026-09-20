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

1. Create a consistent backup before replacing binaries:

```bash
ch-ui backup /secure-backups/ch-ui-before-upgrade.db -c /etc/ch-ui/server.yaml
```

   Preserve `APP_SECRET_KEY` / `app_secret_key` separately. The database contains
   encrypted credentials that cannot be recovered with a different key.
   Replace binaries on VM2 and VM1 after the backup succeeds.
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

## 9. Background accounts (v2.12.0)

Under **Admin → Connections → Background accounts**, configure each worker
independently: scheduled queries, models, pipeline sinks, governance, Cluster
Health and telemetry monitors. Only administrators can read or change these
settings. Account configuration is available in every edition; it does not
unlock licensed features.

| Mode | Behavior |
| --- | --- |
| Dedicated ClickHouse account | Verifies `SELECT 1` through the connection before saving an encrypted password; jobs can run without human sessions |
| An active user session | Removes the saved account and borrows a usable session; this remains the upgrade default |
| Disabled | Removes the saved account and denies subsequent background credential acquisition; no session fallback |

CH-UI is a shared workspace. Admin and analyst roles are instance-wide. Giving
those roles access to jobs delegates the configured account's grants through
the jobs, including shared jobs on other connections. Connection selection is
not a tenant boundary. Viewers cannot create, edit, delete or run schedules,
models, pipelines or telemetry monitors. Do not give a background account
broader grants than workspace writers should be able to exercise.

Create dedicated ClickHouse users with grants limited to the required tables:

| Worker | Grants to consider |
| --- | --- |
| Scheduled queries | Operations used by the scheduled SQL |
| Models | SELECT on sources; CREATE VIEW/TABLE and any DROP/ALTER/INSERT needed on targets |
| Pipeline sink | INSERT on destinations; CREATE TABLE if automatic creation is enabled |
| Governance | SELECT and metadata/access visibility on the system tables being collected |
| Cluster Health | SELECT on the system tables used for node and query statistics |
| Telemetry monitors | SELECT on configured logs/traces tables, with permission to describe them |

Sign-in verification does not prove these grants. Run a representative job
after saving. Rejected verification or cancellation preserves the previous
account. Passwords are never returned by the account API; enter the password
again on every save. A blank password explicitly configures an empty password.

Rotation takes effect on the next credential acquisition without a restart.
Pipeline batches acquire credentials individually; model runs retain them for
the whole run. Disabling does not cancel work already holding credentials.
For immediate revocation, also revoke the ClickHouse user's grants or password
and terminate its active queries using your normal ClickHouse procedures.

Manual schedule and telemetry-monitor runs use the caller's credentials and
remain available in Disabled mode. Manual model runs use the model worker's
configured account, so Disabled blocks them as well.

Configuration writes emit `connection.background_account.updated`, identifying
the administrator (SSO subject when present). Dedicated account acquisition
emits `<worker>.credential_use`, at most once per worker/connection/hour and
again on rotation. Session mode retains `<worker>.credential_borrow`. These
events record acquisition, not successful SQL execution; consult job results
for completion. Debug logs record each credential use and lookup failures.

If a worker stops, check connection status, mode, the unchanged app secret and
ClickHouse grants. A configured but unreadable password fails closed; it does
not borrow another user's session. Disabled/missing credentials can leave the
last monitoring sample visible until execution resumes.

### Restore and rollback

Test restores on an isolated host: stop the server, restore a consistent
database backup, use the original app secret, then restart and verify both
session-mode and dedicated-account jobs. Repeated startup migrations are safe.

v2.11.1 and older do not read the new credential or Disabled settings. An old
binary resumes its session-borrowing behavior. Before rollback, stop the server
and workers; restore the pre-upgrade database/configuration and old binary as
one operation. Verify the old job behavior before permitting execution. Do not
rely on a v2.12.0 Disabled setting to protect an older binary.

### Repeatable worker acceptance test

The worker tests use protocol fixtures in normal CI. For live verification,
point them only at a disposable ClickHouse instance: they create or replace
users named `worker_*` and write `default.test_model` and `default.target`.
Prepare `default.otel_logs (Timestamp DateTime64(9), Body String)` with a
MergeTree engine ordered by Timestamp. Supply credentials that can manage
these test users and grant SELECT, INSERT, CREATE TABLE/VIEW, DROP TABLE/VIEW,
ALTER and SHOW. Then run:

```bash
CHUI_TEST_CLICKHOUSE_URL=http://127.0.0.1:18123 \
CHUI_TEST_CLICKHOUSE_USER=release_admin \
CHUI_TEST_CLICKHOUSE_PASSWORD=your-test-password \
go test ./internal/scheduler ./internal/models ./internal/pipelines \
  ./internal/clusterhealth ./internal/governance ./internal/telemetry/monitor \
  -run TestBackgroundAccountExecution -count=1 -v
```
