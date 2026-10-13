<script lang="ts">
  import type { PreviewResult } from '../../types/models'
  import * as api from '../../api/models'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import Button from '../common/Button.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import { AlertCircle } from 'lucide-svelte'

  interface Props {
    modelId: string
    /** The editor's current SQL, so unsaved edits preview too. */
    getSql: () => string
  }

  let { modelId, getSql }: Props = $props()

  let result = $state<PreviewResult | null>(null)
  let loading = $state(false)
  let requestError = $state('')

  type Row = Record<string, unknown>

  function cellText(v: unknown): string {
    if (v == null) return 'NULL'
    if (typeof v === 'object') return JSON.stringify(v)
    return String(v)
  }

  const columns = $derived<DataColumn<Row>[]>(
    (result?.meta ?? []).map((c) => ({ key: c.name, label: c.name, mono: true, truncate: true, format: cellText })),
  )

  async function preview() {
    const sql = getSql()
    if (!sql.trim()) {
      requestError = 'The model has no SQL to preview.'
      return
    }
    loading = true
    requestError = ''
    try {
      result = await api.previewModel(sql, modelId)
    } catch (e: unknown) {
      result = null
      requestError = (e instanceof Error ? e.message : '') || 'Failed to preview model'
    } finally {
      loading = false
    }
  }
</script>

<div class="flex h-full min-h-0 flex-col">
  <div class="flex h-10 shrink-0 items-center gap-2 border-b border-edge-subtle px-3 text-xs text-fg-3">
    <span class="flex-1">First 100 rows, read-only, with your credentials.</span>
    {#if result?.data}
      <span class="tabular-nums">{result.data.length} row{result.data.length === 1 ? '' : 's'}</span>
    {/if}
    <Button size="xs" variant="outline" onclick={preview} loading={loading}>Preview</Button>
  </div>
  {#if requestError || result?.error}
    <div class="m-3 flex shrink-0 items-start gap-2 rounded-md bg-danger-soft px-3 py-2">
      <AlertCircle size={14} class="mt-0.5 shrink-0 text-danger" />
      <pre class="flex-1 whitespace-pre-wrap break-all text-xs text-danger">{requestError || result?.error}</pre>
    </div>
  {/if}
  <div class="min-h-0 flex-1">
    {#if result && !result.error}
      <DataTable
        {columns}
        rows={result.data ?? []}
        fill
        emptyTitle="No rows"
        emptyDescription="The query ran and returned nothing."
      />
    {:else if !result && !requestError}
      <EmptyState size="compact" title="No preview yet" description="Preview runs the editor's SQL, saved or not, and shows what the model would hold." />
    {/if}
  </div>
</div>
