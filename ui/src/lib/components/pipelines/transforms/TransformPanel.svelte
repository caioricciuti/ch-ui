<script lang="ts">
  import { untrack } from 'svelte'
  import { X } from 'lucide-svelte'
  import { isProTransform, mapPathHint, proTransformMessage, transformLabel, transformProblems, type PreviewNode, type TransformNodeType } from '../../../types/transforms'
  import { isProActive } from '../../../stores/license.svelte'
  import { clearNodeError, getNodeError } from './counters.svelte'
  import Tabs from '../../common/Tabs.svelte'
  import ParseConfig from './ParseConfig.svelte'
  import MapConfig from './MapConfig.svelte'
  import FilterConfig from './FilterConfig.svelte'
  import SampleConfig from './SampleConfig.svelte'
  import RedactConfig from './RedactConfig.svelte'
  import RouteConfig from './RouteConfig.svelte'
  import TransformPreview from './TransformPreview.svelte'
  import { inputClass, labelClass } from './ui'

  interface Props {
    nodeId: string
    nodeType: TransformNodeType
    label: string
    config: Record<string, unknown>
    pipelineId: string
    /** Signal of the pipeline's ingest source, if any. */
    signal?: string | null
    /** Transforms from the source up to and including this node, current configs. */
    chain: (PreviewNode & { label: string })[]
    /** Why the chain is not complete, shown in the preview. */
    chainNote?: string
    /** Past a route: the route output this node's branch hangs off. */
    branch?: string
    /** Route nodes: label of the node each connected output feeds, by handle. */
    connected?: Record<string, string>
    onUpdate: (nodeId: string, config: Record<string, unknown>, label: string) => void
    onClose: () => void
  }

  let { nodeId, nodeType, label, config, pipelineId, signal = null, chain, chainNote = '', branch = '', connected = {}, onUpdate, onClose }: Props = $props()

  // Copied once on mount; the editor remounts this panel per node ({#key}).
  let localLabel = $state(untrack(() => label))
  let localConfig = $state<Record<string, unknown>>(untrack(() => ({ ...config })))
  let tab = $state<'config' | 'preview'>('config')

  const problems = $derived(transformProblems(nodeType, localConfig))
  const needsPro = $derived(isProTransform(nodeType) && !isProActive())
  /** First problem anywhere in the chain; the preview waits for it. */
  const blocked = $derived.by(() => {
    for (const n of chain) {
      const p = transformProblems(n.node_type, n.id === nodeId ? localConfig : n.config)
      if (p.length) return n.id === nodeId ? p[0] : `${n.label}: ${p[0]}`
    }
    return ''
  })
  const previewChain = $derived<PreviewNode[]>(chain.map((n) => ({ id: n.id, node_type: n.node_type, config: n.id === nodeId ? localConfig : n.config })))

  const saveError = $derived(getNodeError(nodeId))
  const pathHint = $derived(mapPathHint(signal))

  const labels = $derived(Object.fromEntries(chain.map((n) => [n.id, n.id === nodeId ? localLabel : n.label])))

  function setConfig(next: Record<string, unknown>) {
    clearNodeError(nodeId)
    localConfig = next
    onUpdate(nodeId, localConfig, localLabel)
  }

  function setLabel(v: string) {
    localLabel = v
    onUpdate(nodeId, localConfig, localLabel)
  }
</script>

<div class="flex h-full flex-col border-l border-edge-subtle bg-canvas {tab === 'preview' ? 'w-[560px]' : 'w-96'}">
  <div class="flex items-center justify-between border-b border-edge-subtle px-3 py-2">
    <h3 class="text-xs font-semibold uppercase tracking-wider text-fg-2">{transformLabel(nodeType)}</h3>
    <button class="rounded p-1 text-fg-4 hover:bg-hover" onclick={onClose} aria-label="Close node config" title="Close">
      <X size={14} />
    </button>
  </div>

  <div class="flex h-10 shrink-0 items-center border-b border-edge-subtle px-3">
    <Tabs
      variant="segmented"
      size="sm"
      items={[{ id: 'config', label: 'Config' }, { id: 'preview', label: 'Preview' }]}
      value={tab}
      onchange={(id) => { tab = id === 'preview' ? 'preview' : 'config' }}
    />
  </div>

  <div class="flex-1 space-y-3 overflow-auto p-3">
    {#if needsPro}
      <p class="rounded-md bg-info-soft px-2.5 py-2 text-[12px] text-info">
        {proTransformMessage(nodeType)} to run and preview. The config is saved on any edition.
      </p>
    {/if}

    {#if saveError}
      <p class="rounded-md bg-danger-soft px-2.5 py-2 text-[12px] text-danger break-words">{saveError}</p>
    {/if}

    {#if tab === 'config'}
      <div>
        <label for="transform-label" class={labelClass}>Label</label>
        <input id="transform-label" type="text" class={inputClass} value={localLabel} oninput={(e) => setLabel(e.currentTarget.value)} />
      </div>

      <div class="border-t border-edge-subtle pt-3">
        {#if pathHint}
          <p class="mb-3 text-[11px] text-fg-4">{pathHint}</p>
        {/if}
        {#if nodeType === 'transform_parse'}
          <ParseConfig config={localConfig} {signal} onchange={setConfig} />
        {:else if nodeType === 'transform_map'}
          <MapConfig config={localConfig} {signal} onchange={setConfig} />
        {:else if nodeType === 'transform_filter'}
          <FilterConfig config={localConfig} {signal} onchange={setConfig} />
        {:else if nodeType === 'transform_sample'}
          <SampleConfig config={localConfig} {signal} onchange={setConfig} />
        {:else if nodeType === 'transform_redact'}
          <RedactConfig config={localConfig} onchange={setConfig} />
        {:else if nodeType === 'transform_route'}
          <RouteConfig config={localConfig} {signal} {connected} onchange={setConfig} />
        {/if}
      </div>

      {#if problems.length}
        <ul class="space-y-0.5 rounded-md bg-warning-soft px-2.5 py-2 text-[12px] text-warning">
          {#each problems as p (p)}
            <li>{p}</li>
          {/each}
        </ul>
      {/if}
    {:else}
      <TransformPreview {pipelineId} {nodeId} {nodeType} chain={previewChain} {labels} {chainNote} {blocked} {branch} />
    {/if}
  </div>
</div>
