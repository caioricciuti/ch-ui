<script lang="ts">
  import { onMount, untrack } from 'svelte'
  import { RefreshCw, AlertTriangle, ClipboardPaste } from 'lucide-svelte'
  import { previewTransforms } from '../../../api/pipelines'
  import { ApiError } from '../../../api/client'
  import { PREVIEW_MAX_RECORDS, type PreviewNode, type PreviewResponse, type TransformNodeType } from '../../../types/transforms'
  import { buildRows, parseSampleRecords, stepInput, valueText, type PreviewRow } from './preview'
  import Badge from '../../common/Badge.svelte'
  import Button from '../../common/Button.svelte'
  import Spinner from '../../common/Spinner.svelte'
  import EmptyState from '../../common/EmptyState.svelte'
  import { hintClass, monoInputClass } from './ui'
  import { pastedSample } from './sample.svelte'

  interface Props {
    pipelineId: string
    nodeId: string
    nodeType: TransformNodeType
    /** Transforms from the source up to and including this node, with unsaved configs. */
    chain: PreviewNode[]
    /** Node labels by id, to say which node a Pro error is about. */
    labels?: Record<string, string>
    /** Set when the node is not reached from a source; live samples are then unavailable. */
    chainNote?: string
    /** Problems in this chain's configs; the preview waits until they are fixed. */
    blocked?: string
  }

  let { pipelineId, nodeId, nodeType, chain, labels = {}, chainNote = '', blocked = '' }: Props = $props()

  let loading = $state(false)
  let error = $state('')
  let sampleError = $state('')
  let result = $state<PreviewResponse | null>(null)
  let showUnchanged = $state(false)
  let seq = 0

  const stepIndex = $derived(result ? result.steps.findIndex((s) => s.node_id === nodeId) : -1)
  const step = $derived(result && stepIndex >= 0 ? result.steps[stepIndex] : null)
  const input = $derived(result && stepIndex >= 0 ? stepInput(result, stepIndex) : [])
  const rows = $derived<PreviewRow[]>(step ? buildRows(input, step, nodeType) : [])
  const droppedEarlier = $derived(result ? result.input.length - input.length : 0)
  const keptCount = $derived(rows.filter((r) => !r.dropped).length)
  const droppedCount = $derived(rows.length - keptCount)
  const errors = $derived(step?.errors ?? [])
  const chainKey = $derived(JSON.stringify(chain))

  async function run() {
    if (blocked) return
    let records: Record<string, unknown>[] | undefined
    if (pastedSample.active) {
      const parsed = parseSampleRecords(pastedSample.text)
      sampleError = parsed.error
      if (parsed.error) return
      records = parsed.records
    }
    const my = ++seq
    loading = true
    error = ''
    try {
      const res = await previewTransforms(pipelineId, { nodes: chain, records })
      if (my !== seq) return
      result = res
      if (!records) pastedSample.noLive = false
    } catch (e: unknown) {
      if (my !== seq) return
      result = null
      error = e instanceof Error ? e.message : String(e)
      // Pro errors (402) name the node only by id; say which one when it is an earlier step.
      const body = e instanceof ApiError && e.body && typeof e.body === 'object' ? (e.body as Record<string, unknown>) : null
      const other = typeof body?.node_id === 'string' && body.node_id !== nodeId ? labels[body.node_id] : ''
      if (e instanceof ApiError && (e.status === 402 || e.status === 403) && other) error = `${other}: ${error}`
      if (e instanceof ApiError && e.status === 413) error = 'The sample is over 1 MB; paste fewer or smaller records.'
      // Without records a 400 means no live sample; anything else is a config or access problem.
      if (!records && e instanceof ApiError && e.status === 400 && /no sample/i.test(error)) {
        pastedSample.noLive = true
        pastedSample.active = true
      }
    } finally {
      if (my === seq) loading = false
    }
  }

  onMount(() => {
    void run()
  })

  // Rerun shortly after a config edit, once a preview has worked.
  $effect(() => {
    void chainKey
    void blocked
    const ready = untrack(() => result !== null || (pastedSample.active && pastedSample.text.trim() !== ''))
    if (!ready || blocked) return
    const t = setTimeout(() => { void run() }, 600)
    return () => clearTimeout(t)
  })

  function fieldClass(change: string, dropped: boolean): string {
    if (dropped) return 'text-fg-4 line-through'
    if (change === 'added') return 'bg-success-soft text-success'
    if (change === 'changed') return 'bg-warning-soft text-fg'
    if (change === 'removed') return 'text-fg-4 line-through'
    return 'text-fg-3'
  }
</script>

