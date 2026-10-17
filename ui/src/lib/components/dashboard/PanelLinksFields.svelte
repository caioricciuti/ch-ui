<script lang="ts">
  import type { Dashboard, DashboardVariable, PanelLink, PanelLinkKind } from '../../types/api'
  import { listDashboards } from '../../api/dashboards'
  import { shareLinkIssue } from '../../utils/drilldown'
  import Combobox from '../common/Combobox.svelte'
  import type { ComboboxOption } from '../common/Combobox.svelte'
  import { Plus, X } from 'lucide-svelte'

  interface Props {
    links: PanelLink[]
    /** Variables of the dashboard the panel is on. */
    variables: DashboardVariable[]
    onchange: (links: PanelLink[]) => void
  }

  let { links, variables, onchange }: Props = $props()

  const MAX_LINKS = 10
  const KINDS: { value: PanelLinkKind; label: string }[] = [
    { value: 'setVariable', label: 'Filter' },
    { value: 'dashboard', label: 'Dashboard' },
    { value: 'sql', label: 'SQL' },
    { value: 'url', label: 'URL' },
  ]

  const uid = Math.random().toString(36).slice(2, 8)

  // Dashboards to link to, loaded once the section is shown.
  let dashboards = $state<Dashboard[]>([])
  let dashboardsLoaded = $state(false)
  let dashboardsRequested = false
  $effect(() => {
    if (dashboardsRequested || !links.some(l => l.kind === 'dashboard')) return
    dashboardsRequested = true
    listDashboards()
      .then((d) => (dashboards = d))
      .catch(() => {})
      .finally(() => (dashboardsLoaded = true))
  })

  const variableOptions = $derived<ComboboxOption[]>(
    variables
      .filter(v => v.type !== 'constant')
      .map(v => ({ value: v.name, label: v.label?.trim() || v.name, hint: v.type })),
  )
  const dashboardOptions = $derived<ComboboxOption[]>(dashboards.map(d => ({ value: d.id, label: d.name })))

  function update(i: number, patch: Partial<PanelLink>) {
    onchange(links.map((l, j) => (j === i ? { ...l, ...patch } : l)))
  }

  function setKind(i: number, kind: PanelLinkKind) {
    const { label } = links[i]
    onchange(links.map((l, j) => (j === i ? { kind, ...(label ? { label } : {}) } : l)))
  }

  function add() {
    if (links.length >= MAX_LINKS) return
    onchange([...links, { kind: variableOptions.length > 0 ? 'setVariable' : 'url' }])
  }

  function remove(i: number) {
    onchange(links.filter((_, j) => j !== i))
  }

  function paramRows(link: PanelLink): [string, string][] {
    return Object.entries(link.params ?? {})
  }

  function setParams(i: number, rows: [string, string][]) {
    update(i, { params: rows.length > 0 ? Object.fromEntries(rows) : undefined })
  }

  function urlProblem(template: string | undefined): string | null {
    const t = (template ?? '').toLowerCase()
    if (!t) return null
    return t.startsWith('http://') || t.startsWith('https://') ? null : 'Must start with http:// or https://'
  }

  const choiceClass = (active: boolean) =>
    active ? 'border-accent bg-accent-soft text-accent' : 'border-edge-subtle text-fg-3 hover:border-edge hover:text-fg'
  const inputClass = 'w-full bg-transparent border border-edge rounded px-2 py-1.5 text-fg'
</script>

