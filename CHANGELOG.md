# Changelog

All notable changes to CH-UI are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- Command palette no longer enters an infinite Svelte update loop
  (`effect_update_depth_exceeded`) when opened: the open routine runs `loadAll()`
  inside `untrack()` so its writes to the databases/data stores can't re-trigger
  the effect that started it (#129).
- `⌘/Ctrl+K` now toggles the command palette, so pressing it again closes the
  palette as the help text describes (#129).

## [2.5.2] - 2026-06-29

### Fixed

- Improve destructive-action alert contrast in the light theme so error and
  delete-confirmation panels in the database explorer are legible (#111).

### Changed

- Bump the Go dependency group: `IBM/sarama` 1.47.0→1.50.3, `coreos/go-oidc/v3`
  3.18.0→3.19.0, `go-chi/chi/v5` 5.2.5→5.3.0, `go-sql-driver/mysql`
  1.9.3→1.10.0, `minio/minio-go/v7` 7.0.98→7.2.0, `lib/pq` 1.11.2→1.12.3,
  `modernc.org/sqlite` 1.44.3→1.52.0, `fatih/color` 1.18.0→1.19.0 (#126).
- Bump `@types/node` 25→26 in the UI dev dependencies (#127).
- Bump `actions/checkout` 6→7 in the CI and release workflows (#125).

## [2.5.1] - 2026-06-15

### Security

- Rebuild release binaries and the Docker image on Go 1.25.11, patching 23
  standard-library vulnerabilities reachable from the codebase (crypto/x509,
  crypto/tls, net/http, net/textproto, mime, net/url, os, …).
- Bump `golang.org/x/net` to v0.56.0 (GO-2026-4918).

### Fixed

- CI: the backend job now compiles (a `//go:embed ui/dist` placeholder), so
  `go vet`/`go test`/`govulncheck` actually run on every PR and push.

### Changed

- Bump GitHub Actions to Node 24-compatible versions (checkout v6, setup-go v6,
  docker buildx v4 / login v4 / build-push v7).

## [2.5.0] - 2026-06-15

### Security

- Audit failed login attempts (`user.login_failed`) in the immutable audit trail,
  not just successful logins — enables brute-force and credential-stuffing detection.
- Require admin role to create or delete a connection and to read or rotate a
  connection's tunnel token. Reads (list/get/test) remain available to any
  authenticated user.
- Admin-only access to the audit-log read and export endpoints (the trail
  contains other users' usernames, IPs, and query text).
- `viewer` role is now read-only on shared workspace objects (dashboards,
  pipelines, models, saved queries): create/edit/delete and pipeline/model runs
  require `admin` or `analyst`. Viewing and running queries are unchanged and
  remain governed by each user's ClickHouse grants.
- Sanitize all rendered Markdown (Brain AI output and dashboard text panels,
  including unauthenticated public dashboard share links) with DOMPurify to close
  a stored-XSS vector. External links now open with `rel="noopener noreferrer"`.
- Per-IP rate limiting on the unauthenticated public-dashboard endpoints, and a
  32 MB cap on request bodies.
- Native TLS termination (`tls_cert_file`/`tls_key_file`); when serving plaintext
  HTTP the server now logs a prominent warning instead of staying silent.

### Added

- **OIDC Single Sign-On (Pro)**: log in via any OpenID Connect provider
  (Okta/Entra/Google/Keycloak). OIDC authenticates the person (identity, role,
  and audit are per-person); queries run through a per-connection ClickHouse
  service account. Role is mapped from IdP groups, with optional email-domain
  restriction. The flow uses state + nonce and verifies the ID token. Password
  login keeps working alongside. See `docs/sso.md`.
- **License grace period**: an expired Pro license now enters a 14-day read-only
  window (monitoring keeps working, writes are blocked) instead of hard-locking
  the installation at the moment of expiry.
- **Prometheus `/metrics`** endpoint (HTTP counters, latency, in-flight, Go
  runtime, build info) — no external dependency.
- **Audit forwarding (SIEM, Pro)**: optionally stream audit events to a webhook,
  a JSONL file, or structured stdout, plus an admin CSV/JSON export endpoint. The
  authoritative copy always stays in the database.
- Panic-recovery middleware for HTTP handlers and a panic-safe wrapper around
  background workers (scheduler, alert dispatcher, governance syncer, cluster
  health harvester, model scheduler).
- Helm chart (`deploy/helm/ch-ui`) and a `docker-compose.yml` quick-start, both
  documenting the single-instance constraint.
- `ch-ui backup` command — a consistent SQLite snapshot via `VACUUM INTO` (safe
  to run against a live, WAL-mode database), with an `APP_SECRET_KEY` reminder.
- Database schema-version tracking recorded on each migration run for upgrade
  observability.
- Release artifacts now ship a CycloneDX **SBOM** and **cosign**-signed checksums;
  Docker images are cosign-signed with SBOM + provenance attestations.
- Docker `HEALTHCHECK`.
- Continuous Integration workflow: gofmt check, `go vet`, `go test -race`,
  `govulncheck`, frontend typecheck, unit tests, and production build now run on
  every pull request and push to `main`.
- Dependabot configuration for Go modules, UI npm packages, and GitHub Actions.
- `SECURITY.md` vulnerability disclosure policy.
- Tests for license validation (valid/grace/expired/tampered/wrong-key) and the
  recovery/metrics middleware.
- This changelog.

### Fixed

- Kafka pipeline ingestion is now at-least-once: consumer offsets are committed
  only after the batch is durably written to the sink, instead of when the
  message is first read (previously a crash mid-batch silently dropped data).
- `ch-ui update` now verifies the download checksum **fail-closed**: it refuses to
  install if a checksum cannot be fetched or verified, instead of warning and
  continuing.
- WebSocket tunnel (`/connect`) upgrades no longer break when the metrics
  middleware is in the chain (the response-writer wrapper now preserves
  `http.Hijacker`).

### Changed

- Privacy policy now accurately lists every optional third-party egress path
  (your LLM provider, GitHub for updates/model sync, configured email/alert
  providers) instead of only OpenAI.
- Reproducible release builds: frontend build uses `bun install --frozen-lockfile`
  and the release/CI Go toolchain is pinned via `go-version-file: go.mod`.

### Removed

- Removed all remaining Langfuse references from documentation and README. The
  Langfuse integration is no longer part of CH-UI.

## [2.4.0]

- Query Insights (Pro): `system.query_log` analytics.
- Cluster Health (Pro): operations and database monitoring.
- Result filters and ClickHouse error parsing in the query results view.

[2.5.1]: https://github.com/caioricciuti/ch-ui/compare/v2.5.0...v2.5.1
[2.5.0]: https://github.com/caioricciuti/ch-ui/compare/v2.4.0...v2.5.0
[2.4.0]: https://github.com/caioricciuti/ch-ui/releases/tag/v2.4.0
