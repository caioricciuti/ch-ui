<script lang="ts">
  import { onMount } from 'svelte'
  import type { Pipeline } from '../lib/types/pipelines'
  import * as api from '../lib/api/pipelines'
  import { success as toastSuccess, error as toastError } from '../lib/stores/toast.svelte'
  import { getCurrentPipelineId, pushPipelineDetail, pushPipelineList } from '../lib/stores/router.svelte'
  import PipelineList from '../lib/components/pipelines/PipelineList.svelte'
  import PipelineEditor from '../lib/components/pipelines/PipelineEditor.svelte'
  import InputDialog from '../lib/components/common/InputDialog.svelte'

  let pipelines = $state<Pipeline[]>([])
  let loading = $state(true)
  const selectedPipelineId = $derived(getCurrentPipelineId() ?? null)
  let showCreate = $state(false)
  let createName = $state('')
  let createLoading = $state(false)
  /** Pipelines with a start or stop request in flight. */
  let pendingIds = $state<string[]>([])

  function errorText(e: unknown, fallback: string): string {
    return (e instanceof Error ? e.message : String(e)) || fallback
  }

  async function withPending(id: string, fn: () => Promise<void>) {
    if (pendingIds.includes(id)) return
    pendingIds = [...pendingIds, id]
    try {
      await fn()
    } finally {
      pendingIds = pendingIds.filter((p) => p !== id)
    }
  }

  onMount(async () => {
    await loadPipelines()
  })

  async function loadPipelines() {
    loading = true
    try {
      const res = await api.listPipelines()
      pipelines = res.pipelines ?? []
    } catch (e: unknown) {
      toastError(errorText(e, 'Failed to load pipelines'))
    } finally {
      loading = false
    }
  }

  async function handleCreate(name: string) {
    createLoading = true
    try {
      const res = await api.createPipeline({ name })
      toastSuccess(`Pipeline "${name}" created`)
      showCreate = false
      createName = ''
      await loadPipelines()
      // Open the editor for the newly created pipeline
      pushPipelineDetail(res.pipeline.id)
    } catch (e: unknown) {
      toastError(errorText(e, 'Failed to create pipeline'))
    } finally {
      createLoading = false
    }
  }

  async function handleDelete(id: string) {
    try {
      await api.deletePipeline(id)
      toastSuccess('Pipeline deleted')
      await loadPipelines()
    } catch (e: unknown) {
      toastError(errorText(e, 'Failed to delete pipeline'))
    }
  }

  function handleStart(id: string) {
    return withPending(id, async () => {
      try {
        await api.startPipeline(id)
        toastSuccess('Pipeline started')
      } catch (e: unknown) {
        toastError(errorText(e, 'Failed to start pipeline'))
      }
      await loadPipelines()
    })
  }

  function handleStop(id: string) {
    return withPending(id, async () => {
      try {
        await api.stopPipeline(id)
        toastSuccess('Pipeline stopped')
      } catch (e: unknown) {
        toastError(errorText(e, 'Failed to stop pipeline'))
      }
      await loadPipelines()
    })
  }
</script>

{#if selectedPipelineId}
  <PipelineEditor
    pipelineId={selectedPipelineId}
    onBack={() => {
      pushPipelineList()
      loadPipelines()
    }}
  />
{:else}
  <PipelineList
    {pipelines}
    {loading}
    {pendingIds}
    onCreate={() => { showCreate = true }}
    onSelect={(id) => { pushPipelineDetail(id) }}
    onDelete={handleDelete}
    onStart={handleStart}
    onStop={handleStop}
  />
{/if}

<InputDialog
  open={showCreate}
  title="Create Pipeline"
  description="Give your pipeline a name. You can configure the source and sink in the editor."
  placeholder="My pipeline"
  bind:value={createName}
  confirmLabel="Create"
  loading={createLoading}
  onconfirm={handleCreate}
  oncancel={() => { showCreate = false; createName = '' }}
/>
