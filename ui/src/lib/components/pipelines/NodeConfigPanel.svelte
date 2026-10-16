<script lang="ts">
  import { untrack } from 'svelte'
  import { CONNECTOR_FIELDS, SECRET_MASK, generateToken, schemaColumnsOf, sinkFieldHidden, sinkPresetOf, type NodeType, type ConnectorFieldDef, type InferredSchema, type SchemaColumn } from '../../types/pipelines'
  import { X, HelpCircle, Copy, Check, Shield, RefreshCw } from 'lucide-svelte'
  import SinkSchemaEditor from './SinkSchemaEditor.svelte'
  import SinkTableLayout from './SinkTableLayout.svelte'
  import { signalHint } from '../../types/ingest'
  import { clearNodeError, getNodeError } from './transforms/counters.svelte'

  interface Props {
    nodeId: string
    nodeType: NodeType
    label: string
    config: Record<string, unknown>
    pipelineId: string
    onUpdate: (nodeId: string, config: Record<string, unknown>, label: string) => void
    onClose: () => void
    /** Saves, samples the source and returns ClickHouse's inferred schema. */
    onInferSchema?: () => Promise<InferredSchema | null>
    /** Signal of the pipeline's ingest source, if it has one (for the sink hint). */
    sourceSignal?: string | null
    /** Sinks: shown under an empty table name (a routed sink that did not get the OTel default). */
    tableHint?: string
    /** Active ingest keys of this pipeline; null while unknown. */
    activeKeys?: number | null
    onManageKeys?: () => void
    onSendData?: () => void
  }

  let { nodeId, nodeType, label, config, pipelineId, onUpdate, onClose, onInferSchema, sourceSignal = null, tableHint = '', activeKeys = null, onManageKeys, onSendData }: Props = $props()


  let copied = $state(false)
  let copiedToken = $state(false)
  /** Per number field: why the typed text was not applied. */
  let numberErrors = $state<Record<string, string>>({})

  // Copied once on mount; the editor remounts this panel per node ({#key}).
  let localLabel = $state(untrack(() => label))
  let localConfig = $state<Record<string, unknown>>(untrack(() => ({ ...config })))

  const isSink = $derived(nodeType === 'sink_clickhouse')
  const sinkPreset = $derived(sinkPresetOf(localConfig, sourceSignal))
  const fields = $derived(CONNECTOR_FIELDS[nodeType] || [])
  const visibleFields = $derived(isSink ? fields.filter((f) => !sinkFieldHidden(f.key, sinkPreset)) : fields)
  const tokenHidden = $derived(localConfig['auth_token'] === SECRET_MASK)

  // Initialize defaults for fields that don't have a value
  $effect(() => {
    let changed = false
    for (const field of fields) {
      if (localConfig[field.key] === undefined && field.default !== undefined) {
        localConfig[field.key] = field.default
        changed = true
      }
    }
    if (changed) {
      localConfig = { ...localConfig }
    }
  })

  const nodeError = $derived(getNodeError(nodeId))

  function handleChange(key: string, value: unknown) {
    clearNodeError(nodeId)
    localConfig = { ...localConfig, [key]: value }
    onUpdate(nodeId, localConfig, localLabel)
  }

  function handleLabelChange(value: string) {
    localLabel = value
    onUpdate(nodeId, localConfig, localLabel)
  }

  function getFieldValue(field: ConnectorFieldDef): unknown {
    return localConfig[field.key] ?? field.default ?? ''
  }

  /** An emptied or invalid number keeps the previous value and shows why. */
  function handleNumberInput(field: ConnectorFieldDef, raw: string) {
    const text = raw.trim()
    const n = Number(text)
    let problem = ''
    if (text === '' || !Number.isFinite(n)) problem = 'Enter a number'
    else if (!field.decimal && !Number.isInteger(n)) problem = 'Enter a whole number'
    else if (field.min !== undefined && n < field.min) problem = `Must be at least ${field.min}`
    else if (field.max !== undefined && n > field.max) problem = `Must be at most ${field.max}`
    if (problem) {
      numberErrors = { ...numberErrors, [field.key]: problem }
      return
    }
    const { [field.key]: _cleared, ...rest } = numberErrors
    numberErrors = rest
    handleChange(field.key, n)
  }

  const schemaColumns = $derived(schemaColumnsOf(localConfig))

  function setSchemaColumns(columns: SchemaColumn[]) {
    handleChange('create_table_columns', columns.length ? columns : null)
  }

  function applySchema(schema: InferredSchema) {
    localConfig = {
      ...localConfig,
      create_table_columns: schema.columns,
      create_table_order_by: schema.order_by,
      create_table_partition_by: schema.partition_by,
    }
    onUpdate(nodeId, localConfig, localLabel)
  }

  function handleAuthToggle(enabled: boolean) {
    if (enabled) {
      localConfig = { ...localConfig, auth_enabled: true, auth_token: generateToken() }
    } else {
      localConfig = { ...localConfig, auth_enabled: false, auth_token: '' }
    }
    onUpdate(nodeId, localConfig, localLabel)
  }

  function regenerateToken() {
    localConfig = { ...localConfig, auth_enabled: true, auth_token: generateToken() }
    onUpdate(nodeId, localConfig, localLabel)
  }

  const inputClass = 'w-full rounded-md border border-edge bg-surface px-2.5 py-1.5 text-[13px] text-fg hover:border-edge-strong focus:border-accent focus:outline-none'
