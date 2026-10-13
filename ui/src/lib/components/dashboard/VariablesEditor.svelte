<script lang="ts">
  import type { DashboardVariable, DashboardVariableOption, DashboardVariableType } from '../../types/api'
  import { fetchVariableOptions, saveDashboardVariables } from '../../api/dashboards'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import { isValidVariableName, parseOptionList } from '../../utils/dashboard-variables'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import SectionHeader from '../common/SectionHeader.svelte'
  import Sheet from '../common/Sheet.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import FormField from '../common/FormField.svelte'
  import Input from '../common/Input.svelte'
  import Select from '../common/Select.svelte'
  import Textarea from '../common/Textarea.svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import { Plus, Pencil, Trash2, ArrowUp, ArrowDown } from 'lucide-svelte'

  interface Props {
    dashboardId: string
    variables: DashboardVariable[]
    onsaved: (variables: DashboardVariable[]) => void
  }

  let { dashboardId, variables, onsaved }: Props = $props()

  const TYPE_OPTIONS: { value: DashboardVariableType; label: string }[] = [
    { value: 'query', label: 'Query' },
    { value: 'custom', label: 'Custom list' },
    { value: 'text', label: 'Text box' },
    { value: 'interval', label: 'Interval' },
    { value: 'constant', label: 'Constant (hidden)' },
  ]

  type Row = {
    index: number
    name: string
    label: string
    type: DashboardVariableType
    selection: string
    default: string
  }

  const columns: DataColumn<Row>[] = [
    { key: 'name', label: 'Name', mono: true, sortable: false },
    { key: 'label', label: 'Label', sortable: false },
    { key: 'type', label: 'Type', sortable: false },
    { key: 'selection', label: 'Selection', sortable: false },
    { key: 'default', label: 'Default', mono: true, truncate: true, sortable: false },
  ]

  function selectionOf(v: DashboardVariable): string {
    if (v.type !== 'query' && v.type !== 'custom') return ''
    const parts = [v.multi ? 'Multi' : 'Single']
    if (v.include_all) parts.push('All')
    return parts.join(', ')
  }

  const rows = $derived<Row[]>(
    variables.map((v, index) => ({
      index,
      name: v.name,
      label: v.label ?? '',
      type: v.type,
      selection: selectionOf(v),
      default: (v.default ?? []).join(', '),
    })),
  )

  let busy = $state(false)

  async function persist(next: DashboardVariable[], message: string): Promise<boolean> {
    busy = true
    try {
      const saved = await saveDashboardVariables(dashboardId, next)
      onsaved(saved)
      toastSuccess(message)
      return true
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
      return false
    } finally {
      busy = false
    }
  }

  function move(index: number, dir: -1 | 1) {
    const to = index + dir
    if (busy || to < 0 || to >= variables.length) return
    const next = [...variables]
    ;[next[index], next[to]] = [next[to], next[index]]
    void persist(next, 'Variable order saved')
  }

  // Delete
  let deleteIndex = $state<number | null>(null)

  async function confirmDelete() {
    if (deleteIndex === null) return
    const name = variables[deleteIndex]?.name
    const ok = await persist(variables.filter((_, i) => i !== deleteIndex), `Variable ${name} deleted`)
    if (ok) deleteIndex = null
  }

  // Edit sheet
  interface Form {
    name: string
    label: string
    type: DashboardVariableType
    query: string
    options: string
    multi: boolean
    includeAll: boolean
    defaultText: string
  }

  const emptyForm = (): Form => ({
    name: '', label: '', type: 'query', query: '', options: '', multi: false, includeAll: false, defaultText: '',
  })

  let sheetOpen = $state(false)
  let editIndex = $state<number | null>(null)
  let form = $state<Form>(emptyForm())
  let nameError = $state('')
  let preview = $state<{ options: DashboardVariableOption[]; truncated: boolean } | null>(null)
  let previewing = $state(false)

  const canMulti = $derived(form.type === 'query' || form.type === 'custom')
  const editingName = $derived(editIndex === null ? '' : (variables[editIndex]?.name ?? ''))

  function openNew() {
    editIndex = null
    form = emptyForm()
    nameError = ''
    preview = null
    sheetOpen = true
  }

  function openEdit(index: number) {
    const v = variables[index]
    if (!v) return
    editIndex = index
    form = {
      name: v.name,
      label: v.label ?? '',
      type: v.type,
      query: v.query ?? '',
      options: v.options ?? '',
      multi: !!v.multi,
      includeAll: !!v.include_all,
      defaultText: (v.default ?? []).join(', '),
    }
    nameError = ''
    preview = null
    sheetOpen = true
  }

  function buildDefinition(): DashboardVariable {
    const t = form.type
    const def: DashboardVariable = { name: form.name.trim(), type: t }
    if (form.label.trim()) def.label = form.label.trim()
    if (t === 'query') def.query = form.query.trim()
    if (t === 'custom' || t === 'interval') def.options = form.options.trim()
    if (canMulti && form.multi) def.multi = true
    if (canMulti && form.includeAll) def.include_all = true
    const text = form.defaultText.trim()
    const dflt = canMulti && form.multi ? parseOptionList(text) : text ? [text] : []
    if (dflt.length > 0) def.default = dflt
    return def
  }

  function checkName(name: string): string {
    if (!isValidVariableName(name)) return 'Letters, digits and _, starting with a letter or _ (not __), at most 64 characters'
    if (variables.some((v, i) => v.name === name && i !== editIndex)) return 'Another variable has this name'
    return ''
  }

  async function runPreview() {
    const def = buildDefinition()
    nameError = checkName(def.name)
    if (nameError) return
    previewing = true
    try {
      preview = await fetchVariableOptions(dashboardId, def.name, { definition: def, variables: {} })
    } catch (e: unknown) {
      preview = null
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      previewing = false
    }
  }

  async function submit() {
    const def = buildDefinition()
    nameError = checkName(def.name)
    if (nameError || busy) return
    const next = editIndex === null
      ? [...variables, def]
      : variables.map((v, i) => (i === editIndex ? def : v))
    const ok = await persist(next, editIndex === null ? `Variable ${def.name} added` : `Variable ${def.name} saved`)
    if (ok) sheetOpen = false
  }
