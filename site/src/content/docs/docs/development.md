---
title: Development
description: Build CH-UI from source
---

## Requirements

- Go 1.26.6 or later (the version in `go.mod`)
- [Bun](https://bun.sh)

## Clone and Build

```bash
git clone https://github.com/caioricciuti/ch-ui.git
cd ch-ui
make build
ch-ui
```

If the binary is not installed globally, run `./ch-ui`.

## Dev Mode

Run the Go backend and frontend dev server separately. `make dev` starts the
server with `--dev`, which proxies the UI to Vite on port 5173:

```bash
# Terminal 1: backend
make dev

# Terminal 2: frontend
cd ui && bun install && bun run dev
```

## Make Targets

| Target | Description |
|---|---|
| `make build` | Full production build (frontend + Go binary). `make app` is the same |
| `make rebuild` | `make clean`, then `make build` |
| `make from-scratch` | Alias for `make rebuild` |
| `make build-frontend` | Build frontend only (`bun install --frozen-lockfile`, then the Vite build) |
| `make build-go` | Build Go binary only |
| `make dev` | Run the server in dev mode (expects Vite on `:5173`) |
| `make test` | Run all Go tests (`go test ./... -v -count=1`) |
| `make vet` | Run Go vet |
| `make tidy` | Run `go mod tidy` |
| `make clean` | Remove build artifacts |
| `make demo` | Start the SSO and audit demo stack (ClickHouse, Keycloak, CH-UI) with Docker Compose and bootstrap it |
| `make demo-bootstrap` | Re-run the demo's setup (admin login, SSO account, license) |
| `make demo-logs` | Tail the demo stack's logs |
| `make demo-down` | Tear down the demo stack and remove its volumes |
| `make help` | List the targets |

## Background Worker Tests Against ClickHouse

By default the background worker tests (schedules, models, pipelines, Cluster
Health, governance, telemetry monitors) run against protocol fixtures. Point
them at a disposable ClickHouse to run the real SQL instead. The user needs
rights to create users and grant privileges, because the tests create a
`worker_*` account per worker, rotate its password, then disable it.

```bash
export CHUI_TEST_CLICKHOUSE_URL=http://127.0.0.1:8123
export CHUI_TEST_CLICKHOUSE_USER=default
export CHUI_TEST_CLICKHOUSE_PASSWORD=secret

# the telemetry monitor test expects this table
curl --fail --user "$CHUI_TEST_CLICKHOUSE_USER:$CHUI_TEST_CLICKHOUSE_PASSWORD" \
  "$CHUI_TEST_CLICKHOUSE_URL" --data-binary \
  'CREATE TABLE default.otel_logs (Timestamp DateTime64(9), Body String) ENGINE=MergeTree ORDER BY Timestamp'

go test ./... -run TestBackgroundAccountExecution -count=1
```

Use a throwaway ClickHouse only. CI runs the same test on every pull request
and every push to `main`.
