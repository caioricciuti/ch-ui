<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { onMount } from 'svelte'
  import type { RbacAssignees, RbacOp, RbacRowPolicy } from '../../../types/rbac'
  import { listDatabases, listTables } from '../../../api/query'
  import Sheet from '../../common/Sheet.svelte'
  import Button from '../../common/Button.svelte'
  import Input from '../../common/Input.svelte'
  import Select from '../../common/Select.svelte'
  import Textarea from '../../common/Textarea.svelte'
  import FormField from '../../common/FormField.svelte'
  import Combobox, { type ComboboxOption } from '../../common/Combobox.svelte'
  import AssigneesField from './AssigneesField.svelte'

  /** Create or change one row policy (FOR SELECT). Staged, never run here. */
  interface Props {
    open: boolean
    policy: RbacRowPolicy | null
    assigneeOptions: ComboboxOption[]
    readOnly: boolean
    canEdit: boolean
    onstage: (ops: RbacOp[]) => void
    onclose: () => void
  }

  let { open, policy, assigneeOptions, readOnly, canEdit, onstage, onclose }: Props = $props()

  const initial = () => ({
    name: policy?.name ?? '',
    database: policy?.database ?? '',
    table: policy?.table ?? '',
    using: policy?.using ?? '',
    restrictive: policy?.restrictive ?? false,
    to: { all: policy?.to.all ?? false, names: [...(policy?.to.names ?? [])], except: [...(policy?.to.except ?? [])] } as RbacAssignees,
  })
  let form = $state(initial())
  let databases = $state<string[]>([])
  let tables = $state<string[]>([])
  const locked = $derived(readOnly || !canEdit)
  const isNew = $derived(policy === null)

  onMount(() => {
    listDatabases().then((d) => (databases = d)).catch(() => (databases = []))
    if (form.database) void loadTables(form.database)
  })

  async function loadTables(db: string) {
    try {
      tables = await listTables(db)
    } catch {
      tables = []
    }
  }

  function pickDatabase(db: string) {
    form.database = db
    form.table = ''
    tables = []
    if (db) void loadTables(db)
  }

  const valid = $derived(!!form.name.trim() && !!form.database && !!form.table && !!form.using.trim())

  function stage() {
    if (!valid) return
    onstage([{
      kind: isNew ? 'create_row_policy' : 'alter_row_policy',
      row_policy: { name: form.name.trim(), database: form.database, table: form.table, using: form.using.trim(), restrictive: form.restrictive, to: form.to },
    }])
    onclose()
  }

  function stageDrop() {
    if (!policy) return
    onstage([{ kind: 'drop_row_policy', row_policy: { name: policy.name, database: policy.database, table: policy.table, using: '', to: {} } }])
    onclose()
  }
</script>

<Sheet
  {open}
  title={isNew ? 'New row policy' : `Row policy ${policy?.name}`}
  description={readOnly ? 'Defined in users.xml, so it is read-only here.' : 'Filters which rows the listed users and roles can SELECT. Staged until you apply the review.'}
  size="lg"
  {onclose}
>
  <form class="space-y-4" onsubmit={(e) => { e.preventDefault(); stage() }}>
    <div class="grid grid-cols-1 gap-3 md:grid-cols-3">
      <FormField label="Name" for="rbac-pol-name" required>
        <Input id="rbac-pol-name" size="sm" bind:value={form.name} disabled={locked || !isNew} spellcheck={false} placeholder="tenant_filter" />
      </FormField>
      <FormField label="Database" for="rbac-pol-db" required>
        <Combobox id="rbac-pol-db" options={databases.map((d) => ({ value: d, label: d }))} value={form.database} disabled={locked || !isNew} onChange={pickDatabase} />
      </FormField>
      <FormField label="Table" for="rbac-pol-table" required>
        <Combobox id="rbac-pol-table" options={tables.map((t) => ({ value: t, label: t }))} value={form.table} disabled={locked || !isNew || !form.database} onChange={(v) => (form.table = v)} />
      </FormField>
    </div>
    <FormField label="Condition" for="rbac-pol-using" required controlWidth="full" hint="A boolean expression over the table's columns, for example tenant = currentUser(). The review checks it against the table.">
      <Textarea id="rbac-pol-using" rows={3} mono bind:value={form.using} disabled={locked} placeholder="tenant_id = 42" />
    </FormField>
    <FormField label="Mode" for="rbac-pol-mode" hint="Permissive policies add visible rows; restrictive ones narrow them further.">
      <Select
        id="rbac-pol-mode"
        size="sm"
        options={[{ value: 'permissive', label: 'Permissive' }, { value: 'restrictive', label: 'Restrictive' }]}
        value={form.restrictive ? 'restrictive' : 'permissive'}
        disabled={locked}
        onchange={(v) => (form.restrictive = v === 'restrictive')}
      />
    </FormField>
    <FormField label="Applies to" for="rbac-pol-to" controlWidth="full">
      <AssigneesField id="rbac-pol-to" value={form.to} options={assigneeOptions} disabled={locked} onchange={(next) => (form.to = next)} />
    </FormField>
  </form>

  {#snippet footer()}
    {#if !isNew && !locked}
      <Button size="sm" variant="ghost" class="mr-auto text-danger hover:text-danger" onclick={stageDrop}>Drop policy</Button>
    {/if}
    <Button size="sm" variant="outline" onclick={onclose}>Cancel</Button>
    <Button size="sm" disabled={locked || !valid} onclick={stage}>{isNew ? 'Stage policy' : 'Stage change'}</Button>
  {/snippet}
</Sheet>
