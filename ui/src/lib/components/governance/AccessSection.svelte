<script lang="ts">
  import { onMount } from 'svelte'
  import { Eye, Search } from 'lucide-svelte'
  import { formatDate } from '../../utils/format'
  import { error as toastError } from '../../stores/toast.svelte'
  import {
    fetchAccessUsers,
    fetchAccessRoles,
    fetchAccessMatrix,
    fetchOverPermissions,
  } from '../../api/governance'
  import type { ChUser, ChRole, AccessMatrixEntry, OverPermission } from '../../types/governance'
  import Tabs from '../common/Tabs.svelte'
  import Input from '../common/Input.svelte'
  import Select from '../common/Select.svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Sheet from '../common/Sheet.svelte'
  import Spinner from '../common/Spinner.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'

  /**
   * Governance › Access. Three flat tables that fill the viewport: every
   * risky grant across users, the ClickHouse users, and the materialized
   * grants matrix. Details for one risky grant open in a sheet.
   */
  type Row = Record<string, unknown>
  const asRows = <T,>(rows: T[]): Row[] => rows as unknown as Row[]

  type Severity = 'critical' | 'warn' | 'info'
  type BadgeTone = 'neutral' | 'success' | 'warning' | 'danger' | 'info' | 'brand'
  type View = 'risky' | 'users' | 'matrix'

  let loading = $state(true)
  let users = $state<ChUser[]>([])
  let roles = $state<ChRole[]>([])
  let accessMatrix = $state<AccessMatrixEntry[]>([])
  let overPermissions = $state<OverPermission[]>([])

  let view = $state<View>('risky')
  let search = $state('')
  let severityFilter = $state<'' | Severity>('')
  let userFilter = $state('')

  let detailOpen = $state(false)
  let selected = $state<OverPermission | null>(null)

  onMount(() => {
    void load()
  })

  async function load() {
    loading = true
    try {
      const [usersRes, rolesRes, matrixRes, overPermsRes] = await Promise.all([
        fetchAccessUsers(),
        fetchAccessRoles(),
        fetchAccessMatrix(),
        fetchOverPermissions(),
      ])
      users = usersRes?.users ?? []
      roles = rolesRes?.roles ?? []
      accessMatrix = matrixRes?.matrix ?? []
      overPermissions = overPermsRes?.over_permissions ?? []
    } catch (err: any) {
      toastError('Failed to load access data: ' + err.message)
    } finally {
      loading = false
    }
  }

  // ── Helpers ─────────────────────────────────────────────────────────────

  function severityOf(op: OverPermission): Severity {
    if (!op.last_query_time) return 'critical'
    const days = op.days_since_query ?? 0
    if (days >= 90) return 'critical'
    if (days >= 30) return 'warn'
    return 'info'
  }

  function severityTone(severity: string | null | undefined): BadgeTone {
    if (severity === 'critical' || severity === 'error') return 'danger'
    if (severity === 'warn') return 'warning'
    if (severity === 'info') return 'info'
    return 'neutral'
  }

  function severityRank(s: Severity): number {
    return s === 'critical' ? 3 : s === 'warn' ? 2 : 1
  }

  function safeLower(v: string | null | undefined): string {
    return (v ?? '').toLowerCase()
  }

  function formatDefaultRoles(raw: string | null | undefined): string {
    if (!raw) return '-'
    if (raw === 'ALL') return 'ALL'
    const trimmed = raw.trim()
    if (!trimmed) return '-'
    try {
      if (trimmed.startsWith('[') && trimmed.endsWith(']')) {
        const parsed = JSON.parse(trimmed)
        if (Array.isArray(parsed)) return parsed.filter(Boolean).join(', ') || '-'
      }
    } catch {
      // fall through
    }
    return trimmed
  }

  function lastActivity(op: OverPermission): string {
    if (!op.last_query_time) return 'never'
    const days = op.days_since_query ?? 0
    return days === 0 ? 'today' : `${days} d ago`
  }

  // ── Derived rows ────────────────────────────────────────────────────────

  // One row per risky grant, with the derived severity attached so the table
  // can sort and filter on it.
  type RiskyRow = OverPermission & { severity: Severity; severity_rank: number; database_label: string; table_label: string }

  const riskyRows = $derived.by<RiskyRow[]>(() =>
    overPermissions.map((op) => {
      const severity = severityOf(op)
      return {
        ...op,
        severity,
        severity_rank: severityRank(severity),
        database_label: op.database_name || '*',
        table_label: op.table_name || '*',
      }
    }),
  )

  const userOptions = $derived.by(() => {
    const names = Array.from(new Set(riskyRows.map((r) => r.user_name || '(unknown user)'))).sort((a, b) => a.localeCompare(b))
    return [{ value: '', label: 'All users' }, ...names.map((n) => ({ value: n, label: n }))]
  })

  const severityOptions = [
    { value: '', label: 'All severities' },
    { value: 'critical', label: 'Critical' },
    { value: 'warn', label: 'Warn' },
    { value: 'info', label: 'Info' },
  ]

  const filteredRisky = $derived.by(() => {
    const term = search.trim().toLowerCase()
    return riskyRows.filter((r) => {
      if (severityFilter && r.severity !== severityFilter) return false
      if (userFilter && (r.user_name || '(unknown user)') !== userFilter) return false
      if (!term) return true
      return (
        safeLower(r.user_name).includes(term) ||
        safeLower(r.role_name).includes(term) ||
        safeLower(r.privilege).includes(term) ||
        r.database_label.toLowerCase().includes(term) ||
        r.table_label.toLowerCase().includes(term) ||
        safeLower(r.reason).includes(term)
      )
    })
  })

  const filteredUsers = $derived.by(() => {
    const term = search.trim().toLowerCase()
    if (!term) return users
    return users.filter(
      (u) =>
        safeLower(u.name).includes(term) ||
        safeLower(u.auth_type).includes(term) ||
        safeLower(u.host_ip).includes(term) ||
        safeLower(u.default_roles).includes(term),
    )
  })

  const filteredMatrix = $derived.by(() => {
    const term = search.trim().toLowerCase()
    if (!term) return accessMatrix
    return accessMatrix.filter(
      (e) =>
        safeLower(e.user_name).includes(term) ||
        safeLower(e.role_name).includes(term) ||
        safeLower(e.database_name).includes(term) ||
        safeLower(e.table_name).includes(term) ||
        safeLower(e.privilege).includes(term),
    )
  })

  const criticalCount = $derived(riskyRows.filter((r) => r.severity === 'critical').length)

  const viewItems = $derived([
    { id: 'risky', label: 'Risky grants', count: riskyRows.length },
    { id: 'users', label: 'Users', count: users.length },
    { id: 'matrix', label: 'Grants matrix', count: accessMatrix.length },
  ])

  const visibleCount = $derived(
    view === 'risky' ? filteredRisky.length : view === 'users' ? filteredUsers.length : filteredMatrix.length,
  )

  const searchPlaceholder = $derived(
    view === 'risky' ? 'Search user, privilege, database, reason' : view === 'users' ? 'Search users' : 'Search user, database, table, privilege',
  )

  // ── Columns ─────────────────────────────────────────────────────────────

  const riskyColumns: DataColumn<Row>[] = [
    { key: 'user_name', label: 'User', width: '14%' },
    { key: 'role_name', label: 'Role', format: (v) => (v ? String(v) : '-') },
    { key: 'privilege', label: 'Privilege', mono: true },
    { key: 'database_label', label: 'Database', mono: true },
    { key: 'table_label', label: 'Table', mono: true },
    { key: 'reason', label: 'Reason', truncate: true, width: '26%' },
    { key: 'last_query_time', label: 'Last activity', align: 'right', sortValue: (r) => Number(r.days_since_query ?? Number.MAX_SAFE_INTEGER) },
    { key: 'severity', label: 'Severity', align: 'right', width: '96px', sortValue: (r) => Number(r.severity_rank) },
  ]

  const userColumns: DataColumn<Row>[] = [
    { key: 'name', label: 'Name' },
    { key: 'auth_type', label: 'Auth type', format: (v) => (v ? String(v) : '-') },
    { key: 'host_ip', label: 'Host', mono: true, format: (v) => (v ? String(v) : '-') },
    { key: 'default_roles', label: 'Default roles', format: (v) => formatDefaultRoles(v as string | null | undefined) },
    { key: 'first_seen', label: 'First seen', align: 'right', format: (v) => formatDate(v) },
  ]

  const matrixColumns: DataColumn<Row>[] = [
    { key: 'user_name', label: 'User' },
    { key: 'role_name', label: 'Role', format: (v) => (v ? String(v) : '-') },
    { key: 'database_name', label: 'Database', mono: true, format: (v) => (v ? String(v) : '*') },
    { key: 'table_name', label: 'Table', mono: true, format: (v) => (v ? String(v) : '*') },
    { key: 'privilege', label: 'Privilege', mono: true },
    { key: 'is_direct_grant', label: 'Grant', align: 'right', format: (v) => (v ? 'Direct' : 'Inherited') },
  ]

  function openDetails(row: Row) {
    selected = row as unknown as OverPermission
    detailOpen = true
  }

  function closeDetails() {
    detailOpen = false
    selected = null
  }
