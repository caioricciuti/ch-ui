---
title: Configuration
description: Server and connector configuration for CH-UI
---

CH-UI works without config files out of the box. You only need config files when you want production defaults, service-managed startup, or want to avoid passing flags every time.

## Priority Order

Values are resolved in this order (highest wins):

- **Server**: CLI flags > environment variables > `server.yaml` > built-in defaults
- **Connector**: CLI flags > environment variables > `config.yaml` > built-in defaults

### `.env` file

On startup the `ch-ui` binary reads a `.env` file from the current working
directory, if one exists, for both the server and the connector. Each
`KEY=value` line is set as an environment variable only when that variable is
not already set, so real environment variables always take precedence. Lines
starting with `#` are ignored and surrounding quotes are stripped.

```bash
# .env
APP_URL=https://ch-ui.yourcompany.com
APP_SECRET_KEY="replace-with-a-long-random-secret"
```

## Server Config

Default config path:

- macOS: `~/.config/ch-ui/server.yaml`
- Linux: `/etc/ch-ui/server.yaml`

### Server config explained

```yaml
port: 3488
app_url: https://ch-ui.yourcompany.com
database_path: /var/lib/ch-ui/ch-ui.db
clickhouse_url: http://localhost:8123
connection_name: Local ClickHouse
app_secret_key: "change-this-in-production"
allowed_origins:
  - https://ch-ui.yourcompany.com
# optional override:
# tunnel_url: wss://ch-ui.yourcompany.com/connect
# trusted networks only, see below:
# allow_login_url: false
# reverse proxies whose X-Forwarded-For counts, see below:
# trusted_proxies:
#   - 10.0.0.0/8
```

Unknown top-level keys are ignored, but the server logs a warning at startup
listing them (`Config file has unknown keys; they are ignored`), so a typo in a
key name shows up in the log.

