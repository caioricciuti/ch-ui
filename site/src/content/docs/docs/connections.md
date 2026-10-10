---
title: Direct vs Tunnel Connections
description: The two ways CH-UI reaches ClickHouse, when to use each, and how to set them up
---

A **connection** is one ClickHouse environment in CH-UI. There are two kinds, and they differ only in who opens the network connection to ClickHouse:

- **Direct**: the CH-UI server calls the ClickHouse HTTP(S) interface itself. You give it a URL and it connects right away.
- **Tunnel**: a small connector (`ch-ui connect`) runs next to ClickHouse and dials out to the CH-UI server over a WebSocket. ClickHouse never needs to accept a connection from the CH-UI host.

Both kinds are in the open-source core. Creating, editing and deleting connections needs the `admin` role, not a Pro license.

## Which one to use

Use **direct** when the CH-UI server can reach ClickHouse's HTTP port: same host, same network, a Docker network, or a managed ClickHouse with a public HTTPS endpoint. It is the simpler setup, with no extra process and no token.

Use a **tunnel** when it cannot: ClickHouse sits behind a firewall or NAT, in another VPC, or in a network that only allows outbound traffic. The connector only needs to reach the CH-UI server.

| | Direct | Tunnel |
|---|---|---|
| Who talks to ClickHouse | The CH-UI server | The `ch-ui connect` process next to ClickHouse |
| Network needed | CH-UI server to ClickHouse HTTP(S) port | Connector to CH-UI server (`ws://` or `wss://`), outbound only |
| Extra process | None | `ch-ui connect`, usually installed as a service |
| Token | None to manage | A `cht_...` token per connection, can be rotated |
| TLS to ClickHouse | Verified against the CH-UI host's system CAs | Verified against the connector host's CAs, can be skipped on the connector |
| Created with | `CLICKHOUSE_URL`, Admin, or the API | `ch-ui tunnel create`, Admin, or the API |

Internally each direct connection runs the same connector code inside the server process, dialing the server's own gateway. So everything above the connection (queries, sessions, governance, schedules, background accounts) works the same for both kinds.

## No CORS or mixed-content problems

With either kind, your browser only talks to CH-UI. The ClickHouse calls are made by the server (direct) or the connector (tunnel), never by the browser. You do not need CORS headers on ClickHouse, and an HTTPS CH-UI can use a plain `http://` ClickHouse URL without mixed-content errors.

## Direct connections

### The connection from server config

The server creates one direct connection on startup from its config, called the embedded connection. By default it is named "Local ClickHouse" and points at `http://localhost:8123`. Change it with environment variables, flags or `config.yaml`:

```bash
CLICKHOUSE_URL=http://clickhouse:8123 CONNECTION_NAME="Production" ch-ui server
# or
ch-ui server --clickhouse-url http://clickhouse:8123 --connection-name "Production"
```

The server config is the source of truth for this connection: it is updated on every start, and Admin and the API cannot edit it. In **Admin > Connections** its row shows **Server config** instead of Edit and Delete buttons, and `PUT /api/connections/{id}` returns `400` for it. See [Configuration](/docs/configuration).

### The connection from first-run setup

If the embedded connection cannot reach ClickHouse on a fresh install, nobody can sign in to fix it. Until the first admin signs in, the login page offers **Set up ClickHouse connection**: with the one-time setup code from the server log, it adds a separate direct connection with the name and URL you enter. The embedded connection is left as it is and still follows the server config. After setup closes, the setup connection is an ordinary direct connection that admins can rename, repoint or delete.

