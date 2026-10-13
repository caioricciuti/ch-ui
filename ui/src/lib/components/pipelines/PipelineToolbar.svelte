<script lang="ts">
  import type { PipelineStatus } from '../../types/pipelines'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import { ArrowLeft, Save, Play, Square, History, FlaskConical } from 'lucide-svelte'

  interface Props {
    pipelineName: string
    status: PipelineStatus
    saving: boolean
    /** A start or stop request is in flight. */
    pending: boolean
    dirty: boolean
    /** Running, but the last batches or polls are failing and being retried. */
    retrying: boolean
    onBack: () => void
    onSave: () => void
    onStart: () => void
    onStop: () => void
    onRuns: () => void
    /** A test is running. */
    testing: boolean
    onTest: () => void
  }

  let { pipelineName, status, saving, pending, dirty, retrying, onBack, onSave, onStart, onStop, onRuns, testing, onTest }: Props = $props()

  const isRunning = $derived(status === 'running' || status === 'starting')

  const tone = $derived(
    status === 'running' ? (retrying ? 'warning' : 'success')
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
  <Badge {tone} dot={status === 'running'}>{status === 'running' && retrying ? 'running, retrying' : status}</Badge>
  {#if dirty}
    <span class="text-[13px] text-fg-3">Unsaved changes</span>
  {/if}

  <div class="ml-auto flex shrink-0 items-center gap-2">
    <Button size="sm" variant="ghost" onclick={onRuns} title="Runs and logs">
      <History size={13} /> Runs
    </Button>
    <Button size="sm" variant="ghost" onclick={onTest} loading={testing} disabled={testing || saving} title="Save, then read a sample from the source and check the sink">
      <FlaskConical size={13} /> Test
    </Button>
    <Button size="sm" variant="outline" onclick={onSave} loading={saving} disabled={pending}>
      <Save size={13} /> Save
    </Button>
    {#if isRunning}
      <Button size="sm" variant="danger" onclick={onStop} loading={pending} disabled={pending}>
        <Square size={13} /> Stop
      </Button>
    {:else}
      <Button size="sm" onclick={onStart} loading={pending} disabled={pending || saving}>
        <Play size={13} /> Run
      </Button>
    {/if}
  </div>
</header>
