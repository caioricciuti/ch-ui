<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { errorText } from '../../../utils/errors'
  import { onMount } from 'svelte'
  import { ShieldQuestion } from 'lucide-svelte'
  import type { RbacEvidence, RbacExplainQuery, RbacExplanation, RbacSnapshot, RbacWhoCanResult, RbacWhoCanUser } from '../../../types/rbac'
  import { fetchEffectiveAccess, fetchRbacSnapshot, fetchWhoCan } from '../../../api/rbac'
  import { listDatabases, listTables } from '../../../api/query'
  import { effectBadge, objectName, queryFromSearch, searchWithQuery, verdictLabel, verdictTone } from './explain'
  import Select from '../../common/Select.svelte'
  import Button from '../../common/Button.svelte'
  import Badge from '../../common/Badge.svelte'
  import Spinner from '../../common/Spinner.svelte'
  import EmptyState from '../../common/EmptyState.svelte'
  import Combobox, { type ComboboxOption } from '../../common/Combobox.svelte'
  import DataTable, { type DataColumn } from '../../common/DataTable.svelte'

  /**
   * Governance > Effective access. With a user it explains what that user
   * can do on an object and why, step by step; with no user it lists who
   * can. The question lives in the URL so it can be linked.
   */
  type Row = Record<string, unknown>

  let snapshot = $state<RbacSnapshot | null>(null)
  let databases = $state<string[]>([])
  let tables = $state<string[]>([])

  let q = $state<RbacExplainQuery>({ user: '', database: '', table: '', column: '', privilege: 'SELECT' })
  let asked = $state<RbacExplainQuery | null>(null)
  let explanation = $state<RbacExplanation | null>(null)
  let whoCan = $state<RbacWhoCanResult | null>(null)
  let loading = $state(false)
  let error = $state('')

  onMount(() => {
    q = queryFromSearch(window.location.search)
    fetchRbacSnapshot().then((s) => (snapshot = s)).catch(() => (snapshot = null))
    listDatabases().then((d) => (databases = d)).catch(() => (databases = []))
    if (q.database) void loadTables(q.database)
    if (q.user) void run()
  })

  async function loadTables(db: string) {
    tables = []
    if (!db) return
    try {
      tables = await listTables(db)
    } catch {
      tables = []
    }
  }

  const userOptions = $derived<ComboboxOption[]>([
    { value: '', label: 'Anyone (who can)' },
    ...(snapshot?.users ?? []).map((u) => ({ value: u.name, label: u.name, hint: u.storage === 'users_xml' ? 'users.xml' : undefined })),
  ])
  const databaseOptions = $derived<ComboboxOption[]>([{ value: '', label: 'All databases (*)' }, ...databases.map((d) => ({ value: d, label: d }))])
  const tableOptions = $derived<ComboboxOption[]>([{ value: '', label: 'All tables (*)' }, ...tables.map((t) => ({ value: t, label: t }))])
  const privilegeOptions = $derived.by(() => {
    const names = (snapshot?.privileges ?? []).map((p) => p.name)
    if (!names.includes(q.privilege)) names.unshift(q.privilege)
    return names.map((n) => ({ value: n, label: n }))
  })

  function pickDatabase(db: string) {
    q = { ...q, database: db, table: '', column: '' }
    void loadTables(db)
  }

  function syncUrl() {
    const search = searchWithQuery(window.location.search, q)
    history.replaceState(null, '', `${window.location.pathname}${search ? `?${search}` : ''}`)
  }

  async function run() {
    syncUrl()
    loading = true
    error = ''
    const question = { ...q }
    try {
      if (question.user) {
        explanation = await fetchEffectiveAccess(question)
        whoCan = null
      } else {
        whoCan = await fetchWhoCan(question)
        explanation = null
      }
      asked = question
    } catch (err: unknown) {
      error = errorText(err)
    } finally {
      loading = false
    }
  }

  function explainUser(row: Row) {
    q = { ...q, user: String(row.user) }
    void run()
  }

  // ── Forward ───────────────────────────────────────────────────────

  const evidenceColumns: DataColumn<Row>[] = [
    { key: 'step', label: 'Step', align: 'right', width: '64px' },
    { key: 'source', label: 'Source path', truncate: true },
    { key: 'statement', label: 'Statement', mono: true, truncate: true },
    { key: 'effect', label: 'Effect', width: '120px', sortValue: (r) => String(r.effect) },
    { key: 'detail', label: 'Note', truncate: true },
  ]
  const evidenceRows = $derived<Row[]>((explanation?.evidence ?? []).map((e: RbacEvidence) => ({ ...e })))

  const notices = $derived.by(() => {
    const out: string[] = []
    const src = explanation ?? whoCan
    if (src?.incomplete) out.push(...(src.incomplete_reasons ?? []))
    if (explanation) out.push(...explanation.warnings)
    return out
  })

  const localEntities = $derived((explanation ?? whoCan)?.node.local_entities ?? [])
  const host = $derived((explanation ?? whoCan)?.node.host ?? '')

  // ── Reverse ───────────────────────────────────────────────────────

  const whoColumns: DataColumn<Row>[] = [
    { key: 'user', label: 'User' },
    { key: 'default', label: 'At login', width: '130px', sortValue: (r) => String(r.default) },
    { key: 'all_roles', label: 'After SET ROLE', width: '140px', sortValue: (r) => String(r.all_roles) },
    { key: 'via', label: 'Via', truncate: true },
    { key: 'rows', label: 'Rows', width: '110px' },
    { key: 'columns', label: 'Columns', truncate: true },
  ]
  const whoRows = $derived<Row[]>(
    (whoCan?.users ?? []).map((u: RbacWhoCanUser) => ({
      user: u.user,
      default: u.default.status,
      all_roles: u.all_roles.status,
      via: u.via.join(', '),
      rows: u.row_filtered ? 'Filtered' : '',
      columns: u.columns.join(', '),
      local: !!u.local,
    })),
  )