</script>

<div class="flex h-full min-h-0 flex-col">
  <div class="flex h-10 shrink-0 items-center gap-2 px-5">
    <Tabs variant="segmented" size="sm" items={viewItems} value={view} onchange={(id) => (view = id as View)} />
    {#if view === 'risky' && criticalCount > 0}
      <Badge tone="danger">{criticalCount} critical</Badge>
    {/if}
    <div class="flex-1"></div>
    {#if view === 'risky'}
      <Select size="sm" class="w-40" options={userOptions} bind:value={userFilter} />
      <Select size="sm" class="w-36" options={severityOptions} value={severityFilter} onchange={(v) => (severityFilter = v as '' | Severity)} />
    {/if}
    <div class="relative">
      <Search size={13} class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-4" />
      <Input size="sm" type="search" class="w-64 pl-8" placeholder={searchPlaceholder} bind:value={search} spellcheck={false} />
    </div>
    <span class="text-xs tabular-nums text-fg-4">{visibleCount} rows</span>
  </div>

  <div class="min-h-0 flex-1 px-5 pb-4">
    {#if loading}
      <div class="flex h-full items-center justify-center"><Spinner /></div>
    {:else}
      <div class="h-full overflow-hidden rounded-lg border border-edge-subtle bg-surface">
        {#if view === 'risky'}
          <DataTable
            columns={riskyColumns}
            rows={asRows(filteredRisky)}
            fill
            sort={{ key: 'severity', dir: 'desc' }}
            emptyTitle={overPermissions.length === 0 ? 'No risky grants detected' : 'Nothing matches the current filters'}
            emptyDescription={overPermissions.length === 0 ? 'Every grant has matching query activity in the last 30 days.' : undefined}
            onrowclick={openDetails}
          >
            {#snippet cell(row, col, value)}
              {#if col.key === 'severity'}
                <Badge tone={severityTone(value)}>{value}</Badge>
              {:else if col.key === 'user_name'}
                <span class="font-medium text-fg">{value || '(unknown user)'}</span>
              {:else if col.key === 'last_query_time'}
                <span title={row.last_query_time ? formatDate(row.last_query_time) : 'No query usage found'}>{lastActivity(row as unknown as OverPermission)}</span>
              {:else}
                {value}
              {/if}
            {/snippet}
            {#snippet actions(row)}
              <Button icon variant="ghost" size="xs" aria-label="Grant details" title="Details" onclick={() => openDetails(row)}>
                <Eye size={13} />
              </Button>
            {/snippet}
          </DataTable>
        {:else if view === 'users'}
          <DataTable
            columns={userColumns}
            rows={asRows(filteredUsers)}
            fill
            rowKey={(r) => String(r.id)}
            sort={{ key: 'name', dir: 'asc' }}
            emptyTitle={users.length === 0 ? 'No users found' : 'No user matches the search'}
            emptyDescription={users.length === 0 ? 'Run an access sync to collect users from system.users.' : undefined}
          >
            {#snippet cell(row, col, value)}
              {#if col.key === 'name'}<span class="font-medium text-fg">{value}</span>{:else}{value}{/if}
            {/snippet}
          </DataTable>
        {:else}
          <DataTable
            columns={matrixColumns}
            rows={asRows(filteredMatrix)}
            fill
            rowKey={(r) => String(r.id)}
            sort={{ key: 'user_name', dir: 'asc' }}
            emptyTitle={accessMatrix.length === 0 ? 'No access grants found' : 'No grant matches the search'}
            emptyDescription={accessMatrix.length === 0 ? 'Run an access sync to materialize grants from system.grants.' : undefined}
          >
            {#snippet cell(row, col, value)}
              {#if col.key === 'privilege'}
                <Badge tone="brand">{value}</Badge>
              {:else if col.key === 'user_name'}
                <span class="font-medium text-fg">{value}</span>
              {:else}
                {value}
              {/if}
            {/snippet}
          </DataTable>
        {/if}
      </div>
    {/if}
  </div>
</div>

<Sheet
  open={detailOpen}
  title={selected ? `${selected.user_name || '(unknown user)'} · ${selected.privilege}` : 'Grant details'}
  description={selected ? `${selected.database_name || '*'}.${selected.table_name || '*'}` : undefined}
  size="md"
  onclose={closeDetails}
>
  {#if selected}
    {@const severity = severityOf(selected)}
    <div class="space-y-5">
      <div class="flex items-center gap-2">
        <Badge tone={severityTone(severity)}>{severity}</Badge>
        <span class="text-xs text-fg-3">
          {severity === 'critical'
            ? 'Never used, or unused for 90 days or more.'
            : severity === 'warn'
              ? 'Unused for 30 days or more.'
              : 'Used recently, but broader than the activity suggests.'}
        </span>
      </div>

      <dl class="divide-y divide-edge-subtle rounded-lg border border-edge-subtle bg-surface">
        {#each [
          ['User', selected.user_name || '(unknown user)'],
          ['Role', selected.role_name || 'Direct grant'],
          ['Privilege', selected.privilege],
          ['Database', selected.database_name || '* (all databases)'],
          ['Table', selected.table_name || '* (all tables)'],
          ['Last query', selected.last_query_time ? `${formatDate(selected.last_query_time)} (${selected.days_since_query ?? 0} days ago)` : 'No query usage found'],
        ] as [label, value]}
          <div class="grid grid-cols-[120px_1fr] gap-3 px-4 py-2.5 text-[13px]">
            <dt class="text-fg-3">{label}</dt>
            <dd class="min-w-0 break-words font-mono text-xs leading-relaxed text-fg">{value}</dd>
          </div>
        {/each}
      </dl>

      <div>
        <p class="mb-1 text-xs font-medium text-fg-3">Why it is flagged</p>
        <p class="text-[13px] leading-relaxed text-fg-2">{selected.reason}</p>
      </div>

      {#if roles.length > 0}
        <div>
          <p class="mb-1.5 text-xs font-medium text-fg-3">Roles on this cluster</p>
          <div class="flex flex-wrap gap-1.5">
            {#each roles as role (role.id)}
              <Badge tone={role.name === selected.role_name ? 'brand' : 'neutral'}>{role.name}</Badge>
            {/each}
          </div>
        </div>
      {/if}
    </div>
  {/if}
  {#snippet footer()}
    <Button size="sm" variant="outline" onclick={closeDetails}>Close</Button>
  {/snippet}
</Sheet>
