<script lang="ts">
  import type { SavedQuery, SavedQueryVersion } from '../../types/api'
  import { listSavedQueryVersions, getSavedQueryVersion, restoreSavedQueryVersion } from '../../api/savedQueries'
  import { formatDate, formatRelativeTime } from '../../utils/format'
  import { error as toastError, success as toastSuccess } from '../../stores/toast.svelte'
  import Sheet from '../common/Sheet.svelte'
  import Button from '../common/Button.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import Spinner from '../common/Spinner.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'

  /**
   * Version history of a saved query: the recorded versions, a read-only
   * preview of the selected one, and Restore (owner or admin).
   */
  interface Props {
    query: SavedQuery | null
    onclose: () => void
    onrestored: () => void
  }

  let { query, onclose, onrestored }: Props = $props()

  type Row = Record<string, unknown> & SavedQueryVersion & { author: string }

  let versions = $state<SavedQueryVersion[]>([])
  let loading = $state(false)
  let selected = $state<SavedQueryVersion | null>(null)
  let previewLoading = $state(false)
  let confirmOpen = $state(false)
  let restoring = $state(false)
  let loadedFor = $state<string | null>(null)

  $effect(() => {
    const q = query
    if (!q) {
      loadedFor = null
      return
    }
    if (q.id === loadedFor) return
    loadedFor = q.id
    void load(q.id)
  })

  async function load(id: string) {
    loading = true
    selected = null
    versions = []
    try {
      versions = await listSavedQueryVersions(id)
      if (versions.length > 0) await pick(versions[0])
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      loading = false
    }
  }

  async function pick(v: SavedQueryVersion) {
    if (!query) return
    previewLoading = true
    try {
      selected = await getSavedQueryVersion(query.id, v.version)
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      previewLoading = false
    }
  }

  async function restore() {
    if (!query || !selected) return
    restoring = true
    try {
      await restoreSavedQueryVersion(query.id, selected.version)
      toastSuccess(`Restored version ${selected.version}`)
      confirmOpen = false
      onrestored()
      await load(query.id)
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      restoring = false
    }
  }

  const rows = $derived<Row[]>(versions.map((v) => ({ ...v, author: v.created_by ?? '' })))
  const latest = $derived(versions[0]?.version ?? 0)

  const columns: DataColumn<Row>[] = [
    { key: 'version', label: 'Version', width: '80px', align: 'right' },
    { key: 'note', label: 'Note', truncate: true, format: (v) => String(v || '') },
    { key: 'author', label: 'By', width: '140px', truncate: true },
    { key: 'created_at', label: 'When', width: '120px', format: (v) => formatRelativeTime(v) },
  ]
</script>

<Sheet
  open={!!query}
  title="History"
  description={query ? `${query.name}. The latest 50 versions are kept.` : undefined}
  size="lg"
  onclose={onclose}
>
  {#if loading}
    <div class="flex items-center justify-center py-14"><Spinner /></div>
  {:else if versions.length === 0}
    <EmptyState
      size="compact"
      title="No history yet"
      description="Versions are recorded when the name, description, SQL, parameters or connection change."
    />
  {:else}
    <div class="space-y-4">
      <div class="max-h-64 overflow-auto rounded-lg border border-edge-subtle bg-surface">
        <DataTable
          framed={false}
          {columns}
          {rows}
          rowKey={(r) => r.id}
          onrowclick={(r) => void pick(r)}
        >
          {#snippet cell(row, col, value)}
            {#if col.key === 'version'}
              <span class={selected?.version === row.version ? 'font-semibold text-fg' : ''}>{value}</span>
            {:else if col.key === 'created_at'}
              <span title={formatDate(row.created_at)}>{value}</span>
            {:else if col.key === 'note'}
              <span class="text-fg-3">{value || (row.version === latest ? 'Current' : '')}</span>
            {:else}
              {value}
            {/if}
          {/snippet}
        </DataTable>
      </div>

      {#if previewLoading}
        <div class="flex items-center justify-center py-8"><Spinner /></div>
      {:else if selected?.snapshot}
        {@const snap = selected.snapshot}
        <div class="space-y-2">
          <div class="flex items-baseline gap-2 text-[13px]">
            <span class="font-medium text-fg">Version {selected.version}</span>
            <span class="truncate text-fg-3">{snap.name}</span>
          </div>
          {#if snap.description}
            <p class="text-[13px] text-fg-2">{snap.description}</p>
          {/if}
          <pre class="max-h-80 overflow-auto whitespace-pre rounded-md border border-edge-subtle bg-surface-2 p-3 font-mono text-xs leading-relaxed text-fg">{snap.query}</pre>
        </div>
      {/if}
    </div>
  {/if}
  {#snippet footer()}
    <Button size="sm" variant="ghost" onclick={onclose}>Close</Button>
    <Button
      size="sm"
      disabled={!query?.can_edit || !selected || selected.version === latest}
      title={!query?.can_edit ? 'Only the owner or an admin can restore' : undefined}
      onclick={() => (confirmOpen = true)}
    >
      {selected ? `Restore version ${selected.version}` : 'Restore'}
    </Button>
  {/snippet}
</Sheet>

<ConfirmDialog
  open={confirmOpen}
  title="Restore this version?"
  description={selected ? `The name, description, SQL and parameters go back to version ${selected.version}. The current state stays in the history.` : ''}
  confirmLabel="Restore"
  loading={restoring}
  onconfirm={() => void restore()}
  oncancel={() => (confirmOpen = false)}
/>
