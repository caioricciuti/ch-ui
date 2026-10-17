<script lang="ts">
  import { SOURCE_NODE_TYPES, type Pipeline } from '../../types/pipelines'
  import { formatDate, formatRelativeTime } from '../../utils/format'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import Input from '../common/Input.svelte'
  import PageHeader from '../common/PageHeader.svelte'
  import { docsURL } from '../../routes'
  import Spinner from '../common/Spinner.svelte'
  import { Plus, Trash2, Play, Square, Workflow, Search } from 'lucide-svelte'

  interface Props {
    pipelines: Pipeline[]
    loading: boolean
    /** Pipelines with a start or stop request in flight. */
    pendingIds: string[]
    onCreate: () => void
    onSelect: (id: string) => void
    onDelete: (id: string) => void
    onStart: (id: string) => void
    onStop: (id: string) => void
  }

  let { pipelines, loading, pendingIds, onCreate, onSelect, onDelete, onStart, onStop }: Props = $props()

  let confirmDelete = $state<Pipeline | null>(null)
  let search = $state('')

  type Row = Record<string, unknown> & Pipeline

  const filtered = $derived<Row[]>(
    (search.trim()
      ? pipelines.filter((p) => p.name.toLowerCase().includes(search.toLowerCase()))
      : pipelines) as Row[],
  )

  type Tone = 'neutral' | 'success' | 'warning' | 'danger'

  function statusTone(status: string): Tone {
    switch (status) {
      case 'running':
        return 'success'
      case 'error':
        return 'danger'
      case 'starting':
      case 'stopping':
        return 'warning'
      default:
        return 'neutral'
    }
  }

  function sourceLabel(p: Pipeline): string {
    if (!p.source_type) return ''
    const label = SOURCE_NODE_TYPES.find((s) => s.type === p.source_type)?.label ?? p.source_type
    return p.signal ? `${label} (${p.signal})` : label
  }

  /** The error the card showed: only while erroring or retrying. */
  function errorText(p: Pipeline): string {
    if (!p.last_error || (p.status !== 'error' && p.status !== 'running')) return ''
    return p.status === 'running' ? `Retrying: ${p.last_error}` : p.last_error
  }

  function ts(value: string | null): number {
    const n = value ? new Date(value).getTime() : NaN
    return Number.isNaN(n) ? 0 : n
  }

  const columns: DataColumn<Row>[] = [
    { key: 'name', label: 'Name', truncate: true },
    { key: 'status', label: 'Status', width: '110px' },
    { key: 'source_type', label: 'Source', width: '160px', format: (_v, row) => sourceLabel(row) },
    { key: 'last_error', label: 'Last error', truncate: true, sortable: false, format: (_v, row) => errorText(row) },
    { key: 'updated_at', label: 'Updated', width: '120px', format: (v) => formatRelativeTime(v), sortValue: (row) => ts(row.updated_at) },
  ]
</script>

<div class="flex h-full min-h-0 flex-col">
  <PageHeader title="Pipelines" subtitle="Stream data into ClickHouse from external sources" docs={docsURL('pipelines')}>
    {#snippet meta()}
      {#if !loading}
        <Badge tone="neutral">{pipelines.length} total</Badge>
      {/if}
    {/snippet}
    {#snippet actions()}
      <Button size="sm" onclick={onCreate}>
        <Plus size={14} /> New pipeline
      </Button>
    {/snippet}
  </PageHeader>

  {#if loading}
    <div class="flex flex-1 items-center justify-center py-12"><Spinner /></div>
  {:else if pipelines.length === 0}
    <EmptyState
      icon={Workflow}
      title="No pipelines yet"
      description="A pipeline connects a source (Kafka, HTTP, files) to a ClickHouse table and keeps rows flowing."
      primary={{ label: 'New pipeline', onclick: onCreate }}
    />
  {:else}
    <div class="flex h-10 shrink-0 items-center gap-2 border-b border-edge-subtle px-4">
      <div class="relative">
        <Search size={13} class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-4" />
        <Input size="sm" type="search" class="w-64 pl-8" placeholder="Search pipelines" bind:value={search} spellcheck={false} />
      </div>
      <span class="ml-auto text-xs text-fg-4">{filtered.length} {filtered.length === 1 ? 'pipeline' : 'pipelines'}</span>
    </div>
    <div class="min-h-0 flex-1">
      {#if filtered.length === 0}
        <EmptyState icon={Workflow} title="No pipelines match “{search}”" secondary={{ label: 'Clear search', onclick: () => (search = '') }} />
      {:else}
        <DataTable
          fill
          {columns}
          rows={filtered}
          rowKey={(r) => r.id}
          sort={{ key: 'updated_at', dir: 'desc' }}
          onrowclick={(r) => onSelect(r.id)}
        >
          {#snippet cell(row, col, value)}
            {#if col.key === 'name'}
              <div class="min-w-0">
                <div class="truncate font-medium text-fg">{row.name}</div>
                {#if row.description}
                  <div class="truncate text-xs text-fg-3">{row.description}</div>
                {/if}
              </div>
            {:else if col.key === 'status'}
              <Badge tone={statusTone(row.status)} dot={row.status === 'running'}>{row.status}</Badge>
            {:else if col.key === 'last_error' && value}
              <span class={row.status === 'error' ? 'text-danger' : 'text-warning'}>{value}</span>
            {:else if col.key === 'updated_at'}
              <span title={formatDate(row.updated_at)}>{value}</span>
            {:else}
              {value}
            {/if}
          {/snippet}
          {#snippet actions(row)}
            {#if row.status === 'running' || row.status === 'starting'}
              <Button icon size="xs" variant="ghost" title="Stop pipeline" aria-label="Stop pipeline" disabled={pendingIds.includes(row.id)} onclick={(e) => { e.stopPropagation(); onStop(row.id) }}>
                <Square size={13} />
              </Button>
            {:else}
              <Button icon size="xs" variant="ghost" title="Start pipeline" aria-label="Start pipeline" disabled={pendingIds.includes(row.id)} onclick={(e) => { e.stopPropagation(); onStart(row.id) }}>
                <Play size={13} />
              </Button>
            {/if}
            <Button icon size="xs" variant="ghost" class="hover:text-danger" title="Delete pipeline" aria-label="Delete pipeline" onclick={(e) => { e.stopPropagation(); confirmDelete = row }}>
              <Trash2 size={13} />
            </Button>
          {/snippet}
        </DataTable>
      {/if}
    </div>
  {/if}
</div>

<ConfirmDialog
  open={!!confirmDelete}
  title="Delete pipeline?"
  description="This will permanently delete the pipeline and all its configuration. This cannot be undone."
  confirmLabel="Delete"
  destructive={true}
  onconfirm={() => {
    if (confirmDelete) {
      onDelete(confirmDelete.id)
      confirmDelete = null
    }
  }}
  oncancel={() => { confirmDelete = null }}
/>
