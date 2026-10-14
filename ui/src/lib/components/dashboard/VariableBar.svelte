<script lang="ts">
  import { tick, untrack } from 'svelte'
  import { ChevronDown, Search } from 'lucide-svelte'
  import type { DashboardVariable, DashboardVariableOption } from '../../types/api'
  import { VARIABLE_ALL } from '../../api/dashboards'
  import {
    dependsOn, fallbackValue, resolveSelection, sameValues, staticOptions, variableLabel,
    type VariableOptionsLoader, type VariableValues,
  } from '../../utils/dashboard-variables'
  import Combobox from '../common/Combobox.svelte'
  import Select from '../common/Select.svelte'
  import Input from '../common/Input.svelte'
  import Spinner from '../common/Spinner.svelte'

  interface Props {
    variables: DashboardVariable[]
    values: VariableValues
    /** Options of a query variable; custom and interval lists are read locally. */
    loadOptions: VariableOptionsLoader
    onchange: (values: VariableValues) => void
    /** Share links only accept the default of a text variable. */
    lockText?: boolean
  }

  let { variables, values, loadOptions, onchange, lockText = false }: Props = $props()

  interface OptionState {
    options: DashboardVariableOption[]
    truncated: boolean
    loading: boolean
    error?: string
  }

  let optionState = $state<Record<string, OptionState>>({})
  // Values being worked out while dependents reload; the bar shows these and
  // a second change builds on them, so no change is lost.
  let working = $state<VariableValues | null>(null)
  // A full load (new definitions) and a user change are guarded apart, so a
  // change during the first load does not stop the other variables loading.
  let allSeq = 0
  let commitSeq = 0

  const shown = $derived(variables.filter((v) => v.type !== 'constant'))
  const current = $derived(working ?? values)

  function errorMessage(e: unknown): string {
    return e instanceof Error ? e.message : String(e)
  }

  function setState(name: string, patch: Partial<OptionState>) {
    const prev = optionState[name] ?? { options: [], truncated: false, loading: false }
    optionState = { ...optionState, [name]: { ...prev, ...patch } }
  }

  /**
   * Load the options of one variable and settle its value against them.
   * Returns the values to continue with, or null when a newer load started.
   */
  async function settle(
    v: DashboardVariable,
    next: VariableValues,
    isCurrent: () => boolean,
    force: boolean,
  ): Promise<VariableValues | null> {
    let options = staticOptions(v)
    let truncated = false
    if (!options) {
      setState(v.name, { loading: true, error: undefined })
      try {
        const res = await loadOptions(v.name, next)
        if (!isCurrent()) return null
        options = res.options
        truncated = res.truncated
        setState(v.name, { options, truncated, loading: false })
      } catch (e: unknown) {
        if (!isCurrent()) return null
        setState(v.name, { loading: false, error: errorMessage(e) })
        return next
      }
    } else {
      setState(v.name, { options, truncated: false, loading: false, error: undefined })
    }
    // An unset variable stays unset on first load: the server falls back to
    // the same value the bar shows.
    if (!force && !next[v.name]) return next
    const resolved = resolveSelection(v, next[v.name], options, truncated)
    if (sameValues(resolved, next[v.name])) return next
    return { ...next, [v.name]: resolved }
  }

  async function loadAll(vars: DashboardVariable[]) {
    const seq = ++allSeq
    const isCurrent = () => seq === allSeq
    commitSeq++
    working = null
    const start = values
    let next = start
    for (const v of vars) {
      if (v.type === 'constant' || v.type === 'text') continue
      const settled = await settle(v, next, isCurrent, false)
      if (!settled) return
      next = settled
    }
    // Report only the values this load corrected, on top of whatever the
    // user picked meanwhile; a change still settling reports its own.
    const fixed = Object.fromEntries(
      Object.entries(next).filter(([k, val]) => !sameValues(val, start[k]) && sameValues(values[k], start[k])),
    )
    if (Object.keys(fixed).length > 0 && working === null) onchange({ ...values, ...fixed })
  }

  $effect(() => {
    const vars = variables
    untrack(() => void loadAll(vars))
  })

  /** A user change: set it, reload the variables that depend on it, then report once. */
  async function commit(name: string, selected: string[]) {
    const base = working ?? values
    if (sameValues(base[name], selected)) return
    const seq = ++commitSeq
    const isCurrent = () => seq === commitSeq
    let next: VariableValues = { ...base, [name]: selected }
    working = next
    const changed = new Set([name])
    const idx = variables.findIndex((v) => v.name === name)
    for (const d of variables.slice(idx + 1)) {
      if (!dependsOn(d, changed)) continue
      const settled = await settle(d, next, isCurrent, true)
      if (!settled) return
      if (!sameValues(settled[d.name], next[d.name])) changed.add(d.name)
      next = settled
      working = next
    }
    working = null
    onchange(next)
  }

  /** Selected values to show: the set ones, else what the server falls back to. */
  function selectedOf(v: DashboardVariable): string[] {
    const set = current[v.name]
    if (set && set.length > 0) return set
    return fallbackValue(v, optionState[v.name]?.options ?? [])
  }

  /** Options plus any selected value the list does not hold, so it still shows. */
  function choicesOf(v: DashboardVariable): DashboardVariableOption[] {
    const options = optionState[v.name]?.options ?? staticOptions(v) ?? []
    const known = new Set(options.map((o) => o.value))
    const extra = selectedOf(v)
      .filter((s) => s !== VARIABLE_ALL && !known.has(s))
      .map((s) => ({ value: s, label: s }))
    return [...extra, ...options]
  }

  function comboOptions(v: DashboardVariable) {
    const list = choicesOf(v).map((o) => ({ value: o.value, label: o.label, hint: o.label !== o.value ? o.value : undefined }))
    return v.include_all ? [{ value: VARIABLE_ALL, label: 'All' }, ...list] : list
  }

  function summary(v: DashboardVariable): string {
    const sel = selectedOf(v)
    if (sel.includes(VARIABLE_ALL)) return 'All'
    if (sel.length === 0) return 'None'
    const labels = new Map(choicesOf(v).map((o) => [o.value, o.label]))
    const first = labels.get(sel[0]) ?? sel[0]
    return sel.length > 1 ? `${first} +${sel.length - 1}` : first
  }

  // Multi-select popover: one open at a time; the draft is applied on close,
  // so ticking several values reruns the panels once.
  let multiOpen = $state<string | null>(null)
  let draft = $state<string[]>([])
  let multiQuery = $state('')
  let multiSearch = $state<HTMLInputElement>()

  async function openMulti(v: DashboardVariable) {
    if (multiOpen === v.name) {
      closeMulti()
      return
    }
    closeMulti()
    multiOpen = v.name
    draft = [...selectedOf(v)]
    multiQuery = ''
    await tick()
    multiSearch?.focus()
  }

  function closeMulti() {
    const name = multiOpen
    if (!name) return
    multiOpen = null
    if (draft.length > 0) void commit(name, draft)
  }

  function toggleDraft(value: string) {
    if (value === VARIABLE_ALL) {
      draft = draft.includes(VARIABLE_ALL) ? [] : [VARIABLE_ALL]
      return
    }
    const base = draft.filter((d) => d !== VARIABLE_ALL)
    draft = base.includes(value) ? base.filter((d) => d !== value) : [...base, value]
  }

  function filteredChoices(v: DashboardVariable): DashboardVariableOption[] {
    const term = multiQuery.trim().toLowerCase()
    const list = choicesOf(v)
    if (!term) return list
    return list.filter((o) => `${o.label} ${o.value}`.toLowerCase().includes(term))
  }

  function onWindowPointerDown(e: PointerEvent) {
    if (!multiOpen) return
    const target = e.target as Element | null
    if (target?.closest?.(`[data-var-multi="${CSS.escape(multiOpen)}"]`)) return
    closeMulti()
  }

  function onWindowKeydown(e: KeyboardEvent) {
    if (multiOpen && e.key === 'Escape') {
      e.preventDefault()
      closeMulti()
    }
  }
