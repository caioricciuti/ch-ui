<script lang="ts">
  import { errorText } from '../../utils/errors'
  import Sheet from '../common/Sheet.svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Select from '../common/Select.svelte'
  import Tabs from '../common/Tabs.svelte'
  import Spinner from '../common/Spinner.svelte'
  import FormField from '../common/FormField.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import Input from '../common/Input.svelte'
  import IngestSecret from './IngestSecret.svelte'
  import { listPipelines } from '../../api/pipelines'
  import { createIngestKey, getIngestInfo } from '../../api/ingest'
  import { setupReceive } from '../../api/telemetry'
  import { listDatabases } from '../../api/query'
  import {
    buildSnippet, defaultReceiveTable, isIngestSignal, pickIngestPipeline, receiveSetupError, shipperApisFor, shippersFor, snippetFile, usesOtelLayout,
    KEY_PLACEHOLDER, type IngestSignal, type ReceiveSignal, type Shipper, type ShipperApi,
  } from '../../types/ingest'
  import type { PipelineStatus } from '../../types/pipelines'
  import { pushPipelineList } from '../../stores/router.svelte'
  import { getSession } from '../../stores/session.svelte'
  import { hasProReadAccess } from '../../stores/license.svelte'
  import { success, error as toastError, errorFrom as toastErrorFrom } from '../../stores/toast.svelte'
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
  let esUrl = $state('')
  let hecUrl = $state('')
  let pipelines = $state<IngestPipeline[]>([])
  let selectedId = $state('')
  let api = $state<ShipperApi>('native')
  let shipper = $state<Shipper>('collector')
  /** Keys created in this sheet, per pipeline; never stored anywhere else. */
  let secrets = $state<Record<string, { name: string; secret: string }>>({})
  let naming = $state(false)
  let newName = $state('')
  let creating = $state(false)
  let nameInput = $state<HTMLInputElement>()
  /** Receive setup form, shown when the signal has no ingest pipeline. */
  let pickedSignal = $state<ReceiveSignal>('logs')
  let databases = $state<string[]>([])
  let setupDatabase = $state('default')
  let setupTable = $state('')
  let setupTtl = $state<string | number>(30)
  let settingUp = $state(false)
  /** Pipelines made by the setup in this sheet; their source is already registered. */
  let setupIds = $state<string[]>([])

  const canWrite = $derived((getSession()?.role ?? 'viewer') !== 'viewer')
  const selected = $derived(pipelines.find((p) => p.id === selectedId) ?? null)
  const apis = $derived(selected ? shipperApisFor(selected.signal) : [])
  const shippers = $derived(selected ? shippersFor(selected.signal, api) : [])
  const created = $derived(selected ? secrets[selected.id] ?? null : null)
  const snippet = $derived(
    selected && baseUrl ? buildSnippet({ baseUrl, esUrl, hecUrl, signal: selected.signal, shipper, key: created?.secret }) : '',
  )
  const pipelineOptions = $derived(pipelines.map((p) => ({ value: p.id, label: `${p.name} (${p.signal})` })))
  /** The Telemetry page fixes the signal; elsewhere the setup asks for it. */
  const fixedSignal = $derived<ReceiveSignal | null>(signal === 'logs' || signal === 'traces' ? signal : null)
  const setupSignal = $derived<ReceiveSignal>(fixedSignal ?? pickedSignal)
  const databaseOptions = $derived(databases.map((d) => ({ value: d, label: d })))
  const setupInvalid = $derived(receiveSetupError({ signal: setupSignal, database: setupDatabase, table: setupTable, ttl_days: Number(setupTtl) }))
  const signalOptions = [
    { value: 'logs', label: 'Logs' },
    { value: 'traces', label: 'Traces' },
  ]

  $effect(() => {
    if (open) void load()
  })

  // Keep the API and the shipper valid for the selected signal.
  $effect(() => {
    if (apis.length && !apis.some((a) => a.id === api)) api = apis[0].id
  })
  $effect(() => {
    if (shippers.length && !shippers.some((s) => s.id === shipper)) shipper = shippers[0].id
  })

  $effect(() => {
    if (naming) nameInput?.focus()
  })

  function startNaming() {
    newName = ''
    naming = true
  }

  function cancelNaming() {
    naming = false
    newName = ''
  }

  function submitCreate(e: SubmitEvent) {
    e.preventDefault()
    void create(newName.trim())
  }

  async function load() {
    loading = true
    error = ''
    try {
      const [info, list] = await Promise.all([getIngestInfo(), listPipelines()])
      baseUrl = info.base_url
      esUrl = info.es_url ?? ''
      hecUrl = info.hec_url ?? ''
      pipelines = (list.pipelines ?? [])
        .filter((p) => p.source_type === 'source_ingest')
        .map((p) => ({ id: p.id, name: p.name, status: p.status, signal: isIngestSignal(p.signal) ? p.signal : 'logs' }))
      if (pipelineId && secret) secrets = { ...secrets, [pipelineId]: { name: 'new key', secret } }
      selectedId = pickIngestPipeline(pipelines, { keepId: pipelineId, previousId: selectedId, signal })
      if (!selectedId && canWrite) await loadDatabases()
    } catch (e: unknown) {
      error = errorText(e)
    } finally {
      loading = false
    }
  }

  async function loadDatabases() {
    setupTable = defaultReceiveTable(setupSignal)
    setupTtl = 30
    try {
      databases = await listDatabases()
    } catch {
      databases = []
    }
    if (!databases.includes('default')) databases = ['default', ...databases]
    setupDatabase = 'default'
  }

  function pickSignal(v: string) {
    const next: ReceiveSignal = v === 'traces' ? 'traces' : 'logs'
    // Swap the table only while it is still the other signal's default.
    if (setupTable === defaultReceiveTable(pickedSignal)) setupTable = defaultReceiveTable(next)
    pickedSignal = next
  }

  async function submitSetup(e: SubmitEvent) {
    e.preventDefault()
    if (setupInvalid || settingUp) return
    settingUp = true
    try {
      const res = await setupReceive({ signal: setupSignal, database: setupDatabase, table: setupTable.trim(), ttl_days: Number(setupTtl) })
      secrets = { ...secrets, [res.pipeline.id]: { name: res.key.name, secret: res.secret } }
      setupIds = [...setupIds, res.pipeline.id]
      selectedId = res.pipeline.id
      success(`Receiving ${setupSignal} into ${res.database}.${res.table}`)
      await load()
    } catch (err: unknown) {
      toastErrorFrom(err, 'set up receiving')
    } finally {
      settingUp = false
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
      toastErrorFrom(e)
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
  {:else if !selected && !canWrite}
    <EmptyState size="compact" title="No {fixedSignal ?? 'ingest'} pipeline yet" description="Ask an admin or analyst to set this up." />
  {:else if !selected}
    <form id="send-data-setup" class="space-y-5" onsubmit={submitSetup}>
      <p class="text-[13px] text-fg-2">
        No {fixedSignal ?? 'ingest'} pipeline yet. Set up creates the table, an ingest pipeline with a key, and the Telemetry source in one step.
      </p>
      {#if !fixedSignal}
        <FormField label="Signal" for="send-data-signal" controlWidth="full">
          <Select id="send-data-signal" options={signalOptions} value={pickedSignal} onchange={pickSignal} disabled={settingUp} />
        </FormField>
      {/if}
      <FormField label="Database" for="send-data-database" controlWidth="full">
        <Select id="send-data-database" options={databaseOptions} bind:value={setupDatabase} disabled={settingUp} />
      </FormField>
      <FormField label="Table" for="send-data-table" controlWidth="full" hint="Created in the OpenTelemetry layout if it does not exist.">
        <Input id="send-data-table" mono bind:value={setupTable} disabled={settingUp} />
      </FormField>
      <FormField label="Retention in days" for="send-data-ttl" controlWidth="full" hint="Rows older than this are dropped.">
        <Input id="send-data-ttl" type="number" min={1} step={1} bind:value={setupTtl} disabled={settingUp} />
      </FormField>
      {#if setupSignal === 'traces' && !hasProReadAccess()}
        <p class="text-xs text-fg-3">Traces are stored without a license; browsing them in Telemetry, Traces needs Pro.</p>
      {/if}
      <p class="text-xs text-fg-3">
        To build the pipeline by hand instead, use <button type="button" class="text-accent hover:underline" onclick={openPipelines}>Pipelines</button>.
      </p>
    </form>
  {:else if selected}
    <div class="space-y-5">
      <FormField label="Pipeline" for="send-data-pipeline" controlWidth="full">
        <div class="flex items-center gap-2">
          <Select id="send-data-pipeline" options={pipelineOptions} bind:value={selectedId} class="flex-1" />
          {#if selected.status === 'running'}
            <Badge tone="success">running</Badge>
          {:else}
            <Badge tone="warning" title="Receivers answer 503 until the pipeline runs">{selected.status}</Badge>
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
            {#if canWrite && !naming}
              <Button size="sm" variant="outline" onclick={startNaming}>Create key</Button>
            {/if}
          </div>
          {#if naming}
            <form class="space-y-3 rounded-md bg-surface-2 p-3" onsubmit={submitCreate}>
              <FormField label="Key name" for="send-data-key-name" required controlWidth="full" hint="Also the name shown in the pipeline's key list. Use it to recognise the shipper, e.g. prod-collector.">
                <Input id="send-data-key-name" placeholder="prod-collector" bind:value={newName} bind:ref={nameInput} disabled={creating} />
              </FormField>
              <div class="flex justify-end gap-2">
                <Button size="sm" variant="ghost" onclick={cancelNaming} disabled={creating}>Cancel</Button>
                <Button size="sm" type="submit" loading={creating} disabled={!newName.trim()}>Create key</Button>
              </div>
            </form>
          {/if}
        {/if}
      </div>

      <div class="space-y-2">
        {#if apis.length > 1}
          <Tabs variant="segmented" size="sm" items={apis} value={api} onchange={(id) => (api = id as ShipperApi)} />
        {/if}
        <Tabs variant="segmented" size="sm" items={shippers} value={shipper} onchange={(id) => (shipper = id as Shipper)} />
        {#if api === 'elasticsearch'}
          <p class="text-xs text-fg-3">CH-UI answers the Elasticsearch bulk API and the calls shippers make at startup. Keep template and ILM setup off; update and delete actions are refused.</p>
        {:else if api === 'splunk'}
          <p class="text-xs text-fg-3">The key is the HEC token. Fluent Bit's splunk output and Docker's splunk driver need the separate ingest port, which serves HEC at the root.</p>
        {/if}
        <div class="relative">
          <p class="mb-1 font-mono text-[11px] text-fg-4">{snippetFile(shipper)}</p>
          <pre class="overflow-x-auto whitespace-pre rounded-md bg-surface-2 p-3 pr-10 font-mono text-xs leading-relaxed text-fg-2">{snippet}</pre>
          <Button icon variant="ghost" size="xs" class="absolute right-1.5 top-6" aria-label="Copy snippet" title="Copy" onclick={copySnippet}>
            <Copy size={13} />
          </Button>
        </div>
      </div>

      {#if usesOtelLayout(selected.signal) && !setupIds.includes(selected.id)}
        <p class="text-xs text-fg-3">
          If this pipeline was built in Pipelines, add its table under Telemetry, Sources (Detect sources finds it) to browse the {selected.signal}. Set up here registers the source itself.
        </p>
      {/if}
    </div>
  {/if}
  {#snippet footer()}
    <Button size="sm" variant="ghost" onclick={onclose}>Close</Button>
    {#if !loading && !error && !selected && canWrite}
      <Button size="sm" type="submit" form="send-data-setup" loading={settingUp} disabled={!!setupInvalid} title={setupInvalid || undefined}>Set up</Button>
    {:else if snippet && !loading}
      <Button size="sm" onclick={copySnippet}><Copy size={14} /> Copy snippet</Button>
    {/if}
  {/snippet}
</Sheet>
