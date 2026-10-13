<script lang="ts">
  import { onMount } from 'svelte'
  import type { ModelTest, ModelTestInput, TestArgs, TestKind, TestSeverity } from '../../types/models'
  import * as api from '../../api/models'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import Sheet from '../common/Sheet.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import FormField from '../common/FormField.svelte'
  import Input from '../common/Input.svelte'
  import Select, { type SelectOption } from '../common/Select.svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import { Plus, Pencil, Trash2 } from 'lucide-svelte'

  interface Props {
    modelId: string
    /** GitHub-managed models: tests are listed but not edited here. */
    readonly?: boolean
  }

  let { modelId, readonly = false }: Props = $props()

  let tests = $state<ModelTest[]>([])
  let loading = $state(true)

  // Add / edit sheet
  let sheetOpen = $state(false)
  let editingId = $state<string | null>(null)
  let formKind = $state<TestKind>('not_null')
  let formColumn = $state('')
  let formValues = $state('')
  let formTo = $state('')
  let formField = $state('')
  let formExpression = $state('')
  let formSeverity = $state<TestSeverity>('error')
  let formError = $state('')
  let saving = $state(false)

  // Delete confirm
  let pendingDelete = $state<ModelTest | null>(null)
  let deleting = $state(false)

  const KIND_OPTIONS: SelectOption[] = [
    { value: 'not_null', label: 'not_null: the column has no NULLs' },
    { value: 'unique', label: 'unique: no value appears twice' },
    { value: 'accepted_values', label: 'accepted_values: only listed values' },
    { value: 'relationship', label: 'relationship: every value exists in a parent' },
    { value: 'expression', label: 'expression: a condition holds for every row' },
  ]
  const KINDS: TestKind[] = ['not_null', 'unique', 'accepted_values', 'relationship', 'expression']

  const SEVERITY_OPTIONS: SelectOption[] = [
    { value: 'error', label: 'error' },
    { value: 'warn', label: 'warn' },
  ]

  onMount(() => {
    loadTests()
  })

  async function loadTests() {
    loading = true
    try {
      const res = await api.listModelTests(modelId)
      tests = res.tests ?? []
    } catch (e: unknown) {
      toastError((e instanceof Error ? e.message : '') || 'Failed to load tests')
    } finally {
      loading = false
    }
  }

  function parseArgs(raw: string): TestArgs {
    if (!raw) return {}
    try {
      const parsed: unknown = JSON.parse(raw)
      return parsed && typeof parsed === 'object' ? (parsed as TestArgs) : {}
    } catch {
      return {}
    }
  }

  function details(t: ModelTest): string {
    const args = parseArgs(t.args)
    switch (t.kind) {
      case 'accepted_values': return (args.values ?? []).join(', ')
      case 'relationship': return `${args.to ?? ''}.${args.field ?? ''}`
      case 'expression': return args.expression ?? ''
      default: return ''
    }
  }

  function openAdd() {
    editingId = null
    formKind = 'not_null'
    formColumn = ''
    formValues = ''
    formTo = ''
    formField = ''
    formExpression = ''
    formSeverity = 'error'
    formError = ''
    sheetOpen = true
  }

  function openEdit(t: ModelTest) {
    const args = parseArgs(t.args)
    editingId = t.id
    formKind = t.kind
    formColumn = t.column_name
    formValues = (args.values ?? []).join(', ')
    formTo = args.to ?? ''
    formField = args.field ?? ''
    formExpression = args.expression ?? ''
    formSeverity = t.severity
    formError = ''
    sheetOpen = true
  }

  function buildInput(): ModelTestInput {
    const args: TestArgs = {}
    if (formKind === 'accepted_values') {
      args.values = formValues.split(',').map((v) => v.trim()).filter((v) => v !== '')
    } else if (formKind === 'relationship') {
      args.to = formTo.trim()
      args.field = formField.trim()
    } else if (formKind === 'expression') {
      args.expression = formExpression.trim()
    }
    return { kind: formKind, column_name: formColumn.trim(), args, severity: formSeverity }
  }

  async function submit() {
    saving = true
    formError = ''
    try {
      const input = buildInput()
      if (editingId) {
        await api.updateModelTest(modelId, editingId, input)
        toastSuccess('Test saved')
      } else {
        await api.createModelTest(modelId, input)
        toastSuccess('Test added')
      }
      sheetOpen = false
      await loadTests()
    } catch (e: unknown) {
      formError = (e instanceof Error ? e.message : '') || 'Failed to save test'
    } finally {
      saving = false
    }
  }

  async function confirmDelete() {
    if (!pendingDelete) return
    deleting = true
    try {
      await api.deleteModelTest(modelId, pendingDelete.id)
      toastSuccess('Test deleted')
      await loadTests()
    } catch (e: unknown) {
      toastError((e instanceof Error ? e.message : '') || 'Failed to delete test')
    } finally {
      deleting = false
      pendingDelete = null
    }
  }

  type Row = Record<string, unknown>
  const asTest = (row: Row): ModelTest => row as unknown as ModelTest

  const columns: DataColumn<Row>[] = [
    { key: 'kind', label: 'Kind', mono: true },
    { key: 'column_name', label: 'Column', mono: true, format: (v) => String(v || '-') },
    { key: 'args', label: 'Details', mono: true, truncate: true, sortable: false, format: (_v, r) => details(asTest(r)) || '-' },
    { key: 'severity', label: 'Severity' },
  ]