</script>

<svelte:window onpointerdown={onWindowPointerDown} onkeydown={onWindowKeydown} />

{#if shown.length > 0}
  <div class="flex shrink-0 flex-wrap items-end gap-x-4 gap-y-2 border-b border-edge-subtle px-5 py-2">
    {#each shown as v (v.name)}
      {@const st = optionState[v.name]}
      {@const id = `dash-var-${v.name}`}
      <div class="flex min-w-0 flex-col gap-1">
        <label for={id} class="flex items-center gap-1.5 text-xs text-fg-3">
          {variableLabel(v)}
          {#if st?.loading}<Spinner size="sm" />{/if}
        </label>
        {#if v.type === 'text'}
          <div class="w-44">
            <Input
              {id}
              value={selectedOf(v)[0] ?? ''}
              disabled={lockText}
              onchange={(e) => void commit(v.name, [e.currentTarget.value])}
              onkeydown={(e) => { if (e.key === 'Enter') (e.currentTarget as HTMLInputElement).blur() }}
            />
          </div>
        {:else if v.type === 'interval'}
          <Select
            {id}
            class="w-28"
            value={selectedOf(v)[0] ?? ''}
            options={choicesOf(v)}
            onchange={(value) => void commit(v.name, [value])}
          />
        {:else if v.multi}
          <div class="relative w-44" data-var-multi={v.name}>
            <button
              {id}
              type="button"
              class="ds-input flex items-center gap-2 text-left"
              aria-haspopup="listbox"
              aria-expanded={multiOpen === v.name}
              onclick={() => void openMulti(v)}
            >
              <span class="flex-1 truncate">{summary(v)}</span>
              <ChevronDown size={14} class="text-fg-3" />
            </button>
            {#if multiOpen === v.name}
              <div class="surface-card absolute z-[66] mt-1 w-full min-w-[220px] overflow-hidden rounded-md">
                <div class="flex items-center gap-2 border-b border-edge-subtle px-2.5 py-2">
                  <Search size={13} class="text-fg-3" />
                  <input
                    bind:this={multiSearch}
                    bind:value={multiQuery}
                    class="w-full bg-transparent text-[13px] text-fg outline-none placeholder:text-fg-4"
                    type="text"
                    placeholder="Type to search"
                    aria-label="Search options"
                  />
                </div>
                <div class="max-h-56 overflow-y-auto p-1.5" role="listbox" aria-multiselectable="true" aria-label={variableLabel(v)}>
                  {#if v.include_all && !multiQuery.trim()}
                    <label class="ds-checkbox-label w-full cursor-pointer rounded-sm px-2.5 py-1.5 hover:bg-hover">
                      <input type="checkbox" class="ds-checkbox ds-checkbox-sm" checked={draft.includes(VARIABLE_ALL)} onchange={() => toggleDraft(VARIABLE_ALL)} />
                      All
                    </label>
                  {/if}
                  {#each filteredChoices(v) as opt (opt.value)}
                    <label class="ds-checkbox-label w-full cursor-pointer rounded-sm px-2.5 py-1.5 hover:bg-hover" title={opt.value}>
                      <input type="checkbox" class="ds-checkbox ds-checkbox-sm" checked={draft.includes(opt.value)} onchange={() => toggleDraft(opt.value)} />
                      <span class="truncate">{opt.label}</span>
                    </label>
                  {:else}
                    <div class="px-2.5 py-6 text-center text-xs text-fg-3">No matches</div>
                  {/each}
                </div>
                {#if st?.truncated}
                  <p class="border-t border-edge-subtle px-2.5 py-1.5 text-xs text-fg-3">List cut at the first {st.options.length} values{v.include_all ? '. All still covers every value' : ''}</p>
                {/if}
              </div>
            {/if}
          </div>
        {:else}
          <div class="w-44">
            <Combobox
              {id}
              options={comboOptions(v)}
              value={selectedOf(v)[0] ?? ''}
              placeholder={st?.loading ? 'Loading...' : 'Select...'}
              onChange={(value) => void commit(v.name, [value])}
            />
          </div>
        {/if}
        {#if st?.error}
          <p class="max-w-44 truncate text-xs text-danger" title={st.error}>{st.error}</p>
        {/if}
      </div>
    {/each}
  </div>
{/if}
