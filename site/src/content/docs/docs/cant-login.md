---
title: Can't Login?
description: Fast recovery steps when CH-UI sign-in fails
---

Use this guide if the login screen appears but sign-in fails, local URL is wrong, or retry windows are blocking access.

## Quick Diagnosis

| What you see | Likely cause | What to do |
|---|---|---|
| `Authentication failed` / `Invalid credentials` | ClickHouse rejected the username or password | Retry with the correct username/password for the selected connection. Each rejected attempt counts toward the lockout |
| `Connector offline` under the connection picker, `Connection unavailable` with `Connection offline`, or `Connection "<name>" is offline` | The connector for this connection is not connected (the second form appears when the login page already knows the connection is offline) | Start the connector/agent for the connection, then retry. If the local URL is wrong, use **Set up ClickHouse connection** while [first-run setup](#first-run-setup-from-the-login-page) is open, or **Open setup guide**, update setup and restart CH-UI |
| `Connection unavailable` with `Connection to ClickHouse failed`, while the picker says `Connector online` | ClickHouse did not answer the credential check (timeout, connection refused, network or TLS error, tunnel dropped). `Connector online` only means the connector is connected; ClickHouse is checked when you sign in | Check that ClickHouse is up and reachable from the connector, start the connector if it is stopped, then retry. These attempts do not count toward the lockout |
| `Login temporarily blocked` / `Too many login attempts` | Temporary lockout after repeated failed logins (3 per user or 5 per IP within 15 minutes) | Wait for the retry window shown (`3m`, then `5m`, then capped at `10m`); if URL was wrong, fix setup and restart before retry |
| `No connections configured` | Embedded local connection not reachable/initialized | Use **Set up ClickHouse connection** with the setup code from the server log, or start CH-UI with explicit local URL and connection name |

## First-run setup from the login page

On a fresh install nobody has signed in yet, so nobody can fix a wrong
ClickHouse URL in Admin. For that case the login page can add a working
connection, gated by a one-time setup code that only someone with access to
the server log can read.

1. Find the setup code in the server log. While setup is open, the server
   prints it once at startup as a `WARN` line with a `setup_code` field
   (format `XXXX-XXXX-XXXX`). With Docker: `docker logs <container> 2>&1 | grep setup_code`.
2. On the login page, click **Set up ClickHouse connection**. It shows when
   there are no connections, under a connection whose connector is offline,
   and in the login error box.
3. Enter the setup code, a connection name and the ClickHouse URL
   (for example `http://clickhouse:8123`), then save.
4. The new connection is selected in the picker. Sign in with your ClickHouse
   username and password.

What setup does and does not do:

- It creates a new direct connection. It does not change the embedded
  connection, which is set from `clickhouse_url` / `CLICKHOUSE_URL` /
  `--clickhouse-url` on every start. Saving again while setup is still open
  updates the same setup connection instead of adding another one.
- The URL must be `http://` or `https://` with a host, and must not contain a
  username or password, a query string or a fragment. Link-local and cloud
  metadata addresses (such as `169.254.169.254` or `metadata.google.internal`)
  are refused. Loopback and private addresses are allowed.
- No ClickHouse credentials are stored. You enter them in the sign-in form.

When the code stops working:

- It expires 1 hour after startup.
- It is discarded after 10 wrong codes in total, from any client.
- It is discarded when the first admin signs in, with a password or SSO.
- One IP gets 5 wrong codes within 15 minutes before it is temporarily
  locked out, the same way as failed logins.

Restart CH-UI to get a new code. Setup closes for good after the first admin
signs in, and at startup when an admin already exists (for example after an
upgrade). Once closed, the button no longer shows and no code is printed; fix
the URL in **Admin > Connections** or with a restart as below.

:::note
The code lives in the memory of the server process that printed it. If you run
more than one CH-UI instance behind a load balancer, each prints its own code
and only accepts its own, so run setup against a single instance.
:::

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

- Fixing the embedded connection's URL with a restart needs access to the server, not the CH-UI admin role. First-run setup needs the setup code from the server log, also not the admin role.
- Multiple connections are part of the free core; adding them in Admin needs the admin role, not Pro.
- Setup commands intentionally never include passwords.
- Credentials are entered only in the Sign in form after restart.
- Connection display-name priority:
  - `--connection-name`
  - `CONNECTION_NAME`
  - `server.yaml` (`connection_name`)
  - default `Local ClickHouse`