| Key | Example | Default | Why it matters |
|---|---|---|---|
| `port` | `3488` | `3488` | HTTP port used by CH-UI server |
| `app_url` | `https://ch-ui.yourcompany.com` | `http://localhost:<port>` | Public URL for links and tunnel URL inference |
| `database_path` | `/var/lib/ch-ui/ch-ui.db` | `./data/ch-ui.db` | Where CH-UI stores app state |
| `clickhouse_url` | `http://localhost:8123` | `http://localhost:8123` | Embedded local connection target |
| `connection_name` | `Local ClickHouse` | `Local ClickHouse` | Display name for embedded local connection |
| `app_secret_key` | random long string | auto-generated | Encrypts stored credentials. When unset, CH-UI generates a random key on first start and persists it to `.app_secret_key` next to the database. Back it up with the DB |
| `allowed_origins` | `["https://ch-ui.yourcompany.com"]` | empty | CORS allowlist |
| `tunnel_url` | `wss://ch-ui.yourcompany.com/connect` | derived from port | Explicit tunnel endpoint advertised to agents |
| `tls_cert_file` | `/etc/ch-ui/tls/server.crt` | empty | PEM cert for native TLS (with `tls_key_file`) |
| `tls_key_file` | `/etc/ch-ui/tls/server.key` | empty | PEM key for native TLS |
| `session_max_age` | `86400` | `604800` (7 days) | Session lifetime in seconds |
| `allow_login_url` | `true` | `false` | Lets the login page take a ClickHouse URL instead of a saved connection. Trusted networks only, see [Sign in with a ClickHouse URL](#sign-in-with-a-clickhouse-url) |
| `trusted_proxies` | `["10.0.0.0/8", "192.168.1.20"]` | loopback, private and link-local ranges | Reverse proxies whose `X-Forwarded-For`, `X-Real-IP`, `X-Forwarded-Proto` and `X-Forwarded-Host` headers are believed. `[]` trusts none. See [Behind a reverse proxy](#behind-a-reverse-proxy) |

`clickhouse_url` and `connection_name` are applied to the embedded connection
on every start. A connection added with the first-run setup code on the login
page is a separate direct connection and is not overwritten by them. See
[Can't login?](/docs/cant-login/#first-run-setup-from-the-login-page).

### Sign in with a ClickHouse URL

Off by default. When on, the connection picker on the login page gets an
**Other ClickHouse URL** option: type a ClickHouse URL, a username and a
password, and sign in without an admin adding the connection first, much like
CH-UI v1.

Set it in any of these ways (the flag wins over the environment variable,
which wins over `server.yaml`):

```bash
ch-ui server --allow-login-url
ALLOW_LOGIN_URL=true ch-ui server
```

```yaml
allow_login_url: true
```

`ALLOW_LOGIN_URL` reads `true`, `1` or `yes` (any case) as on and any other
value as off, so `ALLOW_LOGIN_URL=false` turns it off even when `server.yaml`
has it on. The flag is passed on when the server starts with `--detach`, and
`ch-ui update` keeps it when it restarts the server.

### Behind a reverse proxy

Per-IP login and first-run setup limits, the per-IP limits on the OAuth and
public dashboard endpoints, the `ip_address` column of the audit log and the
origin advertised in the MCP OAuth metadata all need the real client address.
Behind a reverse proxy that address arrives in `X-Forwarded-For` (or
`X-Real-IP`), and the public scheme and host in `X-Forwarded-Proto` and
`X-Forwarded-Host`. Any client can send those headers too, so CH-UI believes
them only when the TCP connection comes from a trusted proxy.

`trusted_proxies` lists those proxies as IP addresses or CIDRs. When it is
unset, CH-UI trusts the loopback, private (RFC 1918 and IPv6 unique-local)
and link-local ranges, which covers a proxy on the same host, in the same
Docker network or in the same cluster. Set it when your proxy has a public
address, or to narrow the default to exactly your proxies. An empty list
trusts no proxy at all; then every request is attributed to its TCP peer and
forwarding headers are ignored.

```yaml
trusted_proxies:
  - 10.0.0.0/8
  - 192.168.1.20
```

```bash
TRUSTED_PROXIES=10.0.0.0/8,192.168.1.20 ch-ui server
ch-ui server --trusted-proxies 10.0.0.0/8,192.168.1.20
TRUSTED_PROXIES=none ch-ui server   # trust no proxy
```

The flag wins over the environment variable, which wins over `server.yaml`.
An entry that is not an IP or CIDR stops the server at startup with
`invalid trusted_proxies`. `X-Forwarded-For` is read from the proxy's end:
the hops added by trusted proxies are skipped and the first address that is
not one of them is the client, so a client cannot prepend a made-up address.
When a request from an address outside the trusted ranges carries forwarding
headers, the server logs `Ignoring X-Forwarded-For from an address that is
not a trusted proxy` once for that address. If you see it for your own proxy,
add the proxy to `trusted_proxies`; until you do, every user behind it shares
one per-IP login limit.

:::caution
In v1 the browser connected to ClickHouse. Here the CH-UI server makes the
connection, so anyone who can open the login page can make the server connect
to any address the server can reach. Enable it only where everyone who can
reach the login page is trusted: CH-UI on your laptop or desktop, or on a
private LAN. The server logs a `WARN` line at startup while it is on.
:::

How it behaves:

- The URL follows the same rules as
  [first-run setup](/docs/cant-login/#first-run-setup-from-the-login-page):
  `http://` or `https://` with a host, no username or password, no query
  string or fragment, no link-local or cloud metadata address.
- The server keeps one direct connection per URL (scheme and host compared
  without case, trailing slashes ignored). Signing in again with the same URL
  reuses it, and so does a URL that matches a direct connection an admin
  created. The embedded connection is never reused this way.
- A new connection is named `host:port` from the URL and stays in the picker
  for next time. It is created before the credentials are checked, so a
  failed sign-in can still leave it behind.
- The login page creates at most 20 connections. When that many still exist,
  a new URL is refused until an admin deletes unused ones in
  **Admin > Connections**. Reusing an existing one still works.
- Each new connection is audited as `connection.created_from_login`.

See [Direct vs Tunnel Connections](/docs/connections/#connections-from-the-login-page).

`clickhouse_url` and `connection_name` are applied to the embedded connection
on every start. A connection added with the first-run setup code on the login
page is a separate direct connection and is not overwritten by them. See
[Can't login?](/docs/cant-login/#first-run-setup-from-the-login-page).

### Native TLS

Set both `tls_cert_file` and `tls_key_file` to have CH-UI serve HTTPS directly.
If unset, CH-UI serves plaintext HTTP and expects a reverse proxy to terminate
TLS; when bound to a non-loopback address without TLS it logs a startup warning.
See [Security](/docs/security).

### Audit forwarding (SIEM)

Optionally stream audit events to your tooling (the authoritative copy stays in
the database). See [Monitoring & SIEM](/docs/monitoring).

```yaml
audit_forward_stdout: true
audit_log_file: /var/log/ch-ui/audit.jsonl
audit_webhook_url: https://siem.example.com/hook
```

### OIDC SSO

```yaml
oidc_issuer_url: https://accounts.google.com
oidc_client_id: your-client-id
oidc_client_secret: your-client-secret
oidc_redirect_url: https://ch-ui.yourcompany.com/api/auth/oidc/callback
# Optional:
oidc_allowed_domains: [yourcompany.com]
oidc_groups_claim: groups
oidc_admin_groups: [ch-ui-admins]
oidc_analyst_groups: [data-analysts]
oidc_connection_id: <connection-id>
```

`oidc_groups_claim` is the ID-token claim that holds group memberships
(default `groups`). `oidc_connection_id` picks the connection SSO sessions use;
when unset, CH-UI uses the embedded connection, or the first connection if there
is no embedded one. SSO login is Pro.

Full setup in [Single Sign-On](/docs/sso).

### Server environment variables

| Variable | Description |
|---|---|
| `PORT` | HTTP port |
| `APP_URL` | Public base URL |
| `DATABASE_PATH` | SQLite path |
| `CLICKHOUSE_URL` | Local ClickHouse URL for embedded connector |
| `CONNECTION_NAME` | Display name for embedded local connection |
| `CONNECITION_NAME` | Backward-compatible typo alias for `CONNECTION_NAME` |
| `APP_SECRET_KEY` | Session/password encryption secret |
| `ALLOWED_ORIGINS` | Comma-separated CORS origins |
| `ALLOW_LOGIN_URL` | Sign in with a ClickHouse URL on the login page (`true`, `1` or `yes`; default off). Trusted networks only |
| `TRUSTED_PROXIES` | Comma-separated IPs or CIDRs of reverse proxies whose forwarding headers are believed; `none` trusts no proxy. Default: loopback, private and link-local ranges |
| `TUNNEL_URL` | Override gateway URL |
| `TLS_CERT_FILE` / `TLS_KEY_FILE` | PEM cert/key for native TLS |
| `AUDIT_FORWARD_STDOUT` / `AUDIT_LOG_FILE` / `AUDIT_WEBHOOK_URL` | Audit forwarding sinks |
| `OIDC_ISSUER_URL` / `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET` / `OIDC_REDIRECT_URL` | OIDC SSO |
| `OIDC_CONNECTION_ID` | Connection SSO sessions use (default: the embedded connection, else the first) |
| `OIDC_ALLOWED_DOMAINS` / `OIDC_ADMIN_GROUPS` / `OIDC_ANALYST_GROUPS` / `OIDC_GROUPS_CLAIM` | OIDC mapping |
| `CHUI_LICENSE_FILE` | Path to a Pro license JSON file (e.g. a mounted Kubernetes Secret) |
| `CHUI_LICENSE` | Pro license JSON inline (file takes precedence) |
| `SESSION_MAX_AGE` | Session lifetime in seconds (default `604800`, i.e. 7 days) |
| `NODE_ENV` | `development` enables dev mode (relaxed CORS and security headers). The `server` command's `--dev` flag overrides it; `NODE_ENV` is honored for other entry points |
| `CHUI_SQLITE_MAX_OPEN_CONNS` | Max SQLite connections (default 8) |
| `CHUI_VITE_MINIFY` | Build-time: toggle frontend build minification (`0` disables) |
| `VITE_BASE_PATH` | Build-time: base path for hosting the UI under a sub-path (e.g. `/ch-ui`), read by `ui/vite.config.ts` |

## Connector Config

Default config path:

- macOS: `~/.config/ch-ui/config.yaml`
- Linux: `/etc/ch-ui/config.yaml`

### Connector config explained

```yaml
tunnel_token: "cht_your_token"
clickhouse_url: "http://127.0.0.1:8123"
tunnel_url: "wss://ch-ui.yourcompany.com/connect"
# insecure_skip_verify: false
```

| Key | Example | Default | Why it matters |
|---|---|---|---|
| `tunnel_token` | `cht_...` | none (required) | Auth key created on server (`ch-ui tunnel create`) |
| `clickhouse_url` | `http://127.0.0.1:8123` | `http://localhost:8123` | Local ClickHouse for this VM |
| `tunnel_url` | `wss://ch-ui.yourcompany.com/connect` | `ws://127.0.0.1:3488/connect` | Your CH-UI server's `/connect` gateway endpoint. Set it for any remote server |
| `insecure_skip_verify` | `false` | `false` | Skips TLS certificate checks on both the tunnel and the connector's requests to ClickHouse. Only for dev setups with self-signed certs |

### Connector environment variables

| Variable | Description |
|---|---|
| `TUNNEL_TOKEN` | Tunnel token (`cht_...`) |
| `CLICKHOUSE_URL` | ClickHouse HTTP endpoint |
| `TUNNEL_URL` | WebSocket URL to `/connect` |
| `TUNNEL_INSECURE_SKIP_VERIFY` | Same as `insecure_skip_verify` (`true`, `1` or `yes`) |

The server has no equivalent setting. There is no server-side
`INSECURE_SKIP_VERIFY`, and connections the server runs itself (the embedded
connection) always verify ClickHouse TLS certificates.

## Minimal Production Templates

### Server (`/etc/ch-ui/server.yaml`)

```yaml
port: 3488
app_url: https://ch-ui.yourcompany.com
database_path: /var/lib/ch-ui/ch-ui.db
connection_name: Local ClickHouse
app_secret_key: "replace-with-a-long-random-secret"
allowed_origins:
  - https://ch-ui.yourcompany.com
```

### Connector (`/etc/ch-ui/config.yaml`)

```yaml
tunnel_token: "cht_replace_me"
clickhouse_url: "http://127.0.0.1:8123"
tunnel_url: "wss://ch-ui.yourcompany.com/connect"
```

## Recommended Production Values

- Rotate `APP_SECRET_KEY` per environment.
- Use `wss://` for all connector tunnels.
- Keep connector close to ClickHouse to reduce latency.
- Use non-default file paths managed by system services.

## Validation Commands

```bash
ch-ui server status
ch-ui service status
```
