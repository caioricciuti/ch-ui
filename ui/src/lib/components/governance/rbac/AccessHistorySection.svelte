<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { onMount } from 'svelte'
  import { History, RefreshCw } from 'lucide-svelte'
  import type { RbacHistoryRow } from '../../../types/rbac'
  import { fetchRbacHistory, takeRbacSnapshot } from '../../../api/rbac'
  import { isProActive } from '../../../stores/license.svelte'
  import { success as toastSuccess, error as toastError } from '../../../stores/toast.svelte'
  import { formatDate } from '../../../utils/format'
  import { driftLabel, driftTone, sourceLabel, sourceTone, summaryText } from './history'
  import Select from '../../common/Select.svelte'
  import Button from '../../common/Button.svelte'
  import Badge from '../../common/Badge.svelte'
  import Spinner from '../../common/Spinner.svelte'
  import EmptyState from '../../common/EmptyState.svelte'
  import DataTable, { type DataColumn } from '../../common/DataTable.svelte'
  import HistorySheet from './HistorySheet.svelte'

  /**
   * Governance > Access history. Every distinct access control state seen
   * on this connection, newest first, with where it came from and whether
   * it was changed outside CH-UI. A row opens its diff and the revert.
   */
  type Row = Record<string, unknown>

  let rows = $state<RbacHistoryRow[]>([])
  let nextCursor = $state('')
  let loading = $state(true)
  let loadingMore = $state(false)
  let loadError = $state('')
  let filter = $state<'all' | 'drift'>('all')
  let taking = $state(false)
  let selected = $state<RbacHistoryRow | null>(null)
  let sheetOpen = $state(false)

  const PAGE = 200

  onMount(() => void load())

  async function load() {
    loading = true
    loadError = ''
    try {
      const page = await fetchRbacHistory({ driftOnly: filter === 'drift', limit: PAGE })
      rows = page.snapshots
      nextCursor = page.next_cursor
    } catch (err: unknown) {
      loadError = err instanceof Error ? err.message : String(err)
    } finally {
      loading = false
    }
  }

  async function loadMore() {
    if (!nextCursor) return
    loadingMore = true
    try {
      const page = await fetchRbacHistory({ driftOnly: filter === 'drift', limit: PAGE, cursor: nextCursor })
      rows = [...rows, ...page.snapshots]
      nextCursor = page.next_cursor
    } catch (err: unknown) {
      toastError(err instanceof Error ? err.message : String(err))
    } finally {
      loadingMore = false
    }
  }

  async function takeSnapshot() {
    taking = true
    try {
      const res = await takeRbacSnapshot()
      toastSuccess(res.created ? 'Snapshot stored' : 'No change since the last snapshot')
      await load()
    } catch (err: unknown) {
      toastError(err instanceof Error ? err.message : String(err))
    } finally {
      taking = false
    }
  }

  function setFilter(v: string) {
    filter = v === 'drift' ? 'drift' : 'all'
    void load()
  }

  function open(row: Row) {
    selected = rows.find((r) => r.id === row.id) ?? null
    sheetOpen = selected !== null
  }

  const tableRows = $derived<Row[]>(
    rows.map((r) => ({
      id: r.id,
      time: r.first_seen_at,
      source: r.source,
      actor: r.actor || r.ch_user,
      ch_user: r.ch_user,
      summary: summaryText(r),
      drift: r.drift,
      seen: r.seen_count,
    })),
  )

  const columns: DataColumn<Row>[] = [
    { key: 'time', label: 'Time', width: '190px', format: (v) => formatDate(v) },
    { key: 'source', label: 'Source', width: '150px' },
    { key: 'actor', label: 'Actor', width: '220px', truncate: true },
    { key: 'summary', label: 'Change summary', truncate: true, sortable: false },
    { key: 'drift', label: 'Drift', width: '140px' },
  ]

  const filterOptions = [
    { value: 'all', label: 'All changes' },
    { value: 'drift', label: 'Drift only' },
  ]
</script>

<div class="flex h-full min-h-0 flex-col">
  <div class="flex h-10 shrink-0 items-center gap-2 px-5">
    <Select size="sm" class="w-40" options={filterOptions} value={filter} onchange={setFilter} />
    <div class="flex-1"></div>
    <span class="text-xs tabular-nums text-fg-4">{rows.length}{nextCursor ? '+' : ''} rows</span>
    {#if nextCursor}
      <Button size="sm" variant="ghost" loading={loadingMore} onclick={() => void loadMore()}>Load older</Button>
    {/if}
    <Button icon variant="ghost" size="sm" aria-label="Refresh" title="Refresh" onclick={() => void load()}>
      <RefreshCw size={14} />
    </Button>
    <Button size="sm" variant="outline" loading={taking} disabled={!isProActive()} title={isProActive() ? 'Store the current state now' : 'Needs an active Pro license'} onclick={() => void takeSnapshot()}>
      Take snapshot
    </Button>
  </div>

  <div class="min-h-0 flex-1 px-5 pb-4">
    {#if loading && rows.length === 0}
      <div class="flex h-full items-center justify-center"><Spinner /></div>
    {:else if loadError}
      <div class="rounded-md bg-danger-soft px-3 py-2 text-xs text-danger">{loadError}</div>
    {:else if rows.length === 0 && filter === 'all'}
      <EmptyState
        icon={History}
        title="No access history yet"
        description="History starts now. A snapshot is stored after every apply or revert in Access control, when someone opens Access control and the state differs from the last snapshot, on each governance sync when background sync is enabled, and when you take one here. Changes made outside CH-UI show up as drift."
        primary={isProActive() ? { label: 'Take snapshot', onclick: () => void takeSnapshot() } : undefined}
      />
    {:else}
      <div class="h-full overflow-hidden rounded-lg border border-edge-subtle bg-surface">
        <DataTable
          {columns}
          rows={tableRows}
          fill
          rowKey={(r) => String(r.id)}
          sort={{ key: 'time', dir: 'desc' }}
          emptyTitle="No drift recorded"
          emptyDescription="Every recorded change so far was made through CH-UI."
          onrowclick={open}
        >
          {#snippet cell(row, col, value)}
            {#if col.key === 'source'}
              <Badge tone={sourceTone(row.source as RbacHistoryRow['source'])}>{sourceLabel(row.source as RbacHistoryRow['source'])}</Badge>
            {:else if col.key === 'drift'}
              {#if row.drift === 'none'}
                <span class="text-fg-4">-</span>
              {:else}
                <Badge tone={driftTone(row.drift as RbacHistoryRow['drift'])}>{driftLabel(row.drift as RbacHistoryRow['drift'])}</Badge>
              {/if}
            {:else if col.key === 'actor'}
              <span class="text-fg">{value}</span>
              {#if row.ch_user && row.ch_user !== row.actor}<span class="ml-1 text-xs text-fg-4">as {row.ch_user}</span>{/if}
            {:else if col.key === 'summary'}
              <span class="text-fg-2">{value}</span>
              {#if Number(row.seen) > 1}<span class="ml-1 text-xs text-fg-4">seen {row.seen} times</span>{/if}
            {:else}
              {value}
            {/if}
          {/snippet}
        </DataTable>
      </div>
    {/if}
  </div>
</div>

{#key selected?.id}
  <HistorySheet open={sheetOpen} row={selected} history={rows} onclose={() => (sheetOpen = false)} />
{/key}
