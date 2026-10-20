<script lang="ts">
  import { onMount } from 'svelte'
  import { takePageIntent } from '../lib/stores/page-intent.svelte'
  import type { Pipeline } from '../lib/types/pipelines'
  import * as api from '../lib/api/pipelines'
  import { success as toastSuccess, errorFrom as toastErrorFrom } from '../lib/stores/toast.svelte'
  import { getCurrentPipelineId, pushPipelineDetail, pushPipelineList } from '../lib/stores/router.svelte'
  import PipelineList from '../lib/components/pipelines/PipelineList.svelte'
  import PipelineEditor from '../lib/components/pipelines/PipelineEditor.svelte'
  import Sheet from '../lib/components/common/Sheet.svelte'
  import FormField from '../lib/components/common/FormField.svelte'
  import Input from '../lib/components/common/Input.svelte'
  import Button from '../lib/components/common/Button.svelte'

  let pipelines = $state<Pipeline[]>([])
  let loading = $state(true)
  const selectedPipelineId = $derived(getCurrentPipelineId() ?? null)
  let showCreate = $state(false)
  let createName = $state('')
  let createLoading = $state(false)
  /** Pipelines with a start or stop request in flight. */
  let pendingIds = $state<string[]>([])

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

  // "New pipeline" from the command palette.
  $effect(() => {
    if (takePageIntent('new-pipeline')) showCreate = true
  })

  async function loadPipelines() {
    loading = true
    try {
      const res = await api.listPipelines()
      pipelines = res.pipelines ?? []
    } catch (e: unknown) {
      toastErrorFrom(e, 'load pipelines')
    } finally {
      loading = false
    }
  }

  function cancelCreate() {
    showCreate = false
    createName = ''
  }

  function submitCreate(e: SubmitEvent) {
    e.preventDefault()
    const name = createName.trim()
    if (name) void handleCreate(name)
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
      toastErrorFrom(e, 'create the pipeline')
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
      toastErrorFrom(e, 'delete the pipeline')
    }
  }

  function handleStart(id: string) {
    return withPending(id, async () => {
      try {
        await api.startPipeline(id)
        toastSuccess('Pipeline started')
      } catch (e: unknown) {
        toastErrorFrom(e, 'start the pipeline')
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
        toastErrorFrom(e, 'stop the pipeline')
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

<Sheet open={showCreate} title="Create Pipeline" description="Also the name shown in the pipeline list and in alerts. Source and sink are set in the editor." size="sm" onclose={cancelCreate}>
  <form id="pipeline-create-form" onsubmit={submitCreate}>
    <FormField label="Name" for="pipeline-create-name" required controlWidth="full">
      <Input id="pipeline-create-name" placeholder="My pipeline" bind:value={createName} disabled={createLoading} />
    </FormField>
  </form>
  {#snippet footer()}
    <Button variant="ghost" size="sm" onclick={cancelCreate} disabled={createLoading}>Cancel</Button>
    <Button size="sm" type="submit" form="pipeline-create-form" loading={createLoading} disabled={!createName.trim()}>Create</Button>
  {/snippet}
</Sheet>
