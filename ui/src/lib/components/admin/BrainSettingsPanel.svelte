<script lang="ts">
  import { onMount } from 'svelte'
  import { Plus, Trash2 } from 'lucide-svelte'
  import type { BrainModelOption, BrainModelPrice, BrainSettings } from '../../types/brain'
  import { adminGetBrainSettings, adminUpdateBrainSettings } from '../../api/brain'
  import { isPriced } from '../brain/brain-budget'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import SectionHeader from '../common/SectionHeader.svelte'
  import Panel from '../common/Panel.svelte'
  import FormField from '../common/FormField.svelte'
  import Select from '../common/Select.svelte'
  import Input from '../common/Input.svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Sheet from '../common/Sheet.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'

  // Model roles (agent and quick), the output cap and the price table each
  // request's cost is stored from. Budgets live in Admin > Brain budgets.

  interface Props {
    /** Every model the admin has synced; inactive ones are not offered. */
    models: BrainModelOption[]
  }

  let { models }: Props = $props()

  let settings = $state<BrainSettings | null>(null)
  let defaultPrices = $state<Record<string, BrainModelPrice>>({})
  let saving = $state(false)
  let form = $state<{ agentModelId: string; quickModelId: string; maxOutputTokens: string | number }>({ agentModelId: '', quickModelId: '', maxOutputTokens: '' })

  interface PriceDraft {
    key: number
    model: string
    input: string | number
    output: string | number
    cacheRead: string | number
    cacheWrite: string | number
  }
  let pricesOpen = $state(false)
  let priceDrafts = $state<PriceDraft[]>([])
  let priceError = $state('')
  let confirmResetPrices = $state(false)
  let nextKey = 0

  onMount(() => {
    void load()
  })

  async function load() {
    try {
      const res = await adminGetBrainSettings()
      settings = res.settings
      quickModelSet = res.quickModelSet
      defaultPrices = res.defaultPrices ?? {}
      form = {
        agentModelId: res.settings.agentModelId,
        quickModelId: res.settings.quickModelId,
        maxOutputTokens: res.settings.maxOutputTokens ? String(res.settings.maxOutputTokens) : '',
      }
      // Brain usage links here with ?edit=prices to fix unpriced models.
      const url = new URL(window.location.href)
      if (url.searchParams.get('edit') === 'prices') {
        url.searchParams.delete('edit')
        history.replaceState(null, '', `${url.pathname}?${url.searchParams.toString()}`)
        openPrices()
      }
    } catch (e) {
      toastError(e instanceof Error ? e.message : String(e))
    }
  }

  // Until an admin saves a quick model (Automatic included), Claude Haiku
  // 5.5 is pre-selected when it is synced and active. Nothing is stored
  // until Save.
  const SUGGESTED_QUICK_MODEL = 'claude-haiku-5-5'
  let quickModelSet = $state(true)
  let suggestionApplied = false
  $effect(() => {
    if (!settings || quickModelSet || suggestionApplied || form.quickModelId) return
    const id = models.find((m) => m.name === SUGGESTED_QUICK_MODEL && m.is_active && m.provider_active)?.id
    if (id) {
      form.quickModelId = id
      suggestionApplied = true
    }
  })

  const modelOptions = $derived([
    { value: '', label: 'Automatic: the default model' },
    ...models
      .filter((m) => m.is_active && m.provider_active)
      .map((m) => ({ value: m.id, label: `${m.display_name || m.name} (${m.provider_name})` })),
  ])

  function parseCount(raw: string | number, label: string, max?: number): number | null {
    const t = String(raw ?? '').trim()
    if (!t) return 0
    const n = Number(t)
    if (!Number.isInteger(n) || n < 0 || (max !== undefined && n > max)) {
      toastError(`${label} must be a whole number${max !== undefined ? ` from 0 to ${max.toLocaleString('en-US')}` : ' of 0 or more'}`)
      return null
    }
    return n
  }

  async function saveRoles() {
    const maxOut = parseCount(form.maxOutputTokens, 'Output token limit', 128000)
    if (maxOut === null) return
    saving = true
    try {
      settings = await adminUpdateBrainSettings({
        agentModelId: form.agentModelId,
        quickModelId: form.quickModelId,
        maxOutputTokens: maxOut,
      })
      quickModelSet = true
      toastSuccess('Brain settings saved')
    } catch (e) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      saving = false
    }
  }

  // ── Prices ────────────────────────────────────────────────
  function toDrafts(table: Record<string, BrainModelPrice> | null): PriceDraft[] {
    const str = (v: number | null | undefined) => (v === null || v === undefined ? '' : String(v))
    return Object.entries(table ?? {})
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([model, p]) => ({ key: nextKey++, model, input: str(p.input), output: str(p.output), cacheRead: str(p.cacheRead), cacheWrite: str(p.cacheWrite) }))
  }

  function openPrices() {
    priceDrafts = toDrafts(settings?.prices ?? null)
    priceError = ''
    pricesOpen = true
  }

  function addPriceRow() {
    priceDrafts = [...priceDrafts, { key: nextKey++, model: '', input: '', output: '', cacheRead: '', cacheWrite: '' }]
  }

  function removePriceRow(key: number) {
    priceDrafts = priceDrafts.filter((d) => d.key !== key)
  }

  function resetPrices() {
    confirmResetPrices = false
    priceDrafts = toDrafts(defaultPrices)
  }

  function buildPriceTable(): Record<string, BrainModelPrice> | null {
    const table: Record<string, BrainModelPrice> = {}
    const num = (raw: string | number): number | null | undefined => {
      const t = String(raw ?? '').trim()
      if (!t) return null
      const n = Number(t)
      return Number.isFinite(n) && n >= 0 ? n : undefined
    }
    for (const d of priceDrafts) {
      const model = d.model.trim()
      if (!model) {
        priceError = 'Every row needs a model id.'
        return null
      }
      if (table[model]) {
        priceError = `${model} is listed twice.`
        return null
      }
      const p = { input: num(d.input), output: num(d.output), cacheRead: num(d.cacheRead), cacheWrite: num(d.cacheWrite) }
      if (p.input === undefined || p.output === undefined || p.cacheRead === undefined || p.cacheWrite === undefined) {
        priceError = `Prices for ${model} must be numbers of 0 or more.`
        return null
      }
      table[model] = { input: p.input, output: p.output, cacheRead: p.cacheRead, cacheWrite: p.cacheWrite }
    }
    return table
  }

  async function savePrices() {
    priceError = ''
    const table = buildPriceTable()
    if (!table) return
    saving = true
    try {
      settings = await adminUpdateBrainSettings({ prices: table })
      toastSuccess('Prices saved')
      pricesOpen = false
    } catch (e) {
      priceError = e instanceof Error ? e.message : String(e)
    } finally {
      saving = false
    }
  }

  type PriceRow = { model: string; input: number | null; output: number | null; cacheRead: number | null; cacheWrite: number | null } & Record<string, unknown>
  const priceRows = $derived<PriceRow[]>(
    Object.entries(settings?.prices ?? {}).map(([model, p]) => ({ model, input: p.input, output: p.output, cacheRead: p.cacheRead, cacheWrite: p.cacheWrite })),
  )
  // Active models the price table does not cover: their requests store no
  // cost and do not count toward dollar budgets.
  const unpricedActive = $derived(
    settings
      ? [...new Set(models.filter((m) => m.is_active && m.provider_active && !isPriced(settings?.prices ?? null, m.name)).map((m) => m.name))].sort()
      : [],
  )
  const money = (v: unknown) => (typeof v === 'number' ? `$${v}` : 'Not set')
  const priceColumns: DataColumn<PriceRow>[] = [
    { key: 'model', label: 'Model', mono: true, truncate: true },
    { key: 'input', label: 'Input', align: 'right', width: '90px', format: money },
    { key: 'output', label: 'Output', align: 'right', width: '90px', format: money },
    { key: 'cacheRead', label: 'Cache read', align: 'right', width: '100px', format: money },
    { key: 'cacheWrite', label: 'Cache write', align: 'right', width: '100px', format: money },
  ]
