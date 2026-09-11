<script lang="ts">
  import type { BrainArtifact } from '../../types/brain'
  import { copyToClipboard } from '../../utils/export'
  import { success as toastSuccess } from '../../stores/toast.svelte'
  import { highlightSQL } from './brain-markdown'
  import BrainArtifactCard from './BrainArtifactCard.svelte'
  import { Copy, ExternalLink, Play } from 'lucide-svelte'
  import Spinner from '../common/Spinner.svelte'
  import Badge from '../common/Badge.svelte'

  interface Props {
    sql: string
    messageId?: string
    artifact?: BrainArtifact | null
    running?: boolean
    onRun: (sql: string, messageId?: string) => void
    onOpenInEditor: (sql: string) => void
  }

  let { sql, messageId, artifact = null, running = false, onRun, onOpenInEditor }: Props = $props()

  async function handleCopy() {
    await copyToClipboard(sql)
    toastSuccess('Copied to clipboard')
  }
</script>

<div class="my-3 rounded-lg border border-edge-subtle overflow-hidden">
  <div class="flex items-center justify-between px-3 py-1.5 bg-surface-2 border-b border-edge-subtle">
    <Badge tone="neutral" class="text-[10px]">SQL</Badge>
    <div class="flex items-center gap-1">
      <button
        class="inline-flex items-center gap-1 px-1.5 py-0.5 text-[11px] text-fg-3 hover:text-fg rounded hover:bg-hover transition-colors"
        title="Copy"
        onclick={handleCopy}
      >
        <Copy size={12} />
      </button>
      <button
        class="inline-flex items-center gap-1 px-1.5 py-0.5 text-[11px] text-fg-3 hover:text-fg rounded hover:bg-hover transition-colors"
        title="Open in Editor"
        onclick={() => onOpenInEditor(sql)}
      >
        <ExternalLink size={12} />
        <span>Editor</span>
      </button>
      <button
        class="inline-flex items-center gap-1 px-2 py-0.5 text-[11px] font-medium text-accent-fg bg-accent hover:brightness-110 rounded-md transition-colors disabled:opacity-50"
        onclick={() => onRun(sql, messageId)}
        disabled={running}
      >
        {#if running}
          <Spinner size="sm" />
        {:else}
          <Play size={12} />
        {/if}
        <span>Run</span>
      </button>
    </div>
  </div>
  <pre class="p-3 text-xs font-mono overflow-x-auto bg-surface leading-relaxed"><code>{@html highlightSQL(sql)}</code></pre>
</div>
{#if artifact}
  <BrainArtifactCard {artifact} />
{/if}
