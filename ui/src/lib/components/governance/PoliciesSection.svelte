<script lang="ts">
  import { onMount } from 'svelte'
  import type { Policy, PolicyViolation } from '../../types/governance'
  import {
    fetchPolicies,
    createPolicy,
    updatePolicy,
    deletePolicy,
    fetchViolations,
    promoteViolationToIncident,
    fetchAccessRoles,
  } from '../../api/governance'
  import { listDatabases, listTables, listColumns } from '../../api/query'
  import { formatDate } from '../../utils/format'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import Tabs from '../common/Tabs.svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import Sheet from '../common/Sheet.svelte'
  import FormField from '../common/FormField.svelte'
  import Input from '../common/Input.svelte'
  import Textarea from '../common/Textarea.svelte'
  import Select from '../common/Select.svelte'
  import Combobox, { type ComboboxOption } from '../common/Combobox.svelte'
  import Panel from '../common/Panel.svelte'
  import SectionHeader from '../common/SectionHeader.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import Spinner from '../common/Spinner.svelte'
  import { Plus, Pencil, Trash2, Shield, AlertTriangle, MessageSquare } from 'lucide-svelte'

  // Policies: who may touch which objects. The syncer records a violation
  // when a query breaks one; block mode refuses the query before it runs.
  type Row = Record<string, unknown>
  type BadgeTone = 'neutral' | 'success' | 'warning' | 'danger' | 'info' | 'brand'
  type View = 'policies' | 'violations'

  const ANY = ''

  let view = $state<View>('policies')
  let policies = $state<Policy[]>([])
  let violations = $state<PolicyViolation[]>([])
  let loading = $state(true)

  let sheetOpen = $state(false)
  let editingPolicy = $state<Policy | null>(null)
  let saving = $state(false)
  let policyForm = $state({
    name: '',
    description: '',
    object_type: 'table' as Policy['object_type'],
    object_database: '',
    object_table: '',
    object_column: '',
    required_role: '',
    severity: 'warn',
    enforcement_mode: 'warn' as Policy['enforcement_mode'],
    enabled: true,
  })

  let confirmDeleteOpen = $state(false)
  let pendingDelete = $state<Policy | null>(null)
  let deleting = $state(false)

  // Schema names for the scope pickers, loaded lazily the first time the
  // sheet opens (databases) and whenever the parent selection changes.
  let databases = $state<string[]>([])
  let tables = $state<string[]>([])
  let columns = $state<string[]>([])
  let roles = $state<string[]>([])
  let schemaLoaded = $state(false)

  onMount(() => {
    void load()
  })

  async function load() {
    loading = true
    try {
      const [p, v] = await Promise.all([fetchPolicies(), fetchViolations()])
      policies = p?.policies ?? []
      violations = v?.violations ?? []
    } catch (err: any) {
      toastError('Failed to load policies: ' + err.message)
    } finally {
      loading = false
    }
  }

  async function loadSchemaRoots() {
    if (schemaLoaded) return
    schemaLoaded = true
    const [dbs, rolesRes] = await Promise.allSettled([listDatabases(), fetchAccessRoles()])
    if (dbs.status === 'fulfilled') databases = dbs.value
    if (rolesRes.status === 'fulfilled') roles = (rolesRes.value?.roles ?? []).map((r) => r.name)
  }

  async function loadTables(db: string) {
    tables = []
    columns = []
    if (!db) return
    try {
      tables = await listTables(db)
    } catch {
      tables = []
    }
  }

  async function loadColumns(db: string, table: string) {
    columns = []
    if (!db || !table) return
    try {
      columns = (await listColumns(db, table)).map((c) => c.name)
    } catch {
      columns = []
    }
  }

  // ── Options ──────────────────────────────────────────────────────────

  const objectTypeOptions = [
    { value: 'database', label: 'Database' },
    { value: 'table', label: 'Table' },
    { value: 'column', label: 'Column' },
  ]
  const severityOptions = [
    { value: 'info', label: 'Info' },
    { value: 'warn', label: 'Warning' },
    { value: 'critical', label: 'Critical' },
  ]
  const modeOptions = [
    { value: 'warn', label: 'Warn: run the query, record a violation' },
    { value: 'block', label: 'Block: refuse the query before it runs' },
  ]

  /** "Any" first, then the loaded names, plus the current value if it is not in the list (editing an old policy). */
  function scopeOptions(names: string[], current: string, anyLabel: string): ComboboxOption[] {
    const list: ComboboxOption[] = [{ value: ANY, label: anyLabel }]
    const seen = new Set<string>()
    for (const n of names) {
      seen.add(n)
      list.push({ value: n, label: n })
    }
    if (current && !seen.has(current)) list.push({ value: current, label: current, hint: 'not in the current schema' })
    return list
  }

  const databaseOptions = $derived(scopeOptions(databases, policyForm.object_database, 'Any database (*)'))
  const tableOptions = $derived(scopeOptions(tables, policyForm.object_table, 'Any table (*)'))
  const columnOptions = $derived(scopeOptions(columns, policyForm.object_column, 'Any column (*)'))
  const roleOptions = $derived.by<ComboboxOption[]>(() => {
    const list: ComboboxOption[] = roles.map((r) => ({ value: r, label: r }))
    if (policyForm.required_role && !roles.includes(policyForm.required_role)) {
      list.push({ value: policyForm.required_role, label: policyForm.required_role, hint: 'not a known ClickHouse role' })
    }
    return list
  })

  const scopeLabel = $derived.by(() => {
    const db = policyForm.object_database || '*'
    if (policyForm.object_type === 'database') return db
    const table = policyForm.object_table || '*'
    if (policyForm.object_type === 'table') return `${db}.${table}`
    return `${db}.${table}.${policyForm.object_column || '*'}`
  })

  const summary = $derived.by(() => {
    const role = policyForm.required_role.trim()
    const who = role ? `Users without the role ${role}` : 'Users without the required role'
    const what = `touching the ${policyForm.object_type} ${scopeLabel}`
    const sev = severityOptions.find((s) => s.value === policyForm.severity)?.label.toLowerCase() ?? policyForm.severity
    const then =
      policyForm.enforcement_mode === 'block'
        ? `will have the query refused before it runs and a ${sev} violation recorded.`
        : `will have the query run and a ${sev} violation recorded by the syncer.`
    const state = policyForm.enabled ? '' : ' The policy is disabled, so nothing happens until it is enabled.'
    return `${who} ${what} ${then}${state}`
  })

  // ── Sheet ────────────────────────────────────────────────────────────

  async function openPolicyForm(policy?: Policy) {
    editingPolicy = policy ?? null
    policyForm = policy
      ? {
          name: policy.name,
          description: policy.description ?? '',
          object_type: policy.object_type,
          object_database: policy.object_database ?? '',
          object_table: policy.object_table ?? '',
          object_column: policy.object_column ?? '',
          required_role: policy.required_role,
          severity: policy.severity,
          enforcement_mode: policy.enforcement_mode ?? 'warn',
          enabled: policy.enabled,
        }
      : {
          name: '',
          description: '',
          object_type: 'table',
          object_database: '',
          object_table: '',
          object_column: '',
          required_role: '',
          severity: 'warn',
          enforcement_mode: 'warn',
          enabled: true,
        }
    sheetOpen = true
    await loadSchemaRoots()
    if (policyForm.object_database) await loadTables(policyForm.object_database)
    if (policyForm.object_database && policyForm.object_table) await loadColumns(policyForm.object_database, policyForm.object_table)
  }

  function closePolicyForm() {
    sheetOpen = false
    editingPolicy = null
  }

  function setDatabase(db: string) {
    policyForm.object_database = db
    policyForm.object_table = ''
    policyForm.object_column = ''
    void loadTables(db)
  }

  function setTable(table: string) {
    policyForm.object_table = table
    policyForm.object_column = ''
    void loadColumns(policyForm.object_database, table)
  }

  function setObjectType(type: string) {
    policyForm.object_type = type as Policy['object_type']
    if (type === 'database') {
      policyForm.object_table = ''
      policyForm.object_column = ''
    } else if (type === 'table') {
      policyForm.object_column = ''
    }
  }

  async function submitPolicy() {
    if (!policyForm.name.trim()) {
      toastError('Policy name is required')
      return
    }
    if (!policyForm.required_role.trim()) {
      toastError('Required role is required')
      return
    }
    saving = true
    try {
      const payload: Partial<Policy> = {
        name: policyForm.name.trim(),
        description: policyForm.description.trim() || null,
        object_type: policyForm.object_type,
        object_database: policyForm.object_database || null,
        object_table: policyForm.object_type === 'database' ? null : policyForm.object_table || null,
        object_column: policyForm.object_type === 'column' ? policyForm.object_column || null : null,
        required_role: policyForm.required_role.trim(),
        severity: policyForm.severity,
        enforcement_mode: policyForm.enforcement_mode,
        enabled: policyForm.enabled,
      }
      if (editingPolicy) {
        await updatePolicy(editingPolicy.id, payload)
        toastSuccess('Policy updated')
      } else {
        await createPolicy(payload)
        toastSuccess('Policy created')
      }
      closePolicyForm()
      await load()
    } catch (err: any) {
      toastError('Failed to save policy: ' + err.message)
    } finally {
      saving = false
    }
  }

  function requestDelete(policy: Policy) {
    pendingDelete = policy
    confirmDeleteOpen = true
  }

  function cancelDelete() {
    confirmDeleteOpen = false
    pendingDelete = null
  }

  async function confirmDelete() {
    if (!pendingDelete) return
    deleting = true
    try {
      await deletePolicy(pendingDelete.id)
      toastSuccess('Policy deleted')
      cancelDelete()
      await load()
    } catch (err: any) {
      toastError('Failed to delete policy: ' + err.message)
    } finally {
      deleting = false
    }
  }

  async function createIncidentFromViolation(violation: PolicyViolation) {
    try {
      const res = await promoteViolationToIncident(violation.id)
      toastSuccess(res?.created ? 'Incident created from violation' : 'Existing incident updated')
    } catch (err: any) {
      toastError('Failed to promote violation: ' + err.message)
    }
  }

  // ── Tables ───────────────────────────────────────────────────────────

  function policyScope(policy: Policy): string {
    const db = policy.object_database || '*'
    if (policy.object_type === 'database') return db
    const table = policy.object_table || '*'
    if (policy.object_type === 'table') return `${db}.${table}`
    return `${db}.${table}.${policy.object_column || '*'}`
  }

  function severityTone(severity: string | null | undefined): BadgeTone {
    if (severity === 'critical' || severity === 'error') return 'danger'
    if (severity === 'warn') return 'warning'
    if (severity === 'info') return 'info'
    return 'neutral'
  }

  const policyColumns: DataColumn<Row>[] = [
    { key: 'name', label: 'Policy', width: '30%' },
    { key: 'scope', label: 'Scope', mono: true, format: (_v, row) => policyScope(row as unknown as Policy), sortValue: (row) => policyScope(row as unknown as Policy) },
    { key: 'required_role', label: 'Required role', format: (v) => (v ? String(v) : '—') },
    { key: 'enforcement_mode', label: 'Mode', width: '90px' },
    { key: 'severity', label: 'Severity', width: '100px' },
    { key: 'enabled', label: 'Status', width: '100px', format: (v) => (v ? 'enabled' : 'disabled') },
    { key: 'updated_at', label: 'Updated', width: '160px', format: (v) => formatDate(v) },
  ]

  const violationColumns: DataColumn<Row>[] = [
    { key: 'policy_name', label: 'Policy', width: '22%' },
    { key: 'severity', label: 'Severity', width: '100px' },
    { key: 'ch_user', label: 'User', mono: true, width: '140px' },
    { key: 'violation_detail', label: 'Detail', truncate: true, sortable: false },
    { key: 'detected_at', label: 'Detected', width: '160px', format: (v) => formatDate(v) },
  ]

  function asRows<T>(items: T[]): Row[] {
    return items as unknown as Row[]
  }

  const viewItems = $derived([
    { id: 'policies', label: 'Policies', count: policies.length },
    { id: 'violations', label: 'Violations', count: violations.length },
  ])
