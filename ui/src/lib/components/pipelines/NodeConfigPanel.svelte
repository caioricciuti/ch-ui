<script lang="ts">
  import { CONNECTOR_FIELDS, type NodeType, type ConnectorFieldDef } from '../../types/pipelines'
  import { X, HelpCircle, Copy, Check, Shield } from 'lucide-svelte'

  interface Props {
    nodeId: string
    nodeType: NodeType
    label: string
    config: Record<string, unknown>
    pipelineId: string
    onUpdate: (nodeId: string, config: Record<string, unknown>, label: string) => void
    onClose: () => void
  }

  let { nodeId, nodeType, label, config, pipelineId, onUpdate, onClose }: Props = $props()

  let copied = $state(false)
  let copiedToken = $state(false)

  let localLabel = $state(label)  // eslint-disable-line -- initial value from prop is intentional
  let localConfig = $state<Record<string, unknown>>({ ...config })  // eslint-disable-line -- initial value from prop is intentional

  const fields = $derived(CONNECTOR_FIELDS[nodeType] || [])

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

  function handleChange(key: string, value: unknown) {
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

  function generateToken(): string {
    const bytes = new Uint8Array(24)
    crypto.getRandomValues(bytes)
    return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
  }

  function handleAuthToggle(enabled: boolean) {
    if (enabled) {
      const token = generateToken()
      localConfig = { ...localConfig, auth_enabled: true, auth_token: token }
    } else {
      localConfig = { ...localConfig, auth_enabled: false, auth_token: '' }
    }
    onUpdate(nodeId, localConfig, localLabel)
  }
</script>

<div class="flex flex-col h-full border-l border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-950 w-72">
  <!-- Header -->
  <div class="flex items-center justify-between px-3 py-2 border-b border-gray-200 dark:border-gray-800">
    <h3 class="text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider">
      Node Config
    </h3>
    <button
      class="p-1 rounded hover:bg-gray-100 dark:hover:bg-gray-800 text-gray-400"
      onclick={onClose}
    >
      <X size={14} />
    </button>
  </div>

  <!-- Form -->
  <div class="flex-1 overflow-auto p-3 space-y-3">
    <!-- Label -->
    <div>
      <label for="node-label-input" class="block text-[10px] font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-1">
        Label
      </label>
      <input
        id="node-label-input"
        type="text"
        value={localLabel}
        oninput={(e) => handleLabelChange((e.target as HTMLInputElement).value)}
        class="w-full rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 px-2.5 py-1.5 text-xs text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-orange-500"
      />
    </div>

    <div class="border-t border-gray-200 dark:border-gray-800 pt-3">
      <p class="text-[10px] font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-2">
        {nodeType.replace('source_', '').replace('sink_', '')} Settings
      </p>
    </div>

    {#each fields as field (field.key)}
      <div>
        <label class="flex items-center gap-1 text-[10px] font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-1">
          {field.label}
          {#if field.required}
            <span class="text-red-400">*</span>
          {/if}
          {#if field.help}
            <span class="relative group">
              <HelpCircle size={10} class="text-gray-400 cursor-help" />
              <span class="absolute bottom-full left-1/2 -translate-x-1/2 mb-1 w-48 p-1.5 rounded bg-gray-900 dark:bg-gray-100 text-[9px] text-white dark:text-gray-900 hidden group-hover:block z-50 shadow-lg">
                {field.help}
              </span>
            </span>
          {/if}
        </label>

        {#if field.type === 'text' || field.type === 'password'}
          <input
            type={field.type}
            value={String(getFieldValue(field))}
            placeholder={field.placeholder}
            oninput={(e) => handleChange(field.key, (e.target as HTMLInputElement).value)}
            class="w-full rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 px-2.5 py-1.5 text-xs text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-orange-500"
          />
        {:else if field.type === 'number'}
          <input
            type="number"
            value={Number(getFieldValue(field)) || 0}
            oninput={(e) => handleChange(field.key, Number((e.target as HTMLInputElement).value))}
            class="w-full rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 px-2.5 py-1.5 text-xs text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-orange-500"
          />
        {:else if field.type === 'select'}
          <select
            value={String(getFieldValue(field))}
            onchange={(e) => handleChange(field.key, (e.target as HTMLSelectElement).value)}
            class="w-full rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 px-2.5 py-1.5 text-xs text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-orange-500"
          >
            {#each field.options || [] as opt}
              <option value={opt.value}>{opt.label}</option>
            {/each}
          </select>
        {:else if field.type === 'textarea'}
          <textarea
            value={String(getFieldValue(field))}
            placeholder={field.placeholder}
            oninput={(e) => handleChange(field.key, (e.target as HTMLTextAreaElement).value)}
            rows={3}
            class="w-full rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 px-2.5 py-1.5 text-xs text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-orange-500 resize-none font-mono"
          ></textarea>
        {:else if field.type === 'toggle'}
          <button
            aria-label={field.label}
            class="relative inline-flex h-5 w-9 items-center rounded-full transition-colors {
              getFieldValue(field) ? 'bg-orange-500' : 'bg-gray-300 dark:bg-gray-700'
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
              class="inline-block h-3.5 w-3.5 transform rounded-full bg-white transition-transform shadow-sm {
                getFieldValue(field) ? 'translate-x-4' : 'translate-x-0.5'
              }"
            ></span>
          </button>
          {#if field.key === 'auth_enabled' && getFieldValue(field) && localConfig['auth_token']}
            <div class="mt-2">
              <label class="flex items-center gap-1 text-[10px] font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-1">
                <Shield size={10} class="text-orange-500" />
                Bearer Token
              </label>
              <div class="flex items-center gap-1">
                <input
                  type="text"
                  value={String(localConfig['auth_token'])}
                  readonly
                  class="flex-1 rounded-lg border border-gray-300 dark:border-gray-700 bg-gray-50 dark:bg-gray-800/50 px-2.5 py-1.5 text-xs text-gray-700 dark:text-gray-300 font-mono select-all cursor-text"
                />
                <button
                  class="p-1.5 rounded-lg border border-gray-300 dark:border-gray-700 hover:bg-gray-100 dark:hover:bg-gray-800 text-gray-500 shrink-0"
                  title="Copy token"
                  onclick={() => {
                    navigator.clipboard.writeText(String(localConfig['auth_token']))
                    copiedToken = true
                    setTimeout(() => { copiedToken = false }, 2000)
                  }}
                >
                  {#if copiedToken}
                    <Check size={12} class="text-green-500" />
                  {:else}
                    <Copy size={12} />
                  {/if}
                </button>
              </div>
              <p class="text-[9px] text-gray-400 mt-1">
                Use header: Authorization: Bearer {String(localConfig['auth_token']).slice(0, 8)}...
              </p>
            </div>
          {/if}
        {:else if field.type === 'info'}
          {@const webhookUrl = `${typeof window !== 'undefined' ? window.location.origin : ''}/api/pipelines/webhook/${pipelineId}`}
          <div class="flex items-center gap-1">
            <input
              type="text"
              value={webhookUrl}
              readonly
              class="flex-1 rounded-lg border border-gray-300 dark:border-gray-700 bg-gray-50 dark:bg-gray-800/50 px-2.5 py-1.5 text-xs text-gray-700 dark:text-gray-300 font-mono select-all cursor-text"
            />
            <button
              class="p-1.5 rounded-lg border border-gray-300 dark:border-gray-700 hover:bg-gray-100 dark:hover:bg-gray-800 text-gray-500 shrink-0"
              title="Copy URL"
              onclick={() => {
                navigator.clipboard.writeText(webhookUrl)
                copied = true
                setTimeout(() => { copied = false }, 2000)
              }}
            >
              {#if copied}
                <Check size={12} class="text-green-500" />
              {:else}
                <Copy size={12} />
              {/if}
            </button>
          </div>
        {/if}
      </div>
    {/each}
  </div>
</div>
