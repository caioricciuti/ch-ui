# v2.12.0 release candidate

Prepared 2026-09-20 on `release/v2.12.0`, based on main including Dependabot
#179 (`628b21d`) and #170 (`a83a91f`). VERSION and CHANGELOG are prepared;
no release tag has been created. This document supersedes the initial review.

## Scope

- Dedicated, encrypted accounts per connection for schedules, models, pipeline
  sinks, governance, Cluster Health and telemetry monitors. Admin API and UI
  support verified save/rotation, session mode and explicit disabling.
- Existing installations keep session borrowing. An invalid or disabled
  configured account never falls back to a human session. The API never
  returns passwords; account updates and credential acquisition are audited.
- Schedule create/update/delete/manual-run routes now require writer access.
  Viewers retain read access. Tests cover unauthenticated, viewer, analyst
  and admin requests and shared jobs on another connection.
- The existing shared-workspace policy remains explicit: admin/analyst roles
  are instance-wide and can exercise configured account grants through jobs.
  Connection selection is not tenant isolation. The account UI and runbook
  explain that delegation and the grants administrators should assign.
- Account forms support native validation and Enter submission and reject
  duplicate saves. Manual schedules/monitors use the caller's account;
  manual model runs use the model worker's configuration.
- Fixed an existing Parquet import failure discovered by a real round-trip
  regression test: the reader must infer row types from the file schema.
- Patched frontend dependencies and transitive overrides, retained the Thrift
  update and integrated both reviewed Dependabot updates.

## Verification

| Check | Result |
| --- | --- |
| Full Go suite with race detector | 306 tests passed in 36 packages |
| Go vet and formatting | Passed |
| Go vulnerability scan, pinned CI version v1.7.0 | No reachable vulnerabilities or affected imported packages; one module-only finding in code not called |
| Frozen Bun installation | Passed, no lockfile changes |
| Frontend type checking | No errors; two pre-existing NodeConfigPanel initialization warnings |
| Frontend tests | 83 passed, including seven tests using the real DOMPurify sanitizer |
| Production frontend and Go binary builds | Passed |
| Bun dependency audit | Zero reported vulnerabilities |
| All six workers against ClickHouse 25.8.33.6 | Passed with no human sessions, actual password rotation and disabled execution |
| Chrome account UI against the direct connector and real ClickHouse | Passed: verification, Enter submit, rejected rotation, disable/reopen, worker switching, Escape, load failure and no browser exceptions; light/dark screenshots inspected |
| Upgrade/restart/backup restore | Passed with populated previous-schema connections, sessions, saved SQL and schedules; dedicated and disabled modes survive repeated opens and restore |
| API verification through real WebSocket gateway | Passed: accepted/rejected/empty passwords, cancellation forwarding, failed rotation preservation, wrong app secret and response/audit redaction |

The old-schema test reconstructs the previous layout by removing only the new
background_credentials table and restoring the previous schema-version value.
The migration file on main before this feature is unchanged from v2.11.1.
It is not a production database snapshot.

Normal Go tests use deterministic protocol fixtures. Setting
`CHUI_TEST_CLICKHOUSE_URL`, `CHUI_TEST_CLICKHOUSE_USER` and
`CHUI_TEST_CLICKHOUSE_PASSWORD` exercises the same worker entry points through
WebSocket and the connector's ClickHouse client against a disposable server.
See the [runbook](production-runbook.md#repeatable-worker-acceptance-test).
CI now runs both modes against a digest-pinned ClickHouse service. The frontend
job also runs `bun audit`; green candidate CI is the final merge gate.

Package selection honored the seven-day minimum release age. The lockfile
resolves Svelte 5.57.0, Vite 8.3.0, Vitest 5.0.0, DOMPurify 3.4.15 and devalue
5.9.1. jsdom 27.4.0 is test-only. No unresolved frontend advisories remained
at preparation time; later advisory changes can legitimately fail CI.

## Operations and rollout

1. Confirm candidate CI is green and merge the release PR.
2. Take a consistent `ch-ui backup`; preserve the app secret separately.
3. Verify VERSION/CHANGELOG are on the target commit. Create and push the
   annotated `v2.12.0` tag only when publishing the release is authorized.
4. Watch the release workflow through all binary builds, Alpine smoke,
   SBOM/checksums/signatures and both Docker architectures. Verify published
   versions, signatures and image tags before rollout.
5. Upgrade a canary instance first. Verify session-mode behavior, configure
   a least-privilege account and check actual job results after sign-out.

Existing work may finish after disabling: model runs hold credentials for the
run, while pipelines acquire them per batch. For immediate revocation, revoke
ClickHouse access and terminate active queries as appropriate.

Rollback to v2.11.1 must restore the pre-upgrade database/configuration with
workers stopped. Old binaries ignore dedicated/disabled settings and resume
session borrowing. Do not treat an additive schema as equivalent runtime
behavior on downgrade. These procedures are in the
[production runbook](production-runbook.md#background-accounts-v2120), and
[telemetry documentation](telemetry.md) now describes all credential modes.

## Boundaries and follow-ups

The browser smoke used a community instance; licensed worker execution was
validated directly, independently of the existing license gate. A live SSO
provider, a licensed/grace browser deployment and external MCP OAuth clients
were not exercised. Existing automated authentication/license tests passed.
Those integration checks remain useful canary checks; there was no new OAuth
or license-gate implementation in this candidate.

Existing follow-ups remain outside this release: safe POST reads during
license grace, route-level frontend code splitting (main chunk about 970 kB,
272 kB gzip), and the two intentional Svelte initialization warnings.
No production data or production configuration was used in acceptance tests.