</script>

<section>
  <SectionHeader title="Model roles" description="Which model answers chat and which answers quick tasks, and how long an answer may be. Budgets are set in Admin > Brain budgets.">
    {#snippet actions()}
      <Button size="sm" loading={saving} disabled={!settings} onclick={() => void saveRoles()}>Save</Button>
    {/snippet}
  </SectionHeader>
  <Panel>
    <FormField layout="row" label="Agent model" for="brain-agent-model" hint="Answers Brain chats that have no model picked. Claude Sonnet 5.5 is a good default; Claude Opus 5.5 for harder work.">
      <Select id="brain-agent-model" options={modelOptions} bind:value={form.agentModelId} />
    </FormField>
    <FormField layout="row" label="Quick model" for="brain-quick-model" hint="Answers Ask AI in the editor. A small, fast model such as Claude Haiku 5.5 keeps it cheap. Empty uses the agent model.">
      <Select id="brain-quick-model" options={modelOptions} bind:value={form.quickModelId} />
    </FormField>
    <FormField layout="row" label="Output token limit" for="brain-max-output" controlWidth="sm" hint="max_tokens for each Anthropic request. Empty uses 16,384 (8,192 for Haiku). Thinking counts toward it.">
      <Input id="brain-max-output" type="number" min="0" max="128000" step="1024" placeholder="Model default" bind:value={form.maxOutputTokens} />
    </FormField>
  </Panel>
</section>

<section>
  <SectionHeader title="Prices" description="US dollars per million tokens. Each request's cost is stored from these prices when it runs; editing them changes new requests only.">
    {#snippet actions()}
      <Button size="sm" variant="outline" disabled={!settings} onclick={openPrices}>Edit prices</Button>
    {/snippet}
  </SectionHeader>
  {#if unpricedActive.length > 0}
    <Panel variant="muted" class="mb-3">
      <p class="text-[13px] font-medium text-fg">{unpricedActive.length} active {unpricedActive.length === 1 ? 'model has' : 'models have'} no price</p>
      <p class="mt-1 text-[13px] leading-relaxed text-fg-3">Their requests show no cost and count toward token budgets only. A dollar budget that blocks unpriced models refuses them.</p>
      <div class="mt-2 flex flex-wrap gap-1.5">
        {#each unpricedActive as m (m)}<Badge tone="warning">{m}</Badge>{/each}
      </div>
    </Panel>
  {/if}
  <DataTable columns={priceColumns} rows={priceRows} rowKey={(r) => r.model} sort={{ key: 'model', dir: 'asc' }} emptyTitle="No prices set" emptyDescription="Costs are not shown until a model has a price.">
    {#snippet cell(row, col, value)}
      {#if col.key !== 'model' && row[col.key] === null}
        <Badge>Not set</Badge>
      {:else}
        {value}
      {/if}
    {/snippet}
  </DataTable>
</section>

<Sheet open={pricesOpen} title="Model prices" description="Per million tokens, in US dollars. A model without input and output prices shows no cost." size="lg" onclose={() => (pricesOpen = false)}>
  <div class="space-y-3">
    <div class="grid grid-cols-[minmax(0,1.6fr)_repeat(4,minmax(0,1fr))_32px] gap-2 px-1 text-[11px] font-medium uppercase tracking-wider text-fg-3">
      <span>Model id</span><span>Input</span><span>Output</span><span>Cache read</span><span>Cache write</span><span></span>
    </div>
    {#each priceDrafts as draft (draft.key)}
      <div class="grid grid-cols-[minmax(0,1.6fr)_repeat(4,minmax(0,1fr))_32px] items-center gap-2">
        <Input size="sm" mono placeholder="claude-sonnet-5-5" bind:value={draft.model} />
        <Input size="sm" type="number" min="0" step="0.01" placeholder="Not set" bind:value={draft.input} />
        <Input size="sm" type="number" min="0" step="0.01" placeholder="Not set" bind:value={draft.output} />
        <Input size="sm" type="number" min="0" step="0.01" placeholder="Not set" bind:value={draft.cacheRead} />
        <Input size="sm" type="number" min="0" step="0.01" placeholder="Not set" bind:value={draft.cacheWrite} />
        <Button icon size="xs" variant="ghost" aria-label="Remove price" title="Remove price" onclick={() => removePriceRow(draft.key)}>
          <Trash2 size={13} />
        </Button>
      </div>
    {/each}
    <div class="flex items-center gap-2">
      <Button size="xs" variant="outline" onclick={addPriceRow}><Plus size={12} /> Add model</Button>
      <Button size="xs" variant="ghost" onclick={() => (confirmResetPrices = true)}>Reset to Claude list prices</Button>
    </div>
    <p class="text-xs text-fg-3">Cache read is input served from the prompt cache; cache write is input stored into it (the 5-minute rate). When a cache price is empty, the input price is used.</p>
    {#if priceError}
      <div class="rounded-md bg-danger-soft p-3"><p class="text-xs font-medium text-danger">{priceError}</p></div>
    {/if}
  </div>
  {#snippet footer()}
    <Button size="sm" variant="ghost" onclick={() => (pricesOpen = false)}>Cancel</Button>
    <Button size="sm" loading={saving} onclick={() => void savePrices()}>Save prices</Button>
  {/snippet}
</Sheet>

<ConfirmDialog
  open={confirmResetPrices}
  title="Reset prices?"
  description="Replace the rows in this editor with the current Claude list prices. Nothing is saved until you click Save prices."
  confirmLabel="Reset"
  onconfirm={resetPrices}
  oncancel={() => (confirmResetPrices = false)}
/>