While the embedded connection still has the URL it had when setup ran, the login page hides it, so the picker does not offer a connection that cannot work. Change `clickhouse_url` in the server config and restart, and it appears again. **Admin > Connections** always lists it. See [First-run setup from the login page](/docs/cant-login/#first-run-setup-from-the-login-page).

### More direct connections

Add as many as you like in **Admin > Connections > Add connection**. The form defaults to **Direct URL**: enter a name and the ClickHouse URL, and the connection starts right away.

With the API (admin session):

```bash
curl -X POST http://localhost:3488/api/connections \
  -H "Content-Type: application/json" \
  -H "Cookie: chui_session=..." \
  -d '{"name":"Staging","type":"direct","clickhouse_url":"http://clickhouse-staging:8123"}'
```

`clickhouse_url` is required for direct connections and must be an `http://` or `https://` URL with a host. The `201` response is `{ "connection": {...} }`.

To rename a direct connection or point it at another URL, use the **Edit** button on its row in **Admin > Connections**. Saving a new URL restarts that connection's connector against the new target. The same through the API:

```bash
curl -X PUT http://localhost:3488/api/connections/{id} \
  -H "Content-Type: application/json" \
  -H "Cookie: chui_session=..." \
  -d '{"name":"Staging EU","clickhouse_url":"https://clickhouse-staging.internal:8443"}'
```

Both fields are optional; send only what changes. `clickhouse_url` is validated like on create and can only be set on direct connections. The response is the updated connection object, and a request that changes something is written to the audit log as `connection.updated`. Deleting a direct connection stops its connector.

There is no option to skip TLS verification for direct connections. If ClickHouse uses a self-signed certificate, add its CA to the trust store of the CH-UI host, or use a tunnel connection and set `insecure_skip_verify` on the connector.

## Tunnel connections

Create the connection on the CH-UI server, then run the connector next to ClickHouse with its token:

```bash
# On the CH-UI server host
ch-ui tunnel create --name "Production EU"
ch-ui tunnel show <connection-id>

# Next to ClickHouse
ch-ui connect \
  --url wss://ch-ui.yourcompany.com/connect \
  --key cht_your_token \
  --clickhouse-url http://127.0.0.1:8123
```

In Admin, choose **Remote agent** in **Add connection** to get a token instead. Through the API, omit `type` (or send `"type":"tunnel"`); the response includes `tunnel_token` and `setup_instructions`. A tunnel connection can be renamed with **Edit** in Admin or `PUT /api/connections/{id}` with `{"name":"..."}`; the agent and its token are not affected. Its ClickHouse URL is set on the connector, not on the server. Token rotation, service install and the rest are covered in [Connections & Tunnel](/docs/ingestion) and the [CLI reference](/docs/cli).

The connector reads its settings from flags, from its config file (`~/.config/ch-ui/config.yaml` on macOS, `/etc/ch-ui/config.yaml` on Linux), or from the environment: `TUNNEL_TOKEN`, `TUNNEL_URL`, `CLICKHOUSE_URL`. To skip TLS verification, for example with a self-signed certificate, set `insecure_skip_verify: true` in the config file or `TUNNEL_INSECURE_SKIP_VERIFY=true`. It applies to both the connector's HTTPS calls to ClickHouse and its `wss://` connection to CH-UI, so use it only on networks you trust.

## Live query progress

While a query runs, the editor shows rows and bytes read, elapsed time, and a percentage when ClickHouse can estimate the total rows. The connector (in-process for direct, `ch-ui connect` for tunnel) samples `system.processes` for the running query with the same ClickHouse user that runs it. That user needs read access:

```sql
GRANT SELECT ON system.processes TO your_user;
```

Without it, the query still runs normally; you just get no progress, and the connector stops sampling for that query after the first refusal.

The samples run with `log_queries=0`, so they never show up in `system.query_log`, and so never in your query history, the governance query audit or Query Insights.

## Check that it works

- **Admin > Connections** shows each connection's type (Embedded, Direct or Agent), target and Online or Offline status. `ch-ui tunnel list` shows the same for tunnels.
- On the login screen, pick the connection and sign in with a ClickHouse user.
- If a connection stays offline, see [Troubleshooting](/docs/troubleshooting).
