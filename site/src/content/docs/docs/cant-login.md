---
title: Can't Login?
description: Fast recovery steps when CH-UI sign-in fails
---

Use this guide if the login screen appears but sign-in fails, local URL is wrong, or retry windows are blocking access.

## Quick Diagnosis

| What you see | Likely cause | What to do |
|---|---|---|
| `Authentication failed` / `Invalid credentials` | ClickHouse rejected the username or password | Retry with the correct username/password for the selected connection. Each rejected attempt counts toward the lockout |
| `Connector offline` under the connection picker, `Connection unavailable` with `Connection offline`, or `Connection "<name>" is offline` | The connector for this connection is not connected (the second form appears when the login page already knows the connection is offline) | Start the connector/agent for the connection, then retry. If the local URL is wrong, use **Open setup guide**, update setup and restart CH-UI |
| `Connection unavailable` with `Connection to ClickHouse failed`, while the picker says `Connector online` | ClickHouse did not answer the credential check (timeout, connection refused, network or TLS error, tunnel dropped). `Connector online` only means the connector is connected; ClickHouse is checked when you sign in | Check that ClickHouse is up and reachable from the connector, start the connector if it is stopped, then retry. These attempts do not count toward the lockout |
| `Login temporarily blocked` / `Too many login attempts` | Temporary lockout after repeated failed logins (3 per user or 5 per IP within 15 minutes) | Wait for the retry window shown (`3m`, then `5m`, then capped at `10m`); if URL was wrong, fix setup and restart before retry |
| `No connections configured` | Embedded local connection not reachable/initialized | Start CH-UI with explicit local URL and connection name |

## Recovery From Login Screen

1. On login, click **Can't login?** to open the setup sheet directly.
2. Set:
   - `ClickHouse URL`
   - `Connection Name`
3. Restart CH-UI with one of these commands.

Primary (global install):

```bash
ch-ui server --clickhouse-url 'http://127.0.0.1:8123' --connection-name 'My Connection 1'
```

If running directly from the downloaded binary (no global install):

```bash
./ch-ui server --clickhouse-url 'http://127.0.0.1:8123' --connection-name 'My Connection 1'
```

## Docker Recovery

```bash
docker run --rm \
  -p 3488:3488 \
  -v ch-ui-data:/app/data \
  -e CLICKHOUSE_URL='http://127.0.0.1:8123' \
  -e CONNECTION_NAME='My Connection 1' \
  ghcr.io/caioricciuti/ch-ui:latest
```

## Environment and Config Alternatives

Environment variables:

```bash
CLICKHOUSE_URL='http://127.0.0.1:8123' CONNECTION_NAME='My Connection 1' ch-ui server
```

Config file (`server.yaml`):

```yaml
clickhouse_url: http://127.0.0.1:8123
connection_name: My Connection 1
```

Then:

```bash
ch-ui server -c /etc/ch-ui/server.yaml
```

## Notes

- Local URL setup does **not** require Admin access.
- Multiple connections are part of the free core; adding them in Admin needs the admin role, not Pro.
- Setup commands intentionally never include passwords.
- Credentials are entered only in the Sign in form after restart.
- Connection display-name priority:
  - `--connection-name`
  - `CONNECTION_NAME`
  - `server.yaml` (`connection_name`)
  - default `Local ClickHouse`
