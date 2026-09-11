<script lang="ts">
  import { Plus, ScanSearch, Activity, Pencil, Trash2 } from 'lucide-svelte'
  import PageBody from '../common/PageBody.svelte'
  import SectionHeader from '../common/SectionHeader.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import SourceEditor from './SourceEditor.svelte'
  import DetectSourcesSheet from './DetectSourcesSheet.svelte'
  import { createSource, updateSource, deleteSource, detectSources, testSource } from '../../api/telemetry'
  import type { TelemetrySource, SourceInput, SourceTestResult } from '../../types/telemetry'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import { getSession } from '../../stores/session.svelte'
  import { formatRelativeTime, formatDate } from '../../utils/format'

  interface Props {
    sources: TelemetrySource[]
    loading: boolean
    onreload: () => Promise<void>
  }
  let { sources, loading, onreload }: Props = $props()

  const canWrite = $derived((getSession()?.role ?? 'viewer') !== 'viewer')

  type Row = TelemetrySource & Record<string, unknown> & { table_label: string; status: string }
  type Status = { ok: boolean; missing: string[]; error?: string; latest: string | null; rows: number; json: boolean } | 'pending'

  let tests = $state<Record<string, Status>>({})

  async function runTest(s: TelemetrySource) {
    tests = { ...tests, [s.id]: 'pending' }
    try {
      const r: SourceTestResult = await testSource(s.id)
      tests = { ...tests, [s.id]: { ok: r.ok, missing: r.missing ?? [], error: r.error, latest: r.latest, rows: r.row_count_1h, json: hasJsonAttributes(s, r.columns ?? {}) } }
    } catch (e: unknown) {
      tests = { ...tests, [s.id]: { ok: false, missing: [], error: e instanceof Error ? e.message : String(e), latest: null, rows: 0, json: false } }
    }
  }

  $effect(() => {
    for (const s of sources) if (tests[s.id] === undefined) void runTest(s)
  })

  /** The exporter's json mode stores attribute maps as JSON columns. */
  function hasJsonAttributes(s: TelemetrySource, cols: Record<string, string>): boolean {
    const names = s.kind === 'logs'
      ? [s.logs?.log_attributes, s.logs?.resource_attributes, s.logs?.scope_attributes]
      : s.kind === 'traces' ? [s.traces?.span_attributes, s.traces?.resource_attributes] : []
    return names.some((n) => n && typeof cols[n] === 'string' && cols[n].startsWith('JSON'))
  }

  function jsonAttrs(id: string): boolean {
    const t = tests[id]
    return !!t && t !== 'pending' && t.json
  }

  function tableLabel(s: TelemetrySource): string {
    if (s.kind === 'metrics' && s.tables) return Object.values(s.tables).filter(Boolean).map((t) => `${s.database}.${t}`).join(', ')
    return `${s.database}.${s.table}`
  }

  const rows = $derived<Row[]>(sources.map((s) => ({ ...s, table_label: tableLabel(s), status: statusLabel(tests[s.id]) })))

  function statusLabel(t: Status | undefined): string {
    if (!t || t === 'pending') return 'Checking'
    if (t.ok) return 'Verified'
    if (t.missing.length > 0) return 'Missing columns'
    return 'Unreachable'
  }

  function statusTone(label: string): 'neutral' | 'success' | 'warning' | 'danger' {
    switch (label) {
      case 'Verified': return 'success'
      case 'Missing columns': return 'warning'
      case 'Unreachable': return 'danger'
      default: return 'neutral'
    }
  }

  function statusTitle(s: TelemetrySource): string {
    const t = tests[s.id]
    if (!t || t === 'pending') return 'Running a check'
    if (t.ok) return `${t.rows} rows in the last hour${t.latest ? `, latest ${formatDate(t.latest)}` : ''}`
    if (t.missing.length > 0) return `Missing: ${t.missing.join(', ')}`
    return t.error ?? 'Could not reach the table'
  }

  const columns: DataColumn<Row>[] = [
    { key: 'kind', label: 'Kind', width: '90px' },
    { key: 'name', label: 'Name' },
    { key: 'table_label', label: 'Table', mono: true, truncate: true, width: '34%' },
    { key: 'status', label: 'Status', width: '150px', sortable: false },
    { key: 'enabled', label: 'Enabled', width: '90px', sortable: false },
    { key: 'updated_at', label: 'Updated', width: '120px', format: (v) => formatRelativeTime(v) },
  ]

  // ── Editor ─────────────────────────────────────────────────
  let editorOpen = $state(false)
  let editing = $state<TelemetrySource | null>(null)
  let saving = $state(false)

  async function save(input: SourceInput) {
    saving = true
    try {
      if (editing) {
        await updateSource(editing.id, input)
        toastSuccess('Source updated')
      } else {
        await createSource(input)
        toastSuccess('Source created')
      }
      editorOpen = false
      tests = {}
      await onreload()
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      saving = false
    }
  }

  // ── Detect ─────────────────────────────────────────────────
  let detectOpen = $state(false)
  let detecting = $state(false)
  let proposals = $state<TelemetrySource[]>([])
  let adding = $state(false)

  async function detect() {
    detectOpen = true
    detecting = true
    proposals = []
    try {
      proposals = await detectSources()
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      detecting = false
    }
  }

  async function addProposals(selected: TelemetrySource[]) {
    adding = true
    let added = 0
    try {
      for (const p of selected) {
        const input: SourceInput = {
          kind: p.kind, name: p.name, database: p.database, table: p.table, enabled: true,
          logs: p.logs, traces: p.traces, tables: p.tables, correlated_logs: '', correlated_traces: '',
        }
        await createSource(input)
        added++
      }
      toastSuccess(`${added} source${added === 1 ? '' : 's'} added`)
      detectOpen = false
      tests = {}
      await onreload()
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      adding = false
    }
  }

  // ── Delete ─────────────────────────────────────────────────
  let deleting = $state<TelemetrySource | null>(null)
  let deleteLoading = $state(false)

  async function confirmDelete() {
    if (!deleting) return
    deleteLoading = true
    try {
      await deleteSource(deleting.id)
      toastSuccess('Source removed')
      deleting = null
      await onreload()
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      deleteLoading = false
    }
  }
