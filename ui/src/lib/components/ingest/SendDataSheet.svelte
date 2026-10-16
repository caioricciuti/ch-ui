<script lang="ts">
  import Sheet from '../common/Sheet.svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Select from '../common/Select.svelte'
  import Tabs from '../common/Tabs.svelte'
  import Spinner from '../common/Spinner.svelte'
  import FormField from '../common/FormField.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import InputDialog from '../common/InputDialog.svelte'
  import IngestSecret from './IngestSecret.svelte'
  import { listPipelines } from '../../api/pipelines'
  import { createIngestKey, getIngestInfo } from '../../api/ingest'
  import { buildSnippet, isIngestSignal, shippersFor, snippetFile, usesOtelLayout, KEY_PLACEHOLDER, type IngestSignal, type Shipper } from '../../types/ingest'
  import type { PipelineStatus } from '../../types/pipelines'
  import { pushPipelineList } from '../../stores/router.svelte'
  import { getSession } from '../../stores/session.svelte'
  import { success, error as toastError } from '../../stores/toast.svelte'
  import { AlertTriangle, Copy } from 'lucide-svelte'

  interface Props {
    open: boolean
    onclose: () => void
    /** Signal to prefer when picking the first pipeline (the Telemetry page in view). */
    signal?: IngestSignal
    /** Pipeline to select on open. */
    pipelineId?: string
    /** A key the caller just created for pipelineId, used in the snippet. */
    secret?: string
  }

  let { open, onclose, signal, pipelineId, secret }: Props = $props()

  interface IngestPipeline {
    id: string
    name: string
    status: PipelineStatus
    signal: IngestSignal
  }

  let loading = $state(false)
  let error = $state('')
  let baseUrl = $state('')
  let pipelines = $state<IngestPipeline[]>([])
  let selectedId = $state('')
  let shipper = $state<Shipper>('collector')
  /** Keys created in this sheet, per pipeline; never stored anywhere else. */
  let secrets = $state<Record<string, { name: string; secret: string }>>({})
  let naming = $state(false)
  let newName = $state('')
  let creating = $state(false)

  const canWrite = $derived((getSession()?.role ?? 'viewer') !== 'viewer')
  const selected = $derived(pipelines.find((p) => p.id === selectedId) ?? null)
  const shippers = $derived(selected ? shippersFor(selected.signal) : [])
  const created = $derived(selected ? secrets[selected.id] ?? null : null)
  const snippet = $derived(
    selected && baseUrl ? buildSnippet({ baseUrl, signal: selected.signal, shipper, key: created?.secret }) : '',
  )
  const pipelineOptions = $derived(pipelines.map((p) => ({ value: p.id, label: `${p.name} (${p.signal})` })))

  $effect(() => {
    if (open) void load()
  })

  // Keep the shipper valid for the selected signal.
  $effect(() => {
    if (shippers.length && !shippers.some((s) => s.id === shipper)) shipper = shippers[0].id
  })

  async function load() {
    loading = true
    error = ''
    try {
      const [info, list] = await Promise.all([getIngestInfo(), listPipelines()])
      baseUrl = info.base_url
      pipelines = (list.pipelines ?? [])
        .filter((p) => p.source_type === 'source_ingest')
        .map((p) => ({ id: p.id, name: p.name, status: p.status, signal: isIngestSignal(p.signal) ? p.signal : 'logs' }))
      if (pipelineId && secret) secrets = { ...secrets, [pipelineId]: { name: 'new key', secret } }
      const keep = pipelines.find((p) => p.id === (pipelineId || selectedId))
      selectedId = (keep ?? pipelines.find((p) => p.signal === signal) ?? pipelines[0])?.id ?? ''
    } catch (e: unknown) {
      error = e instanceof Error ? e.message : String(e)
    } finally {
      loading = false
    }
  }

  async function create(name: string) {
    if (!selected || !name) return
    creating = true
    try {
      const res = await createIngestKey(selected.id, name)
      secrets = { ...secrets, [selected.id]: { name: res.key.name, secret: res.secret } }
      naming = false
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      creating = false
    }
  }

  async function copySnippet() {
    try {
      await navigator.clipboard.writeText(snippet)
      success('Snippet copied')
    } catch {
      toastError('Clipboard unavailable')
    }
  }

  function openPipelines() {
    onclose()
    pushPipelineList()
  }
