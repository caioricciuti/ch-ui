---
title: CLI Reference
description: Complete command reference for the CH-UI binary
---

CH-UI ships as a single binary with subcommands for every operational task.

## Quick Start

Download and run locally (fastest way):

```bash
curl -L -o ch-ui https://github.com/caioricciuti/ch-ui/releases/latest/download/ch-ui-darwin-arm64
chmod +x ch-ui
sudo install -m 755 ch-ui /usr/local/bin/ch-ui
ch-ui
```

If you run from a downloaded binary without global install, prefix commands with `./ch-ui`. See [Installation](/docs/installation) for all platforms.

Remote setup (VM2 server + VM1 ClickHouse):

```bash
# VM2
ch-ui server start --detach
ch-ui tunnel create --name "vm1-clickhouse" --url wss://ch-ui.yourcompany.com/connect

# VM1
ch-ui connect --url wss://ch-ui.yourcompany.com/connect --key cht_xxx --clickhouse-url http://127.0.0.1:8123
```

## Top-Level Commands

```bash
ch-ui              # start server (default)
ch-ui server       # run CH-UI web app/API/gateway
ch-ui connect      # run connector agent next to ClickHouse
ch-ui tunnel       # create/manage tunnel keys on server host
ch-ui service      # install connector as OS service
ch-ui backup       # consistent snapshot of the CH-UI database
ch-ui uninstall    # best-effort local uninstall + cleanup hints
ch-ui update       # update binary to latest release
ch-ui version      # print version
ch-ui completion   # generate shell completion
ch-ui help         # show help
```

At startup every command reads a `.env` file in the current working directory,
if there is one. Each `KEY=value` line becomes an environment variable unless
that variable is already set, so real environment variables always win. Blank
lines and lines starting with `#` are skipped, and surrounding quotes are
stripped from values.

## `server`

Run the CH-UI web app, API, and tunnel gateway.

```bash
ch-ui server
ch-ui server start --detach
ch-ui server status
ch-ui server stop
ch-ui server restart
```

### Flags

| Flag | Description | Default |
|---|---|---|
| `--port, -p` | HTTP port | `3488` |
| `--clickhouse-url` | Local ClickHouse HTTP URL for embedded connection | `http://localhost:8123` |
| `--connection-name` | Display name for embedded local connection | `Local ClickHouse` |
| `--config, -c` | Path to `server.yaml` | auto-detected |
| `--detach` | Run in background (`server restart --detach` defaults to `true`) | `false` |
| `--pid-file` | Path to server PID file | `ch-ui-server.pid` |
| `--stop-timeout` | Graceful stop timeout | `10s` |
| `--dev` | Development mode (frontend proxy) | `false` |

## `connect`

Run the connector agent next to a ClickHouse instance. The agent opens an outbound WebSocket to the CH-UI server gateway.

```bash
ch-ui connect --url wss://ch-ui.yourcompany.com/connect --key cht_xxx --clickhouse-url http://127.0.0.1:8123
ch-ui connect --detach
ch-ui connect --takeover
```

### Flags

| Flag | Description | Default |
|---|---|---|
| `--url` | WebSocket tunnel URL (`ws://` or `wss://`) | `ws://127.0.0.1:3488/connect` |
| `--key` | Tunnel token (`cht_...`) | none |
| `--clickhouse-url` | ClickHouse HTTP endpoint | `http://localhost:8123` |
| `--config, -c` | Connector config file path | auto-detected |
| `--detach` | Run in background | `false` |
| `--takeover` | Replace currently connected agent for same token | `false` |

Note: if `--url` is omitted, the connector falls back to `TUNNEL_URL`, then the `tunnel_url` in its config file, then `ws://127.0.0.1:3488/connect`. For a remote server, point it at the server's `/connect` endpoint (`wss://your-ch-ui-server/connect`).

## `tunnel`

Create and manage tunnel keys on the server host. These commands must be run on the machine where `ch-ui.db` lives.

```bash
ch-ui tunnel create --name "vm1-clickhouse"
ch-ui tunnel list
ch-ui tunnel show <connection-id>
ch-ui tunnel rotate <connection-id>
ch-ui tunnel delete <connection-id>
```

### Flags

