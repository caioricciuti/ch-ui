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

  onMount(async () => {
    await loadPipelines()
  })

  async function loadPipelines() {
    loading = true
    try {
      const res = await api.listPipelines()
      pipelines = res.pipelines ?? []
    } catch (e: any) {
      toastError(e.message || 'Failed to load pipelines')
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
    } catch (e: any) {
      toastError(e.message || 'Failed to create pipeline')
    } finally {
      createLoading = false
    }
  }

  async function handleDelete(id: string) {
    try {
      await api.deletePipeline(id)
      toastSuccess('Pipeline deleted')
      await loadPipelines()
    } catch (e: any) {
      toastError(e.message || 'Failed to delete pipeline')
    }
  }

  async function handleStart(id: string) {
    try {
      await api.startPipeline(id)
      toastSuccess('Pipeline started')
      await loadPipelines()
    } catch (e: any) {
      toastError(e.message || 'Failed to start pipeline')
    }
  }

  async function handleStop(id: string) {
    try {
      await api.stopPipeline(id)
      toastSuccess('Pipeline stopped')
      await loadPipelines()
    } catch (e: any) {
      toastError(e.message || 'Failed to stop pipeline')
    }
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
