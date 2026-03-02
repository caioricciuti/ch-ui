<script lang="ts">
  import type { PipelineStatus } from '../../types/pipelines'
  import Button from '../common/Button.svelte'
  import { ArrowLeft, Save, Play, Square, RotateCcw } from 'lucide-svelte'

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
</script>

<div class="flex items-center justify-between px-3 py-2 border-b border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-950">
  <div class="flex items-center gap-2">
    <button
      class="p-1.5 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-800 text-gray-500 dark:text-gray-400"
      onclick={onBack}
      title="Back to pipelines"
    >
      <ArrowLeft size={16} />
    </button>
    <h2 class="text-sm font-semibold text-gray-900 dark:text-gray-100 truncate max-w-[300px]">
      {pipelineName}
    </h2>
    <span class="text-[10px] px-1.5 py-0.5 rounded-full font-medium {
      status === 'running' ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400' :
      status === 'error' ? 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400' :
      status === 'draft' ? 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400' :
      'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400'
    }">{status}</span>
  </div>

  <div class="flex items-center gap-2">
    <Button size="sm" variant="secondary" onclick={onSave} loading={saving}>
      <Save size={14} /> Save
    </Button>
    {#if isRunning}
      <Button size="sm" variant="danger" onclick={onStop}>
        <Square size={14} /> Stop
      </Button>
    {:else}
      <Button size="sm" onclick={onStart}>
        <Play size={14} /> Run
      </Button>
    {/if}
  </div>
</div>