</script>

<div class="flex h-full min-h-0 flex-col">
  <div class="flex h-10 shrink-0 items-center gap-3 px-5">
    <Tabs variant="segmented" size="sm" items={viewItems} value={view} onchange={(id) => (view = id as View)} />
    <span class="ml-auto text-xs text-fg-4">
      {view === 'policies' ? `${policies.length} ${policies.length === 1 ? 'policy' : 'policies'}` : `${violations.length} ${violations.length === 1 ? 'violation' : 'violations'}`}
    </span>
    <Button size="sm" onclick={() => openPolicyForm()}>
      <Plus size={14} /> New policy
    </Button>
  </div>

  <div class="min-h-0 flex-1 px-5 pb-4">
    {#if loading}
      <div class="flex h-full items-center justify-center"><Spinner /></div>
    {:else if view === 'policies'}
      {#if policies.length === 0}
        <EmptyState
          icon={Shield}
          title="No policies yet"
          description="A policy names a database, table or column and the role a user needs to touch it. Queries that break it are recorded as violations, or refused outright in block mode."
          primary={{ label: 'New policy', onclick: () => openPolicyForm() }}
        />
      {:else}
        <div class="h-full overflow-hidden rounded-lg border border-edge-subtle bg-surface">
          <DataTable columns={policyColumns} rows={asRows(policies)} fill rowKey={(r) => String(r.id)} sort={{ key: 'name', dir: 'asc' }}>
            {#snippet cell(row, col, value)}
              {#if col.key === 'name'}
                {@const policy = row as unknown as Policy}
                <p class="truncate font-medium text-fg">{policy.name}</p>
                {#if policy.description}
                  <p class="mt-0.5 truncate text-xs text-fg-3" title={policy.description}>{policy.description}</p>
                {/if}
              {:else if col.key === 'severity'}
                <Badge tone={severityTone(value)}>{value}</Badge>
              {:else if col.key === 'enforcement_mode'}
                <Badge tone={value === 'block' ? 'danger' : 'neutral'}>{value}</Badge>
              {:else if col.key === 'enabled'}
                <Badge dot tone={value === 'enabled' ? 'success' : 'neutral'}>{value}</Badge>
              {:else}
                {value}
              {/if}
            {/snippet}
            {#snippet actions(row)}
              {@const policy = row as unknown as Policy}
              <Button icon variant="ghost" size="sm" aria-label="Edit policy" title="Edit policy" onclick={() => openPolicyForm(policy)}>
                <Pencil size={14} />
              </Button>
              <Button icon variant="ghost" size="sm" aria-label="Delete policy" title="Delete policy" onclick={() => requestDelete(policy)}>
                <Trash2 size={14} />
              </Button>
            {/snippet}
          </DataTable>
        </div>
      {/if}
    {:else if violations.length === 0}
      <EmptyState icon={AlertTriangle} title="No violations detected" description="Violations appear here when a query breaks an enabled policy." />
    {:else}
      <div class="h-full overflow-hidden rounded-lg border border-edge-subtle bg-surface">
        <DataTable columns={violationColumns} rows={asRows(violations)} fill rowKey={(r) => String(r.id)} sort={{ key: 'detected_at', dir: 'desc' }}>
          {#snippet cell(row, col, value)}
            {#if col.key === 'severity'}
              <Badge tone={severityTone(value)}>{value}</Badge>
            {:else if col.key === 'policy_name'}
              <span class="font-medium text-fg">{value}</span>
            {:else}
              {value}
            {/if}
          {/snippet}
          {#snippet actions(row)}
            <Button size="xs" variant="outline" onclick={() => createIncidentFromViolation(row as unknown as PolicyViolation)}>
              <MessageSquare size={12} /> Create incident
            </Button>
          {/snippet}
        </DataTable>
      </div>
    {/if}
  </div>
</div>

<Sheet
  open={sheetOpen}
  title={editingPolicy ? 'Edit policy' : 'New policy'}
  size="lg"
  onclose={closePolicyForm}
>
  <form
    id="policy-form"
    class="space-y-7"
    onsubmit={(e) => {
      e.preventDefault()
      void submitPolicy()
    }}
  >
    <p class="text-[13px] leading-relaxed text-fg-3">
      A policy says which role a user needs to touch a database, table or column. The syncer checks
      recent queries against every enabled policy and records a violation for each match. In warn
      mode the query still runs; in block mode CH-UI refuses it before execution, records the
      violation, opens an incident and raises an alert event.
    </p>

    <div class="space-y-4">
      <FormField label="Name" for="policy-name" required>
        <Input id="policy-name" bind:value={policyForm.name} placeholder="e.g. Analysts only on finance tables" />
      </FormField>
      <FormField label="Description" for="policy-description" hint="Why this rule exists. Shown in the policy list and on violations.">
        <Textarea id="policy-description" bind:value={policyForm.description} rows={2} />
      </FormField>
    </div>

    <div>
      <SectionHeader title="Scope" description="Which objects the rule covers. Leave a level on Any to match everything under it." />
      <div class="space-y-4">
        <FormField label="Object type" for="policy-object-type" hint="The level the rule is evaluated at. A table policy matches every query that reads or writes that table.">
          <Select id="policy-object-type" options={objectTypeOptions} value={policyForm.object_type} onchange={setObjectType} />
        </FormField>
        <div class="grid grid-cols-1 gap-4 md:grid-cols-3">
          <FormField label="Database">
            <Combobox options={databaseOptions} value={policyForm.object_database} onChange={setDatabase} placeholder="Any database (*)" />
          </FormField>
          <FormField label="Table">
            <Combobox
              options={tableOptions}
              value={policyForm.object_table}
              onChange={setTable}
              placeholder={policyForm.object_database ? 'Any table (*)' : 'Pick a database first'}
              disabled={policyForm.object_type === 'database' || !policyForm.object_database}
            />
          </FormField>
          <FormField label="Column">
            <Combobox
              options={columnOptions}
              value={policyForm.object_column}
              onChange={(v) => (policyForm.object_column = v)}
              placeholder={policyForm.object_table ? 'Any column (*)' : 'Pick a table first'}
              disabled={policyForm.object_type !== 'column' || !policyForm.object_table}
            />
          </FormField>
        </div>
      </div>
    </div>

    <div>
      <SectionHeader title="Requirement" description="Who is allowed. Users holding this ClickHouse role never violate the policy." />
      <FormField label="Required role" required hint={roles.length ? 'Roles come from system.roles on the connected cluster.' : 'No roles were found on the cluster. Enter the role name exactly as granted in ClickHouse.'}>
        {#if roles.length}
          <Combobox options={roleOptions} value={policyForm.required_role} onChange={(v) => (policyForm.required_role = v)} placeholder="Select a role" />
        {:else}
          <Input bind:value={policyForm.required_role} placeholder="e.g. analyst" mono />
        {/if}
      </FormField>
    </div>

    <div>
      <SectionHeader title="Enforcement" description="What happens when a query breaks the rule." />
      <div class="space-y-4">
        <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <FormField label="Mode" for="policy-mode" hint={policyForm.enforcement_mode === 'block' ? 'The query is refused with a policy_blocked error, an incident is opened and an alert event is raised.' : 'The query runs. The syncer records a violation on its next pass.'}>
            <Select id="policy-mode" options={modeOptions} value={policyForm.enforcement_mode} onchange={(v) => (policyForm.enforcement_mode = v as Policy['enforcement_mode'])} />
          </FormField>
          <FormField label="Severity" for="policy-severity" hint="Severity of the recorded violation. Alert rules filter on it.">
            <Select id="policy-severity" options={severityOptions} value={policyForm.severity} onchange={(v) => (policyForm.severity = v)} />
          </FormField>
        </div>
        <label class="ds-checkbox-label">
          <input type="checkbox" class="ds-checkbox" bind:checked={policyForm.enabled} />
          Enabled
        </label>
      </div>
    </div>

    <Panel variant="muted" padding="sm">
      <p class="text-[11px] font-medium uppercase tracking-wider text-fg-4">In plain words</p>
      <p class="mt-1 text-[13px] leading-relaxed text-fg">{summary}</p>
    </Panel>
  </form>

  {#snippet footer()}
    <Button variant="ghost" size="sm" onclick={closePolicyForm}>Cancel</Button>
    <Button size="sm" loading={saving} onclick={() => void submitPolicy()}>{editingPolicy ? 'Save changes' : 'Create policy'}</Button>
  {/snippet}
</Sheet>

<ConfirmDialog
  open={confirmDeleteOpen}
  title="Delete policy?"
  description={pendingDelete ? `Delete "${pendingDelete.name}"? Its recorded violations stay in history.` : 'This action cannot be undone.'}
  confirmLabel="Delete"
  destructive={true}
  loading={deleting}
  onconfirm={confirmDelete}
  oncancel={cancelDelete}
/>