</script>

<div class="flex h-full min-h-0 flex-col">
  <form class="flex min-h-10 shrink-0 flex-wrap items-center gap-2 px-5 py-1.5" onsubmit={(e) => { e.preventDefault(); void run() }}>
    <div class="w-52"><Combobox id="explain-user" options={userOptions} value={q.user} placeholder="Anyone (who can)" onChange={(v) => (q = { ...q, user: v })} /></div>
    <div class="w-48"><Combobox id="explain-db" options={databaseOptions} value={q.database} onChange={pickDatabase} /></div>
    <div class="w-48"><Combobox id="explain-table" options={tableOptions} value={q.table} disabled={!q.database} onChange={(v) => (q = { ...q, table: v, column: '' })} /></div>
    <Select size="sm" class="w-48" options={privilegeOptions} value={q.privilege} onchange={(v) => (q = { ...q, privilege: v })} />
    <Button size="sm" type="submit" {loading}>Explain</Button>
    <div class="flex-1"></div>
    {#if host}<span class="text-xs text-fg-4" title="Access was read from this node only">Node {host}</span>{/if}
  </form>

  {#if notices.length > 0}
    <div class="mx-5 mb-2 rounded-md border border-warning/40 bg-warning-soft px-3 py-2 text-[13px] text-fg">
      {#each notices as n, i (i)}<p>{n}</p>{/each}
    </div>
  {/if}

  {#if explanation && asked}
    {@const v = explanation.verdict}
    <div class="flex shrink-0 flex-wrap items-center gap-2 px-5 pb-2 text-[13px]">
      <span class="font-medium text-fg">{asked.user}</span>
      <span class="font-mono text-xs text-fg-3">{asked.privilege} ON {objectName(asked)}</span>
      <Badge tone={verdictTone(v.default.status)} title={v.default.summary}>At login: {verdictLabel(v.default.status)}</Badge>
      <Badge tone={verdictTone(v.all_roles.status)} title={v.all_roles.summary}>After SET ROLE: {verdictLabel(v.all_roles.status)}</Badge>
      {#if localEntities.length > 0}
        <Badge tone="warning" title={`Stored on this node only: ${localEntities.join(', ')}`}>Local to this node</Badge>
      {/if}
      {#if explanation.verification.status === 'match'}
        <Badge tone="neutral" title={explanation.verification.detail ?? explanation.verification.statement}>Matches ClickHouse</Badge>
      {:else if explanation.verification.status === 'mismatch'}
        <Badge tone="warning" title={explanation.verification.statement}>ClickHouse disagrees</Badge>
      {:else}
        <Badge tone="neutral" title={explanation.verification.detail}>Not verified</Badge>
      {/if}
      <span class="min-w-0 truncate text-fg-3" title={v.default.summary}>{v.default.summary}</span>
    </div>
  {/if}

  <div class="min-h-0 flex-1 px-5 pb-4">
    {#if loading && !explanation && !whoCan}
      <div class="flex h-full items-center justify-center"><Spinner /></div>
    {:else if error}
      <div class="rounded-md bg-danger-soft px-3 py-2 text-xs text-danger">{error}</div>
    {:else if explanation}
      <div class="h-full overflow-hidden rounded-lg border border-edge-subtle bg-surface">
        <DataTable columns={evidenceColumns} rows={evidenceRows} fill rowKey={(r) => Number(r.step)} sort={{ key: 'step', dir: 'asc' }}
          emptyTitle="Nothing grants or limits this" emptyDescription="No grant, role, policy, profile or quota touches this privilege on this object.">
          {#snippet cell(row, col, value)}
            {#if col.key === 'effect'}
              {@const b = effectBadge(row.effect as RbacEvidence['effect'])}
              <Badge tone={b.tone}>{b.label}</Badge>
            {:else if col.key === 'source'}
              <span class="inline-flex min-w-0 items-center gap-1.5">
                <span class="truncate" title={value}>{value}</span>
                {#if row.mode === 'all_roles'}<Badge tone="neutral">After SET ROLE</Badge>{/if}
                {#if row.local}<Badge tone="warning">Local to this node</Badge>{/if}
              </span>
            {:else}
              {value}
            {/if}
          {/snippet}
        </DataTable>
      </div>
    {:else if whoCan && asked}
      <div class="h-full overflow-hidden rounded-lg border border-edge-subtle bg-surface">
        <DataTable columns={whoColumns} rows={whoRows} fill rowKey={(r) => String(r.user)} sort={{ key: 'user', dir: 'asc' }}
          emptyTitle={whoCan.incomplete ? 'No user visible to this login can' : 'Nobody can'}
          emptyDescription={`No user holds ${asked.privilege} on ${objectName(asked)}.`}
          onrowclick={explainUser}>
          {#snippet cell(row, col, value)}
            {#if col.key === 'default' || col.key === 'all_roles'}
              <Badge tone={verdictTone(value as RbacWhoCanUser['default']['status'])}>{verdictLabel(value as RbacWhoCanUser['default']['status'])}</Badge>
            {:else if col.key === 'rows'}
              {#if value}<Badge tone="info">{value}</Badge>{/if}
            {:else if col.key === 'user'}
              <span class="inline-flex items-center gap-1.5">
                <span class="font-medium text-fg">{value}</span>
                {#if row.local}<Badge tone="warning">Local to this node</Badge>{/if}
              </span>
            {:else}
              {value}
            {/if}
          {/snippet}
        </DataTable>
      </div>
    {:else}
      <EmptyState icon={ShieldQuestion} title="Ask who can do what"
        description="Pick a user to see what they can do on a database or table, and why. Leave the user blank to list everyone who holds the privilege." />
    {/if}
  </div>
</div>
