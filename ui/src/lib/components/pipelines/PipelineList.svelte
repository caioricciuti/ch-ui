<script lang="ts">
  import type { Pipeline } from '../../types/pipelines'
  import Button from '../common/Button.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import Spinner from '../common/Spinner.svelte'
  import {
    Plus,
    Trash2,
    Play,
    Square,
    Pencil,
    Workflow,
    AlertCircle,
    Clock,
    Radio,
  } from 'lucide-svelte'

  interface Props {
    pipelines: Pipeline[]
    loading: boolean
    onCreate: () => void
    onSelect: (id: string) => void
    onDelete: (id: string) => void
    onStart: (id: string) => void
    onStop: (id: string) => void
  }

  let { pipelines, loading, onCreate, onSelect, onDelete, onStart, onStop }: Props = $props()

  let confirmDelete = $state<Pipeline | null>(null)
  let search = $state('')

  const filtered = $derived(
    search.trim()
      ? pipelines.filter((p) => p.name.toLowerCase().includes(search.toLowerCase()))
      : pipelines,
  )

  function statusColor(status: string): string {
    switch (status) {
      case 'running':
        return 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400'
      case 'error':
        return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400'
      case 'starting':
      case 'stopping':
        return 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400'
      case 'draft':
        return 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400'
      default:
        return 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400'
    }
  }

  function statusIcon(status: string) {
    switch (status) {
      case 'running':
        return Radio
      case 'error':
        return AlertCircle
      case 'starting':
      case 'stopping':
        return Clock
      default:
        return Workflow
    }
  }

  function formatDate(date: string): string {
    return new Date(date).toLocaleDateString(undefined, {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    })
  }
</script>

<div class="flex flex-col h-full">
  <!-- Header -->
  <div class="flex items-center justify-between px-4 py-3 border-b border-gray-200 dark:border-gray-800">
    <div class="flex items-center gap-3">
      <Workflow size={18} class="text-ch-blue" />
      <h1 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Pipelines</h1>
      <span class="text-xs text-gray-400 dark:text-gray-600">{pipelines.length} total</span>
    </div>
    <div class="flex items-center gap-2">
      <input
        bind:value={search}
        type="text"
        placeholder="Search pipelines..."
        class="h-8 w-52 rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 px-3 text-xs text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-orange-500"
      />
      <Button size="sm" onclick={onCreate}>
        <Plus size={14} /> New Pipeline
      </Button>
    </div>
  </div>

  <!-- Content -->
  <div class="flex-1 overflow-auto p-4">
    {#if loading}
      <div class="flex items-center justify-center py-12"><Spinner /></div>
    {:else if filtered.length === 0}
      <div class="text-center py-12 text-gray-500">
        <Workflow size={36} class="mx-auto mb-2 text-gray-300 dark:text-gray-700" />
        {#if search.trim()}
          <p class="mb-1">No pipelines match "{search}"</p>
        {:else}
          <p class="mb-1">No pipelines yet</p>
          <p class="text-xs text-gray-400 dark:text-gray-600">Create a pipeline to start ingesting data into ClickHouse</p>
        {/if}
      </div>
    {:else}
      <div class="grid gap-3 grid-cols-1 md:grid-cols-2 xl:grid-cols-3">
        {#each filtered as pipeline (pipeline.id)}
          <div
            class="group text-left rounded-xl border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900 p-4 hover:border-orange-300 dark:hover:border-orange-700 hover:shadow-sm transition-all cursor-pointer"
            onclick={() => onSelect(pipeline.id)}
            onkeydown={(e) => { if (e.key === 'Enter') onSelect(pipeline.id) }}
            role="button"
            tabindex={0}
          >
            <div class="flex items-start justify-between mb-3">
              <div class="flex-1 min-w-0">
                <h3 class="font-medium text-sm text-gray-900 dark:text-gray-100 truncate">{pipeline.name}</h3>
                {#if pipeline.description}
                  <p class="text-xs text-gray-500 dark:text-gray-400 truncate mt-0.5">{pipeline.description}</p>
                {/if}
              </div>
              <span class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-medium {statusColor(pipeline.status)}">
                {#if pipeline.status === 'running'}
                  <Radio size={10} />
                {:else if pipeline.status === 'error'}
                  <AlertCircle size={10} />
                {:else if pipeline.status === 'starting' || pipeline.status === 'stopping'}
                  <Clock size={10} />
                {:else}
                  <Workflow size={10} />
                {/if}
                {pipeline.status}
              </span>
            </div>

            <div class="flex items-center justify-between">
              <span class="text-[10px] text-gray-400 dark:text-gray-600">
                Updated {formatDate(pipeline.updated_at)}
              </span>
              <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
              <div class="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity" onclick={(e: MouseEvent) => e.stopPropagation()}>
                {#if pipeline.status === 'running' || pipeline.status === 'starting'}
                  <button
                    class="p-1 rounded hover:bg-red-100 dark:hover:bg-red-900/30 text-red-500"
                    title="Stop pipeline"
                    onclick={() => onStop(pipeline.id)}
                  >
                    <Square size={14} />
                  </button>
                {:else}
                  <button
                    class="p-1 rounded hover:bg-green-100 dark:hover:bg-green-900/30 text-green-500"
                    title="Start pipeline"
                    onclick={() => onStart(pipeline.id)}
                  >
                    <Play size={14} />
                  </button>
                {/if}
                <button
                  class="p-1 rounded hover:bg-red-100 dark:hover:bg-red-900/30 text-gray-400 hover:text-red-500"
                  title="Delete pipeline"
                  onclick={() => { confirmDelete = pipeline }}
                >
                  <Trash2 size={14} />
                </button>
              </div>
            </div>

            {#if pipeline.last_error && pipeline.status === 'error'}
              <div class="mt-2 px-2 py-1 rounded bg-red-50 dark:bg-red-900/20 text-[10px] text-red-600 dark:text-red-400 truncate">
                {pipeline.last_error}
              </div>
            {/if}
          </div>
        {/each}
      </div>
    {/if}
  </div>
</div>

<ConfirmDialog
  open={!!confirmDelete}
  title="Delete pipeline?"
  description="This will permanently delete the pipeline and all its configuration. This cannot be undone."
  confirmLabel="Delete"
  destructive={true}
  onconfirm={() => {
    if (confirmDelete) {
      onDelete(confirmDelete.id)
      confirmDelete = null
    }
  }}
  oncancel={() => { confirmDelete = null }}
/>