</script>

<PageBody width="md">
  <div class="space-y-5">
    <SectionHeader
      title="Sources"
      description="Where the OpenTelemetry collector writes logs, traces and metrics on this connection, and which columns play which role."
    >
      {#snippet actions()}
        {#if canWrite}
          <Button size="sm" variant="outline" onclick={detect}><ScanSearch size={14} /> Detect sources</Button>
          <Button size="sm" onclick={() => { editing = null; editorOpen = true }}><Plus size={14} /> New source</Button>
        {/if}
      {/snippet}
    </SectionHeader>

    {#if !loading && sources.length === 0}
      <EmptyState
        icon={ScanSearch}
        title="No sources yet"
        description="Send data with the OpenTelemetry collector's ClickHouse exporter, then let CH-UI detect the otel_logs, otel_traces and otel_metrics_* tables, or map your own tables by hand."
        primary={canWrite ? { label: 'Detect sources', onclick: detect } : undefined}
        secondary={canWrite ? { label: 'New source', onclick: () => { editing = null; editorOpen = true } } : undefined}
      >
        <a class="inline-flex h-7 items-center gap-1.5 rounded-md border border-edge px-2.5 text-xs font-medium text-fg-2 transition-colors hover:border-edge-strong hover:bg-hover hover:text-fg" href="https://github.com/caioricciuti/ch-ui/blob/main/docs/telemetry.md" target="_blank" rel="noopener noreferrer">Read the setup guide</a>
      </EmptyState>
    {:else}
      <DataTable {columns} {rows} rowKey={(r) => r.id} sort={{ key: 'kind', dir: 'asc' }} emptyTitle="No sources"
        onrowclick={canWrite ? (row) => { editing = row; editorOpen = true } : undefined}>
        {#snippet cell(row, col, value)}
          {#if col.key === 'kind'}
            <Badge tone={row.kind === 'logs' ? 'brand' : row.kind === 'traces' ? 'info' : 'success'}>{row.kind}</Badge>
          {:else if col.key === 'name'}
            <span class="font-medium text-fg">{row.name}</span>
          {:else if col.key === 'table_label'}
            <span class="inline-flex min-w-0 items-center gap-2">
              <span class="truncate font-mono text-xs text-fg-2" title={row.table_label}>{row.table_label}</span>
              {#if jsonAttrs(row.id)}<Badge tone="info" title="Attributes are stored as JSON columns (exporter json mode)">JSON attrs</Badge>{/if}
            </span>
          {:else if col.key === 'status'}
            <Badge tone={statusTone(row.status)} title={statusTitle(row)}>{row.status}</Badge>
          {:else if col.key === 'enabled'}
            <Badge dot tone={row.enabled ? 'success' : 'neutral'}>{row.enabled ? 'On' : 'Off'}</Badge>
          {:else}
            {value}
          {/if}
        {/snippet}
        {#snippet actions(row)}
          <Button icon variant="ghost" size="xs" aria-label="Test source" title="Test" onclick={() => runTest(row)}><Activity size={13} /></Button>
          {#if canWrite}
            <Button icon variant="ghost" size="xs" aria-label="Edit source" title="Edit" onclick={() => { editing = row; editorOpen = true }}><Pencil size={13} /></Button>
            <Button icon variant="ghost" size="xs" aria-label="Delete source" title="Delete" onclick={() => (deleting = row)}><Trash2 size={13} /></Button>
          {/if}
        {/snippet}
      </DataTable>
    {/if}
  </div>
</PageBody>

<SourceEditor open={editorOpen} source={editing} others={sources.filter((s) => s.id !== editing?.id)} {saving} onsave={save} onclose={() => (editorOpen = false)} />
<DetectSourcesSheet open={detectOpen} loading={detecting} {proposals} {adding} onadd={addProposals} onclose={() => (detectOpen = false)} />
<ConfirmDialog
  open={!!deleting}
  title="Remove source?"
  description={deleting ? `"${deleting.name}" stops appearing in Telemetry. The ClickHouse table is not touched.` : ''}
  confirmLabel="Remove"
  destructive
  loading={deleteLoading}
  onconfirm={confirmDelete}
  oncancel={() => (deleting = null)}
/>