<div>
  <div class="mb-1 flex items-center justify-between">
    <span class="text-xs font-medium text-fg-2">Links</span>
    {#if links.length < MAX_LINKS}
      <button type="button" class="flex items-center gap-0.5 text-[11px] text-accent transition-colors hover:underline" onclick={add}>
        <Plus size={12} /> Add
      </button>
    {/if}
  </div>
  <p class="mb-2 text-xs text-fg-3">
    What a click on a data point does. Templates take <code class="text-fg-2">{'${__value}'}</code>,
    <code class="text-fg-2">{'${__field}'}</code>, <code class="text-fg-2">{'${__series}'}</code>,
    <code class="text-fg-2">{'${__row.column}'}</code> and <code class="text-fg-2">{'${variable}'}</code>.
  </p>

  <div class="flex flex-col gap-2">
    {#each links as link, i (i)}
      {@const shareIssue = shareLinkIssue(link, variables)}
      <div class="flex flex-col gap-2 rounded-md border border-edge-subtle p-2">
        <div class="flex items-center gap-2">
          <div class="grid flex-1 grid-cols-4 gap-1" role="group" aria-label="Link kind">
            {#each KINDS as k (k.value)}
              <button
                type="button"
                class="rounded border py-1 text-[11px] font-medium transition-colors {choiceClass(link.kind === k.value)}"
                aria-pressed={link.kind === k.value}
                onclick={() => link.kind !== k.value && setKind(i, k.value)}
              >
                {k.label}
              </button>
            {/each}
          </div>
          <button
            type="button"
            class="rounded p-0.5 text-fg-4 transition-colors hover:text-danger"
            onclick={() => remove(i)}
            aria-label="Remove link"
          >
            <X size={14} />
          </button>
        </div>

        <div>
          <label for="link-label-{uid}-{i}" class="mb-1 block text-xs font-medium text-fg-2">Label</label>
          <input
            id="link-label-{uid}-{i}"
            type="text"
            maxlength="100"
            class={inputClass}
            placeholder="Shown when a panel has several links"
            value={link.label ?? ''}
            oninput={(e) => update(i, { label: e.currentTarget.value || undefined })}
          />
        </div>

        {#if link.kind === 'setVariable'}
          <div>
            <span class="mb-1 block text-xs font-medium text-fg-2">Variable</span>
            {#if variableOptions.length === 0}
              <p class="text-xs text-fg-4">This dashboard has no variables. Add one in the dashboard settings.</p>
            {:else}
              <Combobox
                options={variableOptions}
                value={link.variable ?? ''}
                onChange={(v) => update(i, { variable: v || undefined })}
                placeholder="Select variable..."
              />
            {/if}
          </div>
          <div>
            <label for="link-value-{uid}-{i}" class="mb-1 block text-xs font-medium text-fg-2">Value</label>
            <input
              id="link-value-{uid}-{i}"
              type="text"
              class="{inputClass} font-mono"
              placeholder={'${__value}'}
              value={link.template ?? ''}
              oninput={(e) => update(i, { template: e.currentTarget.value || undefined })}
            />
          </div>
        {:else if link.kind === 'dashboard'}
          <div>
            <span class="mb-1 block text-xs font-medium text-fg-2">Dashboard</span>
            <Combobox
              options={dashboardOptions}
              value={link.dashboardId ?? ''}
              onChange={(v) => update(i, { dashboardId: v || undefined })}
              placeholder={dashboardsLoaded ? 'Select dashboard...' : 'Loading dashboards...'}
            />
            {#if link.dashboardId && dashboardsLoaded && !dashboards.some(d => d.id === link.dashboardId)}
              <p class="mt-1 text-xs text-danger">That dashboard no longer exists</p>
            {/if}
          </div>
          <label class="flex cursor-pointer items-center gap-2 text-xs text-fg-2">
            <input
              type="checkbox"
              class="ds-checkbox ds-checkbox-sm"
              checked={link.carryVariables ?? false}
              onchange={(e) => update(i, { carryVariables: e.currentTarget.checked || undefined })}
            />
            Pass the current variable values along
          </label>
          <div>
            <div class="mb-1 flex items-center justify-between">
              <span class="text-xs font-medium text-fg-2">Set variables</span>
              <!-- One unnamed row at a time: params are keyed by name. -->
              <button
                type="button"
                class="flex items-center gap-0.5 text-[11px] text-accent transition-colors hover:underline disabled:opacity-50 disabled:no-underline"
                disabled={paramRows(link).some(([name]) => name === '')}
                onclick={() => setParams(i, [...paramRows(link), ['', '${__value}']])}
              >
                <Plus size={12} /> Add
              </button>
            </div>
            {#each paramRows(link) as [name, tpl], p (p)}
              <div class="mb-1 flex items-center gap-1.5">
                <input
                  type="text"
                  class="{inputClass} w-2/5"
                  placeholder="variable"
                  aria-label="Variable name"
                  value={name}
                  oninput={(e) => {
                    const rows = paramRows(link)
                    rows[p] = [e.currentTarget.value, tpl]
                    setParams(i, rows)
                  }}
                />
                <input
                  type="text"
                  class="{inputClass} flex-1 font-mono"
                  placeholder={'${__value}'}
                  aria-label="Value"
                  value={tpl}
                  oninput={(e) => {
                    const rows = paramRows(link)
                    rows[p] = [name, e.currentTarget.value]
                    setParams(i, rows)
                  }}
                />
                <button
                  type="button"
                  class="rounded p-0.5 text-fg-4 transition-colors hover:text-danger"
                  onclick={() => setParams(i, paramRows(link).filter((_, q) => q !== p))}
                  aria-label="Remove variable"
                >
                  <X size={14} />
                </button>
              </div>
            {/each}
          </div>
        {:else if link.kind === 'sql'}
          <div>
            <label for="link-sql-{uid}-{i}" class="mb-1 block text-xs font-medium text-fg-2">SQL</label>
            <textarea
              id="link-sql-{uid}-{i}"
              rows="4"
              class="{inputClass} resize-y font-mono"
              placeholder={'SELECT * FROM logs WHERE host = ${__value}'}
              value={link.template ?? ''}
              oninput={(e) => update(i, { template: e.currentTarget.value || undefined })}
            ></textarea>
            <p class="mt-1 text-xs text-fg-3">
              Opens in a new query tab without running. Values are quoted; <code class="text-fg-2">{'${__value:raw}'}</code> inserts the value as is.
            </p>
          </div>
        {:else}
          {@const problem = urlProblem(link.template)}
          <div>
            <label for="link-url-{uid}-{i}" class="mb-1 block text-xs font-medium text-fg-2">URL</label>
            <input
              id="link-url-{uid}-{i}"
              type="text"
              class="{inputClass} font-mono {problem ? 'border-danger' : ''}"
              placeholder={'https://example.com/hosts/${__value}'}
              aria-invalid={problem ? true : undefined}
              value={link.template ?? ''}
              oninput={(e) => update(i, { template: e.currentTarget.value || undefined })}
            />
            {#if problem}
              <p class="mt-1 text-xs text-danger">{problem}</p>
            {:else}
              <p class="mt-1 text-xs text-fg-3">Opens in a new browser tab. Values are URL encoded.</p>
            {/if}
          </div>
        {/if}

        {#if shareIssue}
          <p class="text-xs text-fg-4">{shareIssue}</p>
        {/if}
      </div>
    {/each}
  </div>
</div>
