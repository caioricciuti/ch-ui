# Connecting CH-UI to ClickHouse

The browser never talks to ClickHouse directly. The CH-UI **server** owns the UI
and API; an **agent** holds the ClickHouse HTTP client and executes queries. There
are two supported ways to wire the agent to ClickHouse — pick based on how much you
want to expose ClickHouse to the network.

## 1. Direct connection (embedded agent)

Point CH-UI's in-process agent straight at a ClickHouse HTTP endpoint with
`CLICKHOUSE_URL`. This is the simplest setup and works fine against a
reverse-proxied HTTPS endpoint (Nginx, Caddy, Traefik, …):

```bash
CLICKHOUSE_URL=https://clickhouse.example.com ch-ui server
```

- The URL must start with `http://` or `https://`. TLS is honored.
- For an internal CA / self-signed certificate, set `INSECURE_SKIP_VERIFY=true`
  (or `insecure_skip_verify: true` in the config file).
- Auth is passed per-connection via the ClickHouse user/password — not in the URL.

Because the **server** opens the connection (not the browser), none of the
browser-side concerns apply: there are no CORS, mixed-content, or browser
TLS/protocol limitations to work around.

Use this when you're comfortable exposing ClickHouse's HTTP interface (typically
behind a TLS-terminating reverse proxy).

## 2. Tunnel connection (`ch-ui connect`)

When you'd rather **not expose ClickHouse publicly at all**, run an agent next to
ClickHouse with `ch-ui connect`. The agent dials **outbound** to the CH-UI server
over a secure WebSocket and forwards queries to its local ClickHouse, so ClickHouse
keeps zero inbound ports open:

```bash
ch-ui connect \
  --url wss://ch-ui.example.com/connect \
  --key cht_your_tunnel_token \
  --clickhouse-url http://127.0.0.1:8123
```

ClickHouse can stay bound to `127.0.0.1` and the only egress required is the
outbound WebSocket to the CH-UI server. See the
[production runbook](production-runbook.md) for the full server + connector setup.

## Which one?

| | Direct (`CLICKHOUSE_URL`) | Tunnel (`ch-ui connect`) |
|---|---|---|
| ClickHouse network exposure | HTTP(S) endpoint reachable by the server | None (outbound WebSocket only) |
| TLS to ClickHouse | Yes (`https://`, `INSECURE_SKIP_VERIFY` for self-signed) | N/A — local hop on the agent host |
| Best for | A reverse-proxied or already-reachable ClickHouse | Keeping ClickHouse fully private |

Both are first-class — the tunnel is a zero-exposure convenience, not a sign that
the reverse-proxy model is unsupported.