</script>

<Sheet {open} {onclose} size="lg" title="Send data here" description="Point a shipper at an ingest pipeline. CH-UI spools what it receives and writes it to ClickHouse.">
  {#if loading}
    <div class="flex justify-center py-10"><Spinner /></div>
  {:else if error}
    <EmptyState size="compact" icon={AlertTriangle} title="Could not load" description={error} primary={{ label: 'Retry', onclick: () => void load() }} />
  {:else if pipelines.length === 0}
    <EmptyState
      size="compact"
      title="No ingest pipeline yet"
      description="Create a pipeline in Pipelines with an Ingest (OTLP, JSON) source and a ClickHouse sink with Create Table on, then come back for the shipper config."
      primary={{ label: 'Open Pipelines', onclick: openPipelines }}
    />
  {:else if selected}
    <div class="space-y-5">
      <FormField label="Pipeline" for="send-data-pipeline" controlWidth="full">
        <div class="flex items-center gap-2">
          <Select id="send-data-pipeline" options={pipelineOptions} bind:value={selectedId} class="flex-1" />
          {#if selected.status === 'running'}
            <Badge tone="success">running</Badge>
          {:else}
            <Badge tone="warning" title="Receivers answer 404 until the pipeline runs">{selected.status}</Badge>
          {/if}
        </div>
      </FormField>

      {#if selected.status !== 'running'}
        <p class="rounded-md bg-warning-soft px-3 py-2 text-xs text-warning">This pipeline is not running. Start it in Pipelines; until then requests are refused.</p>
      {/if}

      <div class="space-y-2">
        <p class="text-xs font-medium text-fg-2">Key</p>
        {#if created}
          <IngestSecret name={created.name} secret={created.secret} />
        {:else}
          <div class="flex items-center justify-between gap-3 rounded-md bg-surface-2 px-3 py-2">
            <p class="text-xs text-fg-3">The snippet uses {KEY_PLACEHOLDER}. Use an existing key of this pipeline, or create one now to fill it in.</p>
            {#if canWrite}
              <Button size="sm" variant="outline" onclick={() => { newName = ''; naming = true }}>Create key</Button>
            {/if}
          </div>
        {/if}
      </div>

      <div class="space-y-2">
        <Tabs variant="segmented" size="sm" items={shippers} value={shipper} onchange={(id) => (shipper = id as Shipper)} />
        <div class="relative">
          <p class="mb-1 font-mono text-[11px] text-fg-4">{snippetFile(shipper)}</p>
          <pre class="overflow-x-auto whitespace-pre rounded-md bg-surface-2 p-3 pr-10 font-mono text-xs leading-relaxed text-fg-2">{snippet}</pre>
          <Button icon variant="ghost" size="xs" class="absolute right-1.5 top-6" aria-label="Copy snippet" title="Copy" onclick={copySnippet}>
            <Copy size={13} />
          </Button>
        </div>
      </div>

      {#if usesOtelLayout(selected.signal)}
        <p class="text-xs text-fg-3">
          With Create Table on in the sink, the table is created in the OpenTelemetry layout. Add it under Telemetry, Sources (Detect sources finds it) to browse the {selected.signal}.
        </p>
      {/if}
    </div>
  {/if}
  {#snippet footer()}
    <Button size="sm" variant="ghost" onclick={onclose}>Close</Button>
    {#if snippet && !loading}
      <Button size="sm" onclick={copySnippet}><Copy size={14} /> Copy snippet</Button>
    {/if}
  {/snippet}
</Sheet>

<InputDialog
  open={naming}
  title="Create ingest key"
  description="A name to recognise the shipper that uses it, e.g. prod-collector."
  placeholder="prod-collector"
  bind:value={newName}
  confirmLabel="Create key"
  loading={creating}
  onconfirm={create}
  oncancel={() => (naming = false)}
/>