</script>

<div class="flex flex-col h-full border-l border-edge-subtle bg-canvas w-72">
  <!-- Header -->
  <div class="flex items-center justify-between px-3 py-2 border-b border-edge-subtle">
    <h3 class="text-xs font-semibold text-fg-2 uppercase tracking-wider">
      Node Config
    </h3>
    <button
      class="p-1 rounded hover:bg-hover text-fg-4"
      onclick={onClose}
      aria-label="Close node config"
      title="Close"
    >
      <X size={14} />
    </button>
  </div>

  <!-- Form -->
  <div class="flex-1 overflow-auto p-3 space-y-3">
    {#if nodeError}
      <p class="rounded-md bg-danger-soft px-2.5 py-2 text-[12px] text-danger break-words">{nodeError}</p>
    {/if}
    <!-- Label -->
    <div>
      <label for="node-label-input" class="block text-[11px] font-medium text-fg-3 uppercase tracking-wider mb-1">
        Label
      </label>
      <input
        id="node-label-input"
        type="text"
        value={localLabel}
        oninput={(e) => handleLabelChange((e.target as HTMLInputElement).value)}
        class={inputClass}
      />
    </div>

    <div class="border-t border-edge-subtle pt-3">
      <p class="text-[11px] font-medium text-fg-3 uppercase tracking-wider mb-2">
        {nodeType.replace('source_', '').replace('sink_', '')} Settings
      </p>
    </div>

    {#each visibleFields as field (field.key)}
      {@const inputId = `node-field-${field.key}`}
      <div>
        <label for={inputId} class="flex items-center gap-1 text-[11px] font-medium text-fg-3 uppercase tracking-wider mb-1">
          {field.label}
          {#if field.required}
            <span class="text-danger">*</span>
          {/if}
          {#if field.help}
            <span class="relative group">
              <HelpCircle size={10} class="text-fg-4 cursor-help" />
              <span class="absolute bottom-full left-1/2 -translate-x-1/2 mb-1 w-48 p-1.5 rounded border border-edge bg-elevated text-[11px] normal-case tracking-normal text-fg hidden group-hover:block z-50 shadow-popover">
                {field.help}
              </span>
            </span>
          {/if}
        </label>

        {#if field.type === 'text' || field.type === 'password'}
          <input
            id={inputId}
            type={field.type}
            value={String(getFieldValue(field))}
            placeholder={field.placeholder}
            oninput={(e) => handleChange(field.key, (e.target as HTMLInputElement).value)}
            onfocus={(e) => { if (getFieldValue(field) === SECRET_MASK) (e.target as HTMLInputElement).select() }}
            class={inputClass}
          />
          {#if field.key === 'table' && tableHint && !String(localConfig['table'] ?? '').trim()}
            <p class="mt-1 text-[11px] text-fg-4">{tableHint}</p>
          {/if}
          {#if field.type === 'password' && getFieldValue(field) === SECRET_MASK}
            <p class="mt-1 text-[11px] text-fg-4">Stored encrypted. Leave as is to keep it, or type a new value.</p>
          {/if}
        {:else if field.type === 'number'}
          <input
            id={inputId}
            type="number"
            min={field.min}
            max={field.max}
            step={field.decimal ? 'any' : 1}
            value={Number(getFieldValue(field))}
            oninput={(e) => handleNumberInput(field, (e.target as HTMLInputElement).value)}
            aria-invalid={numberErrors[field.key] ? 'true' : undefined}
            class="{inputClass} {numberErrors[field.key] ? 'border-danger' : ''}"
          />
          {#if numberErrors[field.key]}
            <p class="mt-1 text-[11px] text-danger">{numberErrors[field.key]}; keeping {String(getFieldValue(field))}</p>
          {/if}
        {:else if field.type === 'select'}
          <select
            id={inputId}
            value={String(getFieldValue(field))}
            onchange={(e) => handleChange(field.key, (e.target as HTMLSelectElement).value)}
            class={inputClass}
          >
            {#each field.options || [] as opt}
              <option value={opt.value}>{opt.label}</option>
            {/each}
          </select>
          {#if nodeType === 'source_ingest' && field.key === 'signal'}
            <p class="mt-1 text-[11px] text-fg-4">{signalHint(getFieldValue(field))}</p>
          {/if}
        {:else if field.type === 'textarea'}
          <textarea
            id={inputId}
            value={String(getFieldValue(field))}
            placeholder={field.placeholder}
            oninput={(e) => handleChange(field.key, (e.target as HTMLTextAreaElement).value)}
            rows={3}
            class="{inputClass} resize-none font-mono"
          ></textarea>
        {:else if field.type === 'toggle'}
          <button
            id={inputId}
            aria-label={field.label}
            aria-pressed={Boolean(getFieldValue(field))}
            class="relative inline-flex h-5 w-9 items-center rounded-full transition-colors {
 getFieldValue(field) ? 'bg-accent' : 'bg-edge'
            }"
            onclick={() => {
              if (field.key === 'auth_enabled') {
                handleAuthToggle(!getFieldValue(field))
              } else {
                handleChange(field.key, !getFieldValue(field))
              }
            }}
          >
            <span
              class="inline-block h-3.5 w-3.5 transform rounded-full bg-surface transition-transform shadow-sm {
 getFieldValue(field) ? 'translate-x-4' : 'translate-x-0.5'
              }"
            ></span>
          </button>
          {#if field.key === 'auth_enabled' && getFieldValue(field)}
            <div class="mt-2">
              <p class="flex items-center gap-1 text-[11px] font-medium text-fg-3 uppercase tracking-wider mb-1">
                <Shield size={10} class="text-accent" />
                Bearer Token
              </p>
              {#if tokenHidden || !localConfig['auth_token']}
                <p class="text-[12px] text-fg-3">
                  {tokenHidden ? 'The token is stored encrypted and not shown again.' : 'No token set.'}
                  Generate a new one to see it; senders then need the new token.
                </p>
                <button
                  class="mt-1.5 inline-flex items-center gap-1 rounded-md border border-edge px-2 py-1 text-[12px] text-fg-2 hover:bg-hover"
                  onclick={regenerateToken}
                >
                  <RefreshCw size={12} /> Generate new token
                </button>
              {:else}
                <div class="flex items-center gap-1">
                  <input
                    type="text"
                    value={String(localConfig['auth_token'])}
                    readonly
                    aria-label="Bearer token"
                    class="flex-1 rounded-lg border border-edge bg-surface px-2.5 py-1.5 text-[12px] text-fg-2 font-mono select-all cursor-text"
                  />
                  <button
                    class="p-1.5 rounded-lg border border-edge hover:bg-hover text-fg-3 shrink-0"
                    title="Copy token"
                    aria-label="Copy token"
                    onclick={() => {
                      navigator.clipboard.writeText(String(localConfig['auth_token']))
                      copiedToken = true
                      setTimeout(() => { copiedToken = false }, 2000)
                    }}
                  >
                    {#if copiedToken}
                      <Check size={12} class="text-success" />
                    {:else}
                      <Copy size={12} />
                    {/if}
                  </button>
                </div>
                <p class="text-[11px] text-fg-4 mt-1">
                  Copy it now: after saving it is hidden. Use header Authorization: Bearer {String(localConfig['auth_token']).slice(0, 8)}...
                </p>
              {/if}
            </div>
          {/if}
        {:else if field.type === 'info'}
          {@const webhookUrl = `${typeof window !== 'undefined' ? window.location.origin : ''}/api/pipelines/webhook/${pipelineId}`}
          <div class="flex items-center gap-1">
            <input
              id={inputId}
              type="text"
              value={webhookUrl}
              readonly
              class="flex-1 rounded-lg border border-edge bg-surface px-2.5 py-1.5 text-[12px] text-fg-2 font-mono select-all cursor-text"
            />
            <button
              class="p-1.5 rounded-lg border border-edge hover:bg-hover text-fg-3 shrink-0"
              title="Copy URL"
              aria-label="Copy URL"
              onclick={() => {
                navigator.clipboard.writeText(webhookUrl)
                copied = true
                setTimeout(() => { copied = false }, 2000)
              }}
            >
              {#if copied}
                <Check size={12} class="text-success" />
              {:else}
                <Copy size={12} />
              {/if}
            </button>
          </div>
        {/if}
      </div>
    {/each}

    {#if nodeType === 'source_ingest'}
      <div class="space-y-2 border-t border-edge-subtle pt-3">
        <p class="text-[11px] font-medium text-fg-3 uppercase tracking-wider">Ingest keys</p>
        <p class="text-[12px] text-fg-3">
          {activeKeys === null ? 'Shippers authenticate with a key of this pipeline.' : activeKeys === 1 ? '1 active key.' : `${activeKeys} active keys.`}
        </p>
        <div class="flex flex-wrap gap-1.5">
          {#if onManageKeys}
            <button class="inline-flex items-center rounded-md border border-edge px-2 py-1 text-[12px] text-fg-2 hover:bg-hover" onclick={onManageKeys}>Manage keys</button>
          {/if}
          {#if onSendData}
            <button class="inline-flex items-center rounded-md border border-edge px-2 py-1 text-[12px] text-fg-2 hover:bg-hover" onclick={onSendData}>Send data here</button>
          {/if}
        </div>
      </div>
    {/if}

    {#if isSink}
      <SinkTableLayout config={localConfig} preset={sinkPreset} {sourceSignal} onChange={handleChange} />
    {/if}
    {#if isSink && sinkPreset === 'custom' && localConfig['create_table']}
      <SinkSchemaEditor
        columns={schemaColumns}
        database={String(localConfig['database'] ?? 'default')}
        table={String(localConfig['table'] ?? '')}
        engine={String(localConfig['create_table_engine'] ?? 'MergeTree')}
        orderBy={String(localConfig['create_table_order_by'] ?? '')}
        partitionBy={String(localConfig['create_table_partition_by'] ?? '')}
        onColumns={setSchemaColumns}
        onApply={applySchema}
        onInfer={onInferSchema}
      />
    {/if}
  </div>
</div>
