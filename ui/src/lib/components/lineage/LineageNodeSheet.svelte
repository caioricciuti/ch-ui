<script lang="ts">
  import { errorText } from '../../utils/errors'
  import { fetchLineageNode } from '../../api/lineage'
  import { ApiError } from '../../api/client'
  import { success as toastSuccess, errorFrom as toastErrorFrom } from '../../stores/toast.svelte'
  import { copyToClipboard } from '../../utils/export'
  import { formatBytes, formatDate, formatNumber } from '../../utils/format'
  import { KIND_LABELS } from '../../utils/lineage-layout'
  import type { LineageNode, LineageNodeDetail } from '../../types/lineage'
  import Sheet from '../common/Sheet.svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Spinner from '../common/Spinner.svelte'

  interface Props {
    node: LineageNode | null
    /** The graph is already centered on this node. */
    isFocus: boolean
    onclose: () => void
    onfocus: (id: string) => void
    onopentable: (node: LineageNode) => void
    onopeneditor: (node: LineageNode) => void
  }

  let { node, isFocus, onclose, onfocus, onopentable, onopeneditor }: Props = $props()

  let detail = $state<LineageNodeDetail | null>(null)
  let loading = $state(false)
  let loadError = $state('')
  let seq = 0

  // The definition is fetched when the sheet opens on a node, never before.
  $effect(() => {
    const target = node
    detail = null
    loadError = ''
    // The node endpoint has nothing for these and answers 404.
    if (!target || target.database === '' || target.kind === 'external' || target.kind === 'missing' || target.kind === 'hidden') {
      loading = false
      return
    }
    const mine = ++seq
    loading = true
    fetchLineageNode(target.database, target.name).then(
      (d) => {
        if (mine === seq) detail = d
      },
      (e: unknown) => {
        if (mine !== seq) return
        if (e instanceof ApiError && e.status === 404) {
          loadError = 'This object no longer exists. Refresh the graph.'
          return
        }
        loadError = errorText(e)
        toastErrorFrom(e, 'load the object details')
      },
    ).finally(() => {
      if (mine === seq) loading = false
    })
  })

  // Engine tables pointing outside (Kafka, S3, MySQL) are still tables; table functions are not.
  const openable = $derived(!!node && node.database !== '' && node.kind !== 'missing' && node.kind !== 'hidden')
  const rows = $derived(detail?.rows ?? node?.rows ?? null)
  const bytes = $derived(detail?.bytes ?? node?.bytes ?? null)

  async function copyDefinition() {
    if (!detail?.ddl) return
    try {
      await copyToClipboard(detail.ddl)
      toastSuccess('Definition copied')
    } catch (e) {
      toastErrorFrom(e)
    }
  }

  function refreshTone(status: string): 'success' | 'danger' | 'warning' | 'neutral' {
    const s = status.toLowerCase()
    if (s.includes('error') || s.includes('fail') || s.includes('exception')) return 'danger'
    if (s.includes('running') || s.includes('waiting')) return 'warning'
    if (s.includes('scheduled') || s.includes('success')) return 'success'
    return 'neutral'
  }
</script>

<Sheet
  open={!!node}
  title={node?.name ?? ''}
  description={node ? `${node.database || 'External'} · ${KIND_LABELS[node.kind]}` : ''}
  size="lg"
  {onclose}
>
  {#if node}
    <dl class="grid grid-cols-[120px_1fr] gap-x-4 gap-y-2 text-[13px]">
      <dt class="text-fg-3">Engine</dt>
      <dd class="min-w-0 break-words text-fg">{detail?.engine_full || detail?.engine || node.engine || 'Unknown'}</dd>
      <dt class="text-fg-3">Rows</dt>
      <dd class="tabular-nums text-fg">{rows === null ? 'Unknown' : formatNumber(rows)}</dd>
      <dt class="text-fg-3">Size</dt>
      <dd class="tabular-nums text-fg">{bytes === null ? 'Unknown' : formatBytes(bytes)}</dd>
      {#if node.refresh}
        <dt class="text-fg-3">Refresh</dt>
        <dd class="flex flex-wrap items-center gap-x-3 gap-y-1 text-fg">
          <Badge tone={refreshTone(node.refresh.status)}>{node.refresh.status || 'Unknown'}</Badge>
          {#if node.refresh.last_success}<span class="text-fg-3">Last success {formatDate(node.refresh.last_success)}</span>{/if}
          {#if node.refresh.next}<span class="text-fg-3">Next {formatDate(node.refresh.next)}</span>{/if}
        </dd>
      {/if}
      {#if node.external}
        <dt class="text-fg-3">Points at</dt>
        <dd class="min-w-0 break-all font-mono text-xs text-fg">{node.external}</dd>
      {/if}
    </dl>

    {#if node.kind === 'missing'}
      <p class="mt-5 text-[13px] text-fg-3">A definition refers to this object, but it does not exist on the server.</p>
    {:else if node.kind === 'external'}
      <p class="mt-5 text-[13px] text-fg-3">
        {node.database ? 'This table reads or writes data outside ClickHouse.' : 'This data lives outside ClickHouse.'} There is no definition to show.
      </p>
    {:else}
      <div class="mb-2 mt-6 flex items-center justify-between gap-2">
        <h3 class="text-xs font-medium uppercase tracking-wider text-fg-3">Definition</h3>
        {#if detail?.ddl && !loading}
          <Button variant="ghost" size="xs" onclick={copyDefinition}>Copy</Button>
        {/if}
      </div>
      {#if loading}
        <div class="flex items-center gap-2 py-6 text-[13px] text-fg-3"><Spinner size="sm" /> Loading definition</div>
      {:else if loadError}
        <p class="rounded-md bg-danger-soft p-3 text-[13px] text-fg">{loadError}</p>
      {:else if detail?.ddl}
        <pre class="max-h-[50vh] overflow-y-auto whitespace-pre-wrap break-words [overflow-wrap:anywhere] rounded-md border border-edge-subtle bg-surface-2 p-3 font-mono text-xs leading-relaxed text-fg">{detail.ddl}</pre>
      {:else}
        <p class="text-[13px] text-fg-3">No definition available.</p>
      {/if}
    {/if}
  {/if}

  {#snippet footer()}
    {#if node}
      <Button variant="ghost" size="sm" disabled={isFocus} onclick={() => node && onfocus(node.id)}>Focus here</Button>
      <Button variant="outline" size="sm" disabled={!openable} onclick={() => node && onopeneditor(node)}>Open in editor</Button>
      <Button size="sm" disabled={!openable} onclick={() => node && onopentable(node)}>Open table</Button>
    {/if}
  {/snippet}
</Sheet>
