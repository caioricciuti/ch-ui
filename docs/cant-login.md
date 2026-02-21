# Can't Login?

Use this guide when CH-UI loads but sign-in fails, local connection is wrong, or you are blocked by retry windows.

## Quick Diagnosis

| What you see | Most likely cause | What to do |
|---|---|---|
| `Authentication failed` | Wrong ClickHouse username/password | Retry with correct credentials for the selected connection |
| `Connection unavailable` / `Unreachable` | Local ClickHouse URL is wrong or connector is offline | Update local URL/name, restart CH-UI, then retry |
| `Too many login attempts` | Repeated failed attempts triggered temporary lock | Wait retry window; if URL/connection was wrong, fix setup and restart before retrying |
| No connections configured | Embedded local connection was not created/updated correctly | Run setup command below and restart CH-UI |

## Local Recovery (Recommended)

1. Open **Can't login?** in CH-UI login.
2. Set:
   - `ClickHouse URL`
   - `Connection Name`
3. Restart CH-UI with one of these commands.

Global install:

```bash
ch-ui server --clickhouse-url 'http://127.0.0.1:8123' --connection-name 'My Connection 1'
```

Local binary:

```bash
./ch-ui server --clickhouse-url 'http://127.0.0.1:8123' --connection-name 'My Connection 1'
```

Then open `http://localhost:3488` and sign in again.

## Docker Recovery

```bash
docker run --rm \
  -p 3488:3488 \
  -v ch-ui-data:/app/data \
  -e CLICKHOUSE_URL='http://127.0.0.1:8123' \
  -e CONNECTION_NAME='My Connection 1' \
  ghcr.io/caioricciuti/ch-ui:latest
```

## Env And Config Alternatives

Environment variables:

```bash
CLICKHOUSE_URL='http://127.0.0.1:8123' CONNECTION_NAME='My Connection 1' ch-ui server
```

Config file (`server.yaml`):

```yaml
clickhouse_url: http://127.0.0.1:8123
connection_name: My Connection 1
```

## Notes

- Local URL setup does **not** require Admin access.
- Admin and multi-connection management are Pro-only features.
- Setup commands intentionally exclude passwords; credentials stay in the Sign in form.
- Connection name precedence: `--connection-name` > `CONNECTION_NAME` > `server.yaml` > `Local ClickHouse`.