</script>

<div class="space-y-3">
  <SectionHeader title="Variables" description="Filters shown above the dashboard. A query variable can use the variables defined before it, so order matters.">
    {#snippet actions()}
      <Button size="sm" onclick={openNew} disabled={busy}><Plus size={14} /> Add variable</Button>
    {/snippet}
  </SectionHeader>

  <p class="text-xs leading-relaxed text-fg-3">
    Reference a variable as <code class="font-mono text-fg-2">$name</code> or <code class="font-mono text-fg-2">${'{'}name{'}'}</code>.
    Values are quoted for you, so write <code class="font-mono text-fg-2">WHERE db IN (${'{'}db{'}'})</code>, not
    <code class="font-mono text-fg-2">'${'{'}db{'}'}'</code>. Use <code class="font-mono text-fg-2">${'{'}name:number{'}'}</code> for bare numbers.
  </p>

  <DataTable
    {columns}
    {rows}
    rowKey={(r) => r.name}
    emptyTitle="No variables"
    emptyDescription="Add one to filter every panel from a bar above the dashboard."
    onrowclick={(r) => openEdit(r.index)}
  >
    {#snippet cell(row, col, value)}
      {#if col.key === 'type'}
        <Badge tone={row.type === 'query' ? 'info' : 'neutral'}>{row.type}</Badge>
      {:else if col.key === 'selection' || col.key === 'default' || col.key === 'label'}
        {#if value}{value}{:else}<span class="text-fg-4">-</span>{/if}
      {:else}
        {value}
      {/if}
    {/snippet}
    {#snippet actions(row)}
      <Button size="xs" variant="ghost" icon aria-label="Move up" title="Move up" disabled={busy || row.index === 0} onclick={(e) => { e.stopPropagation(); move(row.index, -1) }}>
        <ArrowUp size={13} />
      </Button>
      <Button size="xs" variant="ghost" icon aria-label="Move down" title="Move down" disabled={busy || row.index === rows.length - 1} onclick={(e) => { e.stopPropagation(); move(row.index, 1) }}>
        <ArrowDown size={13} />
      </Button>
      <Button size="xs" variant="ghost" icon aria-label="Edit variable" title="Edit" onclick={(e) => { e.stopPropagation(); openEdit(row.index) }}>
        <Pencil size={13} />
      </Button>
      <Button size="xs" variant="ghost" icon aria-label="Delete variable" title="Delete" class="hover:text-danger" disabled={busy} onclick={(e) => { e.stopPropagation(); deleteIndex = row.index }}>
        <Trash2 size={13} />
      </Button>
    {/snippet}
  </DataTable>
</div>

<Sheet
  open={sheetOpen}
  title={editIndex === null ? 'New variable' : `Variable ${editingName}`}
  description="Panels and later variables use it as $name."
  onclose={() => (sheetOpen = false)}
>
  <form id="dashboard-variable-form" class="space-y-4" onsubmit={(e) => { e.preventDefault(); void submit() }}>
    <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
      <FormField
        label="Name"
        for="var-name"
        required
        controlWidth="full"
        error={nameError}
        hint={editIndex !== null ? 'Renaming does not update panel queries.' : undefined}
      >
        <Input id="var-name" bind:value={form.name} mono placeholder="db" autocomplete="off" spellcheck={false} invalid={!!nameError} />
      </FormField>
      <FormField label="Label" for="var-label" controlWidth="full" hint="Shown in the bar; the name when empty.">
        <Input id="var-label" bind:value={form.label} placeholder={form.name || 'Database'} />
      </FormField>
    </div>

    <FormField label="Type" for="var-type" controlWidth="full">
      <Select id="var-type" options={TYPE_OPTIONS} bind:value={form.type} onchange={() => (preview = null)} />
    </FormField>

    {#if form.type === 'query'}
      <FormField label="Query" for="var-query" required controlWidth="full" hint="First column is the value, an optional second column the label.">
        <Textarea id="var-query" bind:value={form.query} mono rows={6} placeholder="SELECT name FROM system.databases ORDER BY name" />
      </FormField>
      <div class="space-y-2">
        <Button size="sm" variant="outline" onclick={runPreview} loading={previewing} disabled={!form.query.trim()}>Preview</Button>
        {#if preview}
          <div class="rounded-md border border-edge-subtle bg-surface p-3">
            <p class="mb-2 text-xs text-fg-3">
              {preview.options.length}{preview.truncated ? '+' : ''} values{preview.options.length > 20 ? ', first 20 shown' : ''}
            </p>
            {#if preview.options.length > 0}
              <div class="flex flex-wrap gap-1">
                {#each preview.options.slice(0, 20) as opt (opt.value)}
                  <Badge title={opt.value}>{opt.label}</Badge>
                {/each}
              </div>
            {/if}
          </div>
        {/if}
      </div>
    {:else if form.type === 'custom' || form.type === 'interval'}
      <FormField
        label="Options"
        for="var-options"
        required
        controlWidth="full"
        hint={form.type === 'interval' ? 'Comma separated, like 1m, 5m, 1h, 1d. Panels get whole seconds.' : 'Comma separated values.'}
      >
        <Input id="var-options" bind:value={form.options} mono placeholder={form.type === 'interval' ? '1m, 5m, 1h' : 'prod, staging'} />
      </FormField>
    {/if}

    {#if canMulti}
      <div class="flex flex-wrap items-center gap-5">
        <label class="ds-checkbox-label">
          <input type="checkbox" class="ds-checkbox" bind:checked={form.multi} />
          Allow several values
        </label>
        <label class="ds-checkbox-label">
          <input type="checkbox" class="ds-checkbox" bind:checked={form.includeAll} />
          Include All
        </label>
      </div>
    {/if}

    <FormField
      label="Default"
      for="var-default"
      required={form.type === 'constant'}
      controlWidth="full"
      hint={canMulti && form.multi
        ? 'Comma separated. Empty means All when included, else the first option.'
        : form.type === 'constant'
          ? 'The value panels get; constants are not shown in the bar.'
          : form.type === 'text'
            ? 'Initial text of the box.'
            : 'Empty means the first option.'}
    >
      <Input id="var-default" bind:value={form.defaultText} mono />
    </FormField>
  </form>

  {#snippet footer()}
    <Button size="sm" variant="outline" onclick={() => (sheetOpen = false)} disabled={busy}>Cancel</Button>
    <Button size="sm" type="submit" form="dashboard-variable-form" loading={busy}>Save</Button>
  {/snippet}
</Sheet>

<ConfirmDialog
  open={deleteIndex !== null}
  title="Delete variable?"
  description={deleteIndex !== null && variables[deleteIndex]
    ? `Panels that use $${variables[deleteIndex].name} will fail until you edit them.`
    : ''}
  confirmLabel="Delete"
  destructive
  loading={busy}
  onconfirm={confirmDelete}
  oncancel={() => (deleteIndex = null)}
/>
