<script lang="ts">
  import type { PipelineStatus } from '../../types/pipelines'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import { ArrowLeft, Save, Play, Square } from 'lucide-svelte'

  interface Props {
    pipelineName: string
    status: PipelineStatus
    saving: boolean
    onBack: () => void
    onSave: () => void
    onStart: () => void
    onStop: () => void
  }

  let { pipelineName, status, saving, onBack, onSave, onStart, onStop }: Props = $props()

  const isRunning = $derived(status === 'running' || status === 'starting')

  const tone = $derived(
    status === 'running' ? 'success'
    : status === 'error' ? 'danger'
    : status === 'starting' || status === 'stopping' ? 'warning'
    : 'neutral',
  )
</script>

<header class="flex h-12 shrink-0 items-center gap-3 border-b border-edge-subtle px-5">
  <Button icon size="sm" variant="ghost" class="-ml-2" onclick={onBack} title="Back to pipelines" aria-label="Back to pipelines">
    <ArrowLeft size={15} />
  </Button>
  <div class="flex min-w-0 items-baseline gap-3">
    <span class="shrink-0 text-[13px] text-fg-3">Pipelines</span>
    <h1 class="truncate text-[15px] font-semibold tracking-[-0.01em] text-fg">{pipelineName}</h1>
  </div>
  <Badge {tone} dot={status === 'running'}>{status}</Badge>

  <div class="ml-auto flex shrink-0 items-center gap-2">
    <Button size="sm" variant="outline" onclick={onSave} loading={saving}>
      <Save size={13} /> Save
    </Button>
    {#if isRunning}
      <Button size="sm" variant="danger" onclick={onStop}>
        <Square size={13} /> Stop
      </Button>
    {:else}
      <Button size="sm" onclick={onStart}>
        <Play size={13} /> Run
      </Button>
    {/if}
  </div>
</header>
