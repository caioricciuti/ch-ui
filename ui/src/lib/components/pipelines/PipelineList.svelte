<script lang="ts">
  import type { Pipeline } from '../../types/pipelines'
  import { formatDate } from '../../utils/format'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import Input from '../common/Input.svelte'
  import PageBody from '../common/PageBody.svelte'
  import PageHeader from '../common/PageHeader.svelte'
  import Spinner from '../common/Spinner.svelte'
  import { Plus, Trash2, Play, Square, Workflow } from 'lucide-svelte'

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

  type Tone = 'neutral' | 'success' | 'warning' | 'danger'

  function statusTone(status: string): Tone {
    switch (status) {
      case 'running':
        return 'success'
      case 'error':
        return 'danger'
      case 'starting':
      case 'stopping':
        return 'warning'
      default:
        return 'neutral'
    }
  }
</script>

<div class="flex h-full min-h-0 flex-col">
  <PageHeader title="Pipelines" subtitle="Stream data into ClickHouse from external sources">
    {#snippet meta()}
      {#if !loading}
        <Badge tone="neutral">{pipelines.length} total</Badge>
      {/if}
    {/snippet}
    {#snippet actions()}
      <Input size="sm" type="search" bind:value={search} placeholder="Search pipelines" class="w-52" />
      <Button size="sm" onclick={onCreate}>
        <Plus size={14} /> New pipeline
      </Button>
    {/snippet}
  </PageHeader>

  <PageBody width="lg">
    {#if loading}
      <div class="flex items-center justify-center py-12"><Spinner /></div>
    {:else if filtered.length === 0}
      {#if search.trim()}
        <EmptyState icon={Workflow} title="No pipelines match “{search}”" secondary={{ label: 'Clear search', onclick: () => (search = '') }} />
      {:else}
        <EmptyState
          icon={Workflow}
          title="No pipelines yet"
          description="A pipeline connects a source (Kafka, HTTP, files) to a ClickHouse table and keeps rows flowing."
          primary={{ label: 'New pipeline', onclick: onCreate }}
        />
      {/if}
    {:else}
      <div class="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
        {#each filtered as pipeline (pipeline.id)}
          <div
            class="group cursor-pointer rounded-lg border border-edge-subtle bg-surface p-4 text-left transition-colors hover:border-edge-strong hover:bg-hover"
            onclick={() => onSelect(pipeline.id)}
            onkeydown={(e) => { if (e.key === 'Enter') onSelect(pipeline.id) }}
            role="button"
            tabindex={0}
          >
            <div class="mb-3 flex items-start justify-between gap-2">
              <div class="min-w-0 flex-1">
                <h3 class="truncate text-[13px] font-medium text-fg">{pipeline.name}</h3>
                {#if pipeline.description}
                  <p class="mt-0.5 truncate text-xs text-fg-3">{pipeline.description}</p>
                {/if}
              </div>
              <Badge tone={statusTone(pipeline.status)} dot={pipeline.status === 'running'}>{pipeline.status}</Badge>
            </div>

            <div class="flex items-center justify-between">
              <span class="text-[11px] text-fg-4">
                Updated {formatDate(pipeline.updated_at)}
              </span>
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <div role="presentation" class="flex items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100" onclick={(e: MouseEvent) => e.stopPropagation()}>
                {#if pipeline.status === 'running' || pipeline.status === 'starting'}
                  <Button icon size="xs" variant="ghost" title="Stop pipeline" aria-label="Stop pipeline" onclick={() => onStop(pipeline.id)}>
                    <Square size={13} />
                  </Button>
                {:else}
                  <Button icon size="xs" variant="ghost" title="Start pipeline" aria-label="Start pipeline" onclick={() => onStart(pipeline.id)}>
                    <Play size={13} />
                  </Button>
                {/if}
                <Button icon size="xs" variant="ghost" class="hover:text-danger" title="Delete pipeline" aria-label="Delete pipeline" onclick={() => { confirmDelete = pipeline }}>
                  <Trash2 size={13} />
                </Button>
              </div>
            </div>

            {#if pipeline.last_error && pipeline.status === 'error'}
              <div class="mt-2 truncate rounded-sm bg-danger-soft px-2 py-1 text-[11px] text-danger" title={pipeline.last_error}>
                {pipeline.last_error}
              </div>
            {/if}
          </div>
        {/each}
      </div>
    {/if}
  </PageBody>
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
