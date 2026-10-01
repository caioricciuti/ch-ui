---
title: Connections & Tunnel
description: Connect remote ClickHouse instances to CH-UI
---

In CH-UI, a **connection** represents one ClickHouse environment (dev, staging, prod, etc).

A connection is either **direct** (the CH-UI server talks to ClickHouse over HTTP(S) itself) or **tunnel** (a `ch-ui connect` agent next to ClickHouse dials in with a token). This page covers creating tunnel connections and managing their tokens. For when to use which, and for creating direct connections, see [Direct vs tunnel connections](/docs/connections).

## Create a Connection

### Option A: CLI

Run on the CH-UI server host (where `ch-ui.db` lives):

```bash
ch-ui tunnel create --name "Production EU"
ch-ui tunnel list
ch-ui tunnel show <connection-id>
```

Copy token and setup command from `tunnel show`.

Optional UI path (admin role): **Admin > Connections > Add connection**, then choose **Remote agent** (the form defaults to Direct URL). No Pro license is needed.

Note: local first-run URL fixes are not done in Admin; use startup flags (`--clickhouse-url`, `--connection-name`) or [Can't login?](/docs/cant-login).

### Option B: API

Creating a connection needs the admin role.

```bash
# Tunnel connection (the default when "type" is omitted)
curl -X POST http://localhost:3488/api/connections \
  -H "Content-Type: application/json" \
  -H "Cookie: chui_session=..." \
  -d '{"name":"Production EU"}'
```

The `201` response holds `connection`, `tunnel_token` and `setup_instructions` (the `connect` and `service` commands to run next to ClickHouse).

A direct connection needs no agent or token. Pass `"type":"direct"` and an `http://` or `https://` URL the server can reach:

```bash
curl -X POST http://localhost:3488/api/connections \
  -H "Content-Type: application/json" \
  -H "Cookie: chui_session=..." \
  -d '{"name":"Staging","type":"direct","clickhouse_url":"http://clickhouse-staging:8123"}'
```

The `201` response holds only `connection`, and the server starts connecting right away.

## Start Connector

```bash
ch-ui connect \
  --url wss://ch-ui.yourcompany.com/connect \
  --key cht_your_token \
  --clickhouse-url http://127.0.0.1:8123
```

## Validate Connectivity

- Check connection online status in `ch-ui tunnel list` (or in Admin, with the admin role).
- Run **Test Connection** with ClickHouse credentials.
- Login using that connection in CH-UI.

## Tunnel Key Management (CLI)

Run these commands on the CH-UI server host (the VM where `ch-ui.db` lives):

```bash
# Create a connection + key
ch-ui tunnel create --name "vm1-clickhouse"

# List all tunnel connections
ch-ui tunnel list

# Show full token + setup commands for one connection
ch-ui tunnel show <connection-id>

# Rotate token (old token becomes invalid immediately)
ch-ui tunnel rotate <connection-id>

# Delete a tunnel connection
ch-ui tunnel delete <connection-id>
```

### Useful flags

| Flag | Description |
|---|---|
| `--config, -c` | Use a specific server config file |
| `--db` | Override SQLite path directly |
| `--url` | Force public WebSocket URL used in generated setup commands |

## Token Operations

For a connection you can:

- Get token
- Regenerate token
- Revoke by deleting connection

Regenerate if token leakage is suspected.

## Multi-Environment Pattern

Use one connection per environment:

- `dev`
- `staging`
- `production`

This keeps sessions, governance state, and audit logs scoped cleanly by environment.