| Flag | Description |
|---|---|
| `--config, -c` | Path to `server.yaml` |
| `--db` | Override SQLite database path |
| `--url` | Public URL used when printing connect/service setup commands |
| `create --name` | Connection name (required) |
| `list --show-token` | Print full tokens instead of a masked preview |
| `delete --force` | Allow deleting the embedded connection, which is refused otherwise |

### Commands

| Command | What it does |
|---|---|
| `create` | Create a new connection and generate a tunnel token |
| `list` | List all tunnel connections |
| `show` | Show full token and setup commands for a connection |
| `rotate` | Rotate token. The old token stops working immediately |
| `delete` | Delete a tunnel connection |

## `service`

Install and manage the connector as an OS service (systemd / launchd).

```bash
ch-ui service install --key cht_xxx --url wss://ch-ui.yourcompany.com/connect --clickhouse-url http://127.0.0.1:8123
ch-ui service status
ch-ui service start
ch-ui service stop
ch-ui service restart
ch-ui service logs -f
ch-ui service uninstall
```

### Flags

| Flag | Description | Default |
|---|---|---|
| `install --key` | Tunnel token. Writes a fresh connector config file; without it, `install` needs an existing config file | none |
| `install --url` | Server WebSocket URL written to the config | `ws://127.0.0.1:3488/connect` |
| `install --clickhouse-url` | ClickHouse HTTP URL written to the config | `http://localhost:8123` |
| `logs --follow, -f` | Follow log output | `false` |
| `logs --lines, -n` | Number of log lines to show | `50` |
| `uninstall --purge` | Also remove binary and config files | `false` |
| `uninstall --force` | Force uninstall even if errors occur | `false` |

## `backup`

Create a consistent, point-in-time snapshot of the CH-UI SQLite database. Uses SQLite's `VACUUM INTO`, so the copy is fully consistent even while the server is running with WAL-mode writes in flight. A plain `cp` of the live database file is not.

```bash
ch-ui backup                       # writes ch-ui-backup-<timestamp>.db
ch-ui backup /backups/ch-ui.db     # explicit output file
```

The command refuses to overwrite an existing file.

### Flags

| Flag | Description | Default |
|---|---|---|
| `--config, -c` | Path to config file (used to locate the database) | auto-detected |
| `--database-path` | Path to the database (overrides config) | none |

> The backup contains credentials encrypted with `APP_SECRET_KEY`. Back that key up separately. It is not stored in the database, and a restore on another host needs the same key. To restore: stop the server, replace the database file with the backup, start with the same `APP_SECRET_KEY`.

## `update`

Download the latest release from GitHub and replace the current binary in
place. The download is checked against the release's `checksums.txt`; if that
file is missing, has no entry for your platform, or the hash does not match,
the update is refused and the old binary stays.

If a server is running (found through its PID file), `update` stops it and
starts the new version in the background. On Linux it reuses the running
server's arguments; elsewhere it starts `ch-ui server --pid-file <file>`. Pass
`--restart-server=false` to skip that and restart it yourself.

```bash
ch-ui update
ch-ui update --pid-file /var/run/ch-ui-server.pid
ch-ui update --restart-server=false
```

### Flags

| Flag | Description | Default |
|---|---|---|
| `--restart-server` | Restart a running server after the update | `true` |
| `--pid-file` | Server PID file used to find and restart the running server | `ch-ui-server.pid` |
| `--stop-timeout` | Graceful stop timeout when restarting | `10s` |

## `uninstall`

Best-effort local uninstall: stops services and processes, removes local CH-UI
files, and prints manual cleanup commands for anything that still needs a
privileged shell.

```bash
ch-ui uninstall
ch-ui uninstall --print-only
```

### Flags

| Flag | Description | Default |
|---|---|---|
| `--config, -c` | Server config file, used to locate the database | auto-detected |
| `--db` | Override the server SQLite database path | none |
| `--force` | Continue even if some steps fail | `false` |
| `--print-only` | Only print the cleanup commands, do not uninstall | `false` |
| `--pid-file` | Extra server PID file to stop and remove (repeatable) | none |

## Other Commands

```bash
ch-ui version           # print version
ch-ui completion bash   # generate shell completion (bash, zsh, fish, powershell)
ch-ui help              # show help
```
