---
title: Troubleshooting
description: Common issues and how to resolve them
---

## Address already in use

```
listen tcp :3488: bind: address already in use
```

Another process is already using the port.

Check whether CH-UI is already running:

```bash
ch-ui server status
```

Then stop the old process:

```bash
ch-ui server stop
```

If `status` says the PID file is missing but the port is in use, you likely upgraded from an older binary without PID management. Stop the old process once, then restart with the current build.

## Connector auth failures (`invalid token`)

- Verify you copied the latest `cht_...` token.
- Check active connections with `ch-ui tunnel list`.
- Regenerate with `ch-ui tunnel rotate <connection-id>` (or create a new one with `ch-ui tunnel create --name ...`).
- Confirm the agent uses the correct `--url` and token pair.

## Login fails

CH-UI surfaces explicit login states (invalid credentials, offline connection, retry window). If login fails:

- Verify the selected connection is online.
- Confirm ClickHouse credentials are correct for that connection.
- Check connector logs for upstream errors.

Rejected credentials (and errors CH-UI cannot classify) count toward the rate
limit: 3 failures per user or 5 per IP within 15 minutes. If ClickHouse is unreachable, login returns `503` with
`Connection to ClickHouse failed` and the attempt is not counted. The login
screen shows this as **Connection unavailable**, with a hint to start the
connector for that connection and retry.

Rate-limit lockouts are progressive and capped:

- 1st lock: `3 minutes`
- 2nd lock: `5 minutes`
- 3rd+ lock: `10 minutes` (cap)

## Can't login? (local recovery)

If no admin has signed in yet, you can add a working connection from the login
page with the setup code printed in the server log, without a restart. See
[First-run setup from the login page](/docs/cant-login/#first-run-setup-from-the-login-page).

Otherwise, if local URL/connection setup is wrong, restart CH-UI with explicit values:

```bash
ch-ui server --clickhouse-url http://127.0.0.1:8123 --connection-name "My Connection 1"
```

Environment-variable equivalent:

```bash
CLICKHOUSE_URL=http://127.0.0.1:8123 CONNECTION_NAME="My Connection 1" ch-ui server
```

Docker:

```bash
docker run --rm -p 3488:3488 -v ch-ui-data:/app/data \
  -e CLICKHOUSE_URL='http://127.0.0.1:8123' \
  -e CONNECTION_NAME='My Connection 1' \
  ghcr.io/caioricciuti/ch-ui:latest
```

Full guide: [Can't Login?](/docs/cant-login)

## WebSocket / tunnel fails behind proxy

Your reverse proxy must forward WebSocket upgrades on `/connect`:

- Pass `Upgrade` and `Connection: upgrade` headers.
- Set long read/send timeouts (e.g. `proxy_read_timeout 3600`).
- Disable buffering for the tunnel path.

Example Nginx config for the `/connect` path:

```nginx
location /connect {
    proxy_pass http://127.0.0.1:3488/connect;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_read_timeout 3600;
}
```

## Health check

Use the health endpoint to verify the server is running:

```bash
curl http://localhost:3488/health
```

## Dashboards flicker / query history vanishes / `SESSION_IS_LOCKED`

You're running ClickHouse as multiple pods behind a load balancer. `system.*` tables are node-local, so without sticky routing every refresh hits a different pod. See [ClickHouse Clusters & Load Balancers](/docs/clusters) for the `X-CH-UI-Session` header config. Crucially, **don't** also enable ClickHouse's native `session_id` URL param, which serialises queries through a server-side mutex.