</script>

<div class="flex h-full min-h-0 flex-col">
  <div class="flex h-10 shrink-0 items-center gap-2 border-b border-edge-subtle px-3 text-xs text-fg-3">
    <span class="flex-1">
      Data tests run after every build. error fails the model and skips downstream models; warn only records.
    </span>
    {#if readonly}
      <Badge tone="info">Managed by GitHub</Badge>
    {:else}
      <Button size="xs" variant="outline" onclick={openAdd}>
        <Plus size={12} /> Add test
      </Button>
    {/if}
  </div>
  <div class="min-h-0 flex-1">
    {#if loading}
      <div class="flex h-full items-center justify-center text-[13px] text-fg-3">Loading tests...</div>
    {:else}
      <DataTable
        {columns}
        rows={tests as unknown as Row[]}
        rowKey={(r) => String(r.id)}
        fill
        emptyTitle="No data tests"
        emptyDescription={readonly ? 'Tests for this model are defined in the repository.' : 'Add a test to check this model after every run.'}
        onrowclick={readonly ? undefined : (r) => openEdit(asTest(r))}
      >
        {#snippet cell(row, col, value)}
          {#if col.key === 'severity'}
            {@const t = asTest(row)}
            <Badge tone={t.severity === 'error' ? 'danger' : 'warning'}>{t.severity}</Badge>
          {:else}
            {value}
          {/if}
        {/snippet}
        {#snippet actions(row)}
          {#if !readonly}
            <Button icon size="xs" variant="ghost" aria-label="Edit test" title="Edit" onclick={(e) => { e.stopPropagation(); openEdit(asTest(row)) }}>
              <Pencil size={13} />
            </Button>
            <Button icon size="xs" variant="ghost" aria-label="Delete test" title="Delete" onclick={(e) => { e.stopPropagation(); pendingDelete = asTest(row) }}>
              <Trash2 size={13} />
            </Button>
          {/if}
        {/snippet}
      </DataTable>
    {/if}
  </div>
</div>

<Sheet
  open={sheetOpen}
  title={editingId ? 'Edit test' : 'Add test'}
  description="Checks the model's table after every run."
  size="sm"
  onclose={() => { sheetOpen = false }}
>
  <form id="model-test-form" class="space-y-4" onsubmit={(e) => { e.preventDefault(); submit() }}>
    <FormField label="Kind" for="model-test-kind">
      <Select
        id="model-test-kind"
        value={formKind}
        options={KIND_OPTIONS}
        onchange={(v) => { formKind = KINDS.find((k) => k === v) ?? 'not_null' }}
      />
    </FormField>

    {#if formKind === 'expression'}
      <FormField label="Expression" for="model-test-expr" hint="A boolean that must be true for every row, e.g. amount >= 0" required>
        <Input id="model-test-expr" bind:value={formExpression} mono spellcheck={false} placeholder="amount >= 0" />
      </FormField>
      <FormField label="Column" for="model-test-column" hint="Optional, only used as a label.">
        <Input id="model-test-column" bind:value={formColumn} mono spellcheck={false} />
      </FormField>
    {:else}
      <FormField label="Column" for="model-test-column" required>
        <Input id="model-test-column" bind:value={formColumn} mono spellcheck={false} placeholder="id" />
      </FormField>
    {/if}

    {#if formKind === 'accepted_values'}
      <FormField label="Values" for="model-test-values" hint="Comma-separated, compared as text." required>
        <Input id="model-test-values" bind:value={formValues} mono spellcheck={false} placeholder="active, paused, closed" />
      </FormField>
    {:else if formKind === 'relationship'}
      <FormField label="To" for="model-test-to" hint="A model, a table in the same database, or database.table." required>
        <Input id="model-test-to" bind:value={formTo} mono spellcheck={false} placeholder="customers" />
      </FormField>
      <FormField label="Field" for="model-test-field" hint="The parent's key column." required>
        <Input id="model-test-field" bind:value={formField} mono spellcheck={false} placeholder="id" />
      </FormField>
    {/if}

    <FormField label="Severity" for="model-test-severity" hint="error fails the model and skips downstream models; warn only records.">
      <Select
        id="model-test-severity"
        value={formSeverity}
        options={SEVERITY_OPTIONS}
        onchange={(v) => { formSeverity = v === 'warn' ? 'warn' : 'error' }}
      />
    </FormField>

    {#if formError}
      <p class="text-xs text-danger" role="alert">{formError}</p>
    {/if}
  </form>

  {#snippet footer()}
    <Button size="sm" variant="outline" onclick={() => { sheetOpen = false }}>Cancel</Button>
    <Button size="sm" type="submit" form="model-test-form" loading={saving}>Save</Button>
  {/snippet}
</Sheet>

<ConfirmDialog
  open={!!pendingDelete}
  title="Delete test"
  description={pendingDelete ? `Delete the ${pendingDelete.kind} test${pendingDelete.column_name ? ` on ${pendingDelete.column_name}` : ''}? This cannot be undone.` : ''}
  confirmLabel="Delete"
  destructive
  loading={deleting}
  onconfirm={confirmDelete}
  oncancel={() => { pendingDelete = null }}
/>