<div class="space-y-3">
  <div class="flex items-center gap-1.5">
    <Button size="xs" variant="outline" onclick={() => { void run() }} disabled={loading || !!blocked}>
      <RefreshCw size={12} /> {pastedSample.active ? 'Preview pasted records' : 'Refresh'}
    </Button>
    {#if pastedSample.active && !pastedSample.noLive}
      <Button size="xs" variant="ghost" onclick={() => { pastedSample.active = false; sampleError = ''; void run() }}>Use live sample</Button>
    {:else if !pastedSample.active}
      <Button size="xs" variant="ghost" onclick={() => { pastedSample.active = true }}>
        <ClipboardPaste size={12} /> Paste records
      </Button>
    {/if}
    {#if loading}<Spinner size="sm" />{/if}
  </div>

  {#if chainNote}
    <p class="text-[12px] text-fg-3">{chainNote}</p>
  {/if}

  {#if blocked}
    <p class="rounded-md bg-warning-soft px-2.5 py-2 text-[12px] text-warning">Fix the config first: {blocked}</p>
  {/if}

  {#if pastedSample.active}
    <div>
      <textarea
        rows={5}
        class="{monoInputClass} resize-y"
        aria-label="Sample records"
        placeholder={'{"Body":"level=info msg=started","ServiceName":"api"}\n{"Body":"level=error msg=timeout","ServiceName":"api"}'}
        bind:value={pastedSample.text}
      ></textarea>
      {#if sampleError}
        <p class="mt-1 text-[11px] text-danger">{sampleError}</p>
      {:else}
        <p class={hintClass}>One JSON object per line, up to {PREVIEW_MAX_RECORDS}. Used only for this preview.</p>
      {/if}
    </div>
  {/if}

  {#if error}
    <div class="flex items-start gap-2 rounded-md {pastedSample.noLive ? 'bg-info-soft text-info' : 'bg-danger-soft text-danger'} px-2.5 py-2 text-[12px]">
      <AlertTriangle size={13} class="mt-0.5 shrink-0" />
      <span class="min-w-0 break-words">{error}</span>
    </div>
  {/if}

  {#if result && step}
    <div class="flex flex-wrap items-center gap-1.5 text-[12px] tabular-nums text-fg-3">
      <span>{rows.length} in</span>
      <span>{keptCount} out</span>
      {#if droppedCount}<Badge tone="warning">{droppedCount} dropped</Badge>{/if}
      {#if errors.length}<Badge tone="danger">{errors.length} parse {errors.length === 1 ? 'error' : 'errors'}</Badge>{/if}
      {#if droppedEarlier > 0}<span>{droppedEarlier} dropped by earlier steps</span>{/if}
      <label class="ds-checkbox-label ml-auto text-[12px]">
        <input type="checkbox" class="ds-checkbox ds-checkbox-sm" bind:checked={showUnchanged} />
        Unchanged fields
      </label>
    </div>

    {#if errors.length}
      <ul class="space-y-0.5 rounded-md bg-danger-soft px-2.5 py-2 text-[12px] text-danger">
        {#each errors as err (err.index)}
          <li class="break-words">Record {err.index + 1}: {err.message}</li>
        {/each}
      </ul>
    {/if}

    {#if rows.length === 0}
      <EmptyState size="compact" title="No records reach this step" description={droppedEarlier ? 'Earlier steps dropped every record.' : 'The sample is empty.'} />
    {:else}
      <div class="space-y-2">
        {#each rows as row (row.index)}
          {@const visible = row.dropped || showUnchanged ? row.fields : row.fields.filter((f) => f.change !== 'same')}
          <div class="rounded-md border {row.dropped ? 'border-edge-subtle bg-surface-2' : 'border-edge-subtle bg-surface'}">
            <div class="flex items-center gap-2 border-b border-edge-subtle px-2.5 py-1 text-[11px] text-fg-3">
              <span class="tabular-nums">Record {row.index + 1}</span>
              {#if row.dropped}
                <Badge tone="warning" title={row.reason}>Dropped</Badge>
                <span class="min-w-0 truncate" title={row.reason}>{row.reason}</span>
              {:else if row.error}
                <Badge tone="danger" title={row.error}>Parse error, kept</Badge>
              {:else if visible.length === 0}
                <span class="text-fg-4">unchanged</span>
              {/if}
            </div>
            {#if visible.length}
              <table class="w-full table-fixed text-[12px]">
                <tbody>
                  {#each visible as f (f.path)}
                    <tr class="align-top">
                      <td class="w-2/5 truncate px-2.5 py-0.5 font-mono text-fg-3" title={f.path}>{f.path}</td>
                      <td class="px-2.5 py-0.5 font-mono">
                        {#if f.change === 'changed' && !row.dropped}
                          <span class="block break-all text-fg-4 line-through">{valueText(f.before)}</span>
                        {/if}
                        <span class="block break-all rounded-sm px-0.5 {fieldClass(f.change, row.dropped)}">{valueText(f.change === 'removed' ? f.before : f.after)}</span>
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            {/if}
          </div>
        {/each}
      </div>
    {/if}
  {:else if result && !step}
    <p class="text-[12px] text-fg-3">The server did not return this step.</p>
  {:else if !loading && !error && !blocked}
    <EmptyState size="compact" title="No preview yet" description="Refresh to run this chain on a sample." />
  {/if}
</div>
