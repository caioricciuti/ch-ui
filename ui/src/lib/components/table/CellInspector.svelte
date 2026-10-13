<script lang="ts">
  import Sheet from '../common/Sheet.svelte'
  import Button from '../common/Button.svelte'
  import { Copy } from 'lucide-svelte'
  import { inspectCellText, cellCopyText } from '../../utils/cell-inspect'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'

  interface Props {
    open: boolean
    column: string
    type: string
    row: number
    value: unknown
    onclose: () => void
  }

  let { open, column, type, row, value, onclose }: Props = $props()

  const shown = $derived(inspectCellText(value))

  async function copy(text: string) {
    try {
      await navigator.clipboard.writeText(text)
      toastSuccess('Copied to clipboard')
    } catch {
      toastError('Clipboard unavailable')
    }
  }
</script>

<Sheet {open} title={column} description={`${type} · row ${row + 1}`} size="md" {onclose}>
  {#if value === null || value === undefined}
    <span class="inline-flex items-center rounded-sm bg-surface-2 px-1.5 py-0.5 text-xs font-medium uppercase tracking-wide text-fg-3">Null</span>
  {:else}
    <pre class="overflow-auto rounded-md border border-edge-subtle bg-surface-2 p-3 font-mono text-xs text-fg whitespace-pre-wrap break-words">{shown.text}</pre>
  {/if}
  {#snippet footer()}
    <Button size="sm" variant="ghost" onclick={onclose}>Close</Button>
    {#if shown.json}
      <Button size="sm" variant="outline" onclick={() => void copy(cellCopyText(value))}>Copy compact</Button>
    {/if}
    <Button size="sm" disabled={value === null || value === undefined} onclick={() => void copy(shown.json ? shown.text : cellCopyText(value))}>
      <Copy size={14} />
      Copy
    </Button>
  {/snippet}
</Sheet>
