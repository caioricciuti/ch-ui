<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { onMount } from 'svelte'
  import { X } from 'lucide-svelte'
  import type { RbacOp, RbacSnapshot, RbacTarget, RbacGrantRow } from '../../../types/rbac'
  import { listDatabases, listTables } from '../../../api/query'
  import { describeOp, grantsOf, opsForTarget, revokeOpFor, rolesHeldBy, membersOf, splitList } from './changes'
  import Sheet from '../../common/Sheet.svelte'
  import Button from '../../common/Button.svelte'
  import Badge from '../../common/Badge.svelte'
  import Input from '../../common/Input.svelte'
  import Select from '../../common/Select.svelte'
  import FormField from '../../common/FormField.svelte'
  import Panel from '../../common/Panel.svelte'
  import Combobox, { type ComboboxOption } from '../../common/Combobox.svelte'
  import DataTable, { type DataColumn } from '../../common/DataTable.svelte'
  import { navigate } from '../../../stores/router.svelte'
  import { withBase } from '../../../basePath'
  import { explainPath } from './explain'

  /**
   * One role or user: its settings profile, privileges, roles and (for
   * users) default roles. Every edit is staged into the change set; nothing
   * runs until the review is applied. With no target it creates a role.
   */
  interface Props {
    open: boolean
    target: RbacTarget | null
    snapshot: RbacSnapshot | null
    pending: RbacOp[]
    readOnly: boolean
    canEdit: boolean
    onstage: (ops: RbacOp[]) => void
    onunstage: (index: number) => void
    onretarget: (t: RbacTarget) => void
    onreview: () => void
    onclose: () => void
  }

  let { open, target, snapshot, pending, readOnly, canEdit, onstage, onunstage, onretarget, onreview, onclose }: Props = $props()

  type Row = Record<string, unknown>

  const locked = $derived(readOnly || !canEdit)
  const profiles = $derived((snapshot?.settings_profiles ?? []).map((p) => p.name))
  const profileOptions = $derived([{ value: '', label: 'No profile' }, ...profiles.map((p) => ({ value: p, label: p }))])
  const roleOptions = $derived<ComboboxOption[]>(
    (snapshot?.roles ?? []).filter((r) => !(target?.type === 'role' && r.name === target.name)).map((r) => ({ value: r.name, label: r.name })),
  )
  const privilegeOptions = $derived<ComboboxOption[]>(
    (snapshot?.privileges ?? []).map((p) => ({ value: p.name, label: p.name, hint: p.level ? p.level.toLowerCase() : undefined })),
  )

  // New role form.
  let newName = $state('')
  let newProfile = $state('')

  // Existing role profile.
  const currentProfile = $derived(target?.type === 'role' ? (snapshot?.role_profiles?.[target.name] ?? [])[0] ?? '' : '')
  let profileDraft = $state<string | null>(null)
  const profileValue = $derived(profileDraft ?? currentProfile)

  // Grant form.
  let access = $state<string[]>([])
  let database = $state('')
  let table = $state('')
  let columns = $state('')
  let grantOption = $state(false)
  let databases = $state<string[]>([])
  let tables = $state<string[]>([])

  // Role grant form.
  let addRole = $state('')
  let addAdmin = $state(false)

  // Default roles (users).
  const user = $derived(target?.type === 'user' ? (snapshot?.users ?? []).find((u) => u.name === target.name) : undefined)
  let defaultMode = $state<'all' | 'none' | 'list' | null>(null)
  let defaultList = $state<string | null>(null)
  const effectiveDefaultMode = $derived(defaultMode ?? (user?.default_roles_all ? 'all' : user && user.default_roles.length ? 'list' : 'none'))
  const effectiveDefaultList = $derived(defaultList ?? (user?.default_roles ?? []).join(', '))

  onMount(() => {
    listDatabases().then((d) => (databases = d)).catch(() => (databases = []))
  })

  async function pickDatabase(db: string) {
    database = db
    table = ''
    tables = []
    if (db && db !== '*') {
      try {
        tables = await listTables(db)
      } catch {
        tables = []
      }
    }
  }

  const databaseOptions = $derived<ComboboxOption[]>([{ value: '*', label: 'All databases (*)' }, ...databases.map((d) => ({ value: d, label: d }))])
  const tableOptions = $derived<ComboboxOption[]>([{ value: '*', label: 'All tables (*)' }, ...tables.map((t) => ({ value: t, label: t }))])

  const grants = $derived(target ? grantsOf(snapshot, target) : [])
  const held = $derived(target ? rolesHeldBy(snapshot, target) : [])
  const members = $derived(target?.type === 'role' ? membersOf(snapshot, target.name) : [])
  const staged = $derived(target ? opsForTarget(pending, target) : [])

  const grantColumns: DataColumn<Row>[] = [
    { key: 'access', label: 'Privilege', mono: true },
    { key: 'object', label: 'On', mono: true },
    { key: 'flags', label: '', sortable: false, align: 'right' },
  ]
  const grantRows = $derived(
    grants.map((g, i) => ({
      key: i,
      access: g.column ? `${g.access}(${g.column})` : g.access,
      object: g.database ? (g.table ? `${g.database}.${g.table}` : `${g.database}.*`) : '*.*',
      flags: g.partial_revoke ? 'partial revoke' : g.grant_option ? 'grant option' : '',
      grant: g,
    })),
  )

  function stageNewRole() {
    const name = newName.trim()
    if (!name) return
    onstage([{ kind: 'create_role', name, profile: newProfile || undefined }])
    onretarget({ type: 'role', name })
    newName = ''
    newProfile = ''
  }

  function stageProfile() {
    if (!target || profileDraft === null || profileDraft === currentProfile) return
    onstage([{ kind: 'set_role_profile', name: target.name, profile: profileDraft }])
    profileDraft = null
  }

  function addAccess(p: string) {
    if (p && !access.includes(p)) access = [...access, p]
  }

  function stageGrant() {
    if (!target || access.length === 0) return
    const db = database && database !== '*' ? database : ''
    const tbl = db && table && table !== '*' ? table : ''
    onstage([{
      kind: 'grant_privilege', target: { ...target }, access: [...access], database: db, table: tbl,
      columns: tbl ? splitList(columns) : [], grant_option: grantOption,
    }])
    access = []
    columns = ''
    grantOption = false
  }

  function stageRevoke(g: RbacGrantRow) {
    if (!target) return
    onstage([revokeOpFor(target, g)])
  }

  function stageRoleGrant() {
    if (!target || !addRole) return
    onstage([{ kind: 'grant_role', target: { ...target }, roles: [addRole], admin_option: addAdmin }])
    addRole = ''
    addAdmin = false
  }

  function stageDefaults() {
    if (!target) return
    const mode = effectiveDefaultMode
    onstage([{
      kind: 'set_default_roles', target: { ...target }, default_all: mode === 'all',
      roles: mode === 'list' ? splitList(effectiveDefaultList) : [],
    }])
    defaultMode = null
    defaultList = null
  }

  const title = $derived(target ? `${target.type === 'role' ? 'Role' : 'User'} ${target.name}` : 'New role')
  const description = $derived(
    !target
      ? 'Creates a ClickHouse role. Grants are added once the role is staged.'
      : readOnly
        ? 'Defined in users.xml, so it is read-only here.'
        : 'Changes are staged. Nothing runs until you review and apply them.',
  )
</script>

<Sheet {open} {title} {description} size="lg" {onclose}>
  {#if !target}
    <form class="space-y-4" onsubmit={(e) => { e.preventDefault(); stageNewRole() }}>
      <FormField label="Role name" for="rbac-role-name" required>
        <Input id="rbac-role-name" size="sm" placeholder="analyst" bind:value={newName} spellcheck={false} />
      </FormField>
      <FormField label="Settings profile" for="rbac-role-profile" hint="Optional. Members inherit its settings.">
        <Select id="rbac-role-profile" size="sm" options={profileOptions} bind:value={newProfile} />
      </FormField>
    </form>
  {:else}
    <div class="space-y-6">
      {#if target.type === 'user'}
        <p class="text-[13px] text-fg-3">
          What can {target.name} actually do, through every role and policy?
          <a class="font-medium text-fg underline underline-offset-2 hover:text-accent" href={withBase(explainPath(target.name))}
            onclick={(e) => { e.preventDefault(); navigate(explainPath(target.name)) }}>Explain access</a>
        </p>
      {/if}

      {#if target.type === 'role'}
        <Panel title="Settings profile" description={members.length ? `Held by ${members.join(', ')}` : 'Not granted to anyone yet.'}>
          <div class="flex items-end gap-2">
            <div class="min-w-0 flex-1">
              <Select size="sm" options={profileOptions} value={profileValue} disabled={locked} onchange={(v) => (profileDraft = v)} />
            </div>
            <Button size="sm" variant="outline" disabled={locked || profileDraft === null || profileDraft === currentProfile} onclick={stageProfile}>Stage</Button>
          </div>
        </Panel>
      {/if}

      <section class="space-y-2">
        <h3 class="text-[13px] font-semibold text-fg">Privileges</h3>
        <DataTable columns={grantColumns} rows={grantRows} rowKey={(r) => Number(r.key)} emptyTitle="No privileges" emptyDescription="Add one below.">
          {#snippet cell(_row, col, value)}
            {#if col.key === 'flags'}
              {#if value}<Badge tone={value === 'partial revoke' ? 'warning' : 'neutral'}>{value}</Badge>{/if}
            {:else}
              {value}
            {/if}
          {/snippet}
          {#snippet actions(row)}
            <Button size="xs" variant="ghost" disabled={locked} onclick={() => stageRevoke(row.grant as RbacGrantRow)}>
              {row.flags === 'partial revoke' ? 'Grant back' : 'Revoke'}
            </Button>
          {/snippet}
        </DataTable>

        <Panel variant="muted" padding="sm">
          <div class="space-y-3">
            <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
              <FormField label="Privilege" for="rbac-priv">
                <Combobox id="rbac-priv" options={privilegeOptions.filter((o) => !access.includes(o.value))} placeholder="Add a privilege" disabled={locked} onChange={addAccess} />
              </FormField>
              <div class="flex flex-wrap items-end gap-1.5">
                {#each access as a (a)}
                  <span class="inline-flex items-center gap-1 rounded-md bg-surface py-0.5 pl-2 pr-0.5 font-mono text-xs text-fg-2">
                    {a}
                    <Button icon variant="ghost" size="xs" aria-label={`Remove ${a}`} onclick={() => (access = access.filter((x) => x !== a))}><X size={12} /></Button>
                  </span>
                {/each}
              </div>
              <FormField label="Database" for="rbac-db">
                <Combobox id="rbac-db" options={databaseOptions} value={database || '*'} disabled={locked} onChange={pickDatabase} />
              </FormField>
              <FormField label="Table" for="rbac-table">
                <Combobox id="rbac-table" options={tableOptions} value={table || '*'} disabled={locked || !database || database === '*'} onChange={(v) => (table = v)} />
              </FormField>
              <FormField label="Columns" for="rbac-cols" hint="Optional, comma separated. Needs a table." class="md:col-span-2" controlWidth="full">
                <Input id="rbac-cols" size="sm" mono bind:value={columns} disabled={locked || !table || table === '*'} placeholder="id, tenant" />
              </FormField>
            </div>
            <div class="flex items-center justify-between gap-2">
              <label class="ds-checkbox-label text-xs">
                <input type="checkbox" class="ds-checkbox" bind:checked={grantOption} disabled={locked} />
                With grant option
              </label>
              <Button size="sm" variant="outline" disabled={locked || access.length === 0} onclick={stageGrant}>Stage grant</Button>
            </div>
          </div>
        </Panel>
      </section>

      <section class="space-y-2">
        <h3 class="text-[13px] font-semibold text-fg">Roles</h3>
        {#if held.length === 0}
          <p class="text-xs text-fg-3">No roles granted.</p>
        {:else}
          <div class="divide-y divide-edge-subtle rounded-lg border border-edge-subtle bg-surface">
            {#each held as h (h.role)}
              <div class="flex items-center gap-2 px-3 py-1.5 text-[13px]">
                <span class="flex-1 text-fg">{h.role}</span>
                {#if h.admin}<Badge tone="neutral">admin option</Badge>{/if}
                <Button size="xs" variant="ghost" disabled={locked} onclick={() => target && onstage([{ kind: 'revoke_role', target: { ...target }, roles: [h.role] }])}>Revoke</Button>
              </div>
            {/each}
          </div>
        {/if}
        <div class="flex items-end gap-2">
          <div class="min-w-0 flex-1">
            <Combobox options={roleOptions.filter((o) => !held.some((h) => h.role === o.value))} value={addRole} placeholder="Grant a role" disabled={locked} onChange={(v) => (addRole = v)} />
          </div>
          <label class="ds-checkbox-label shrink-0 text-xs">
            <input type="checkbox" class="ds-checkbox" bind:checked={addAdmin} disabled={locked} />
            Admin option
          </label>
          <Button size="sm" variant="outline" disabled={locked || !addRole} onclick={stageRoleGrant}>Stage</Button>
        </div>
      </section>

      {#if target.type === 'user'}
        <section class="space-y-2">
          <h3 class="text-[13px] font-semibold text-fg">Default roles</h3>
          <p class="text-xs text-fg-3">Roles switched on when the user signs in.</p>
          <div class="grid grid-cols-1 gap-2 md:grid-cols-[180px_1fr_auto] md:items-center">
            <Select
              size="sm"
              options={[{ value: 'all', label: 'All granted roles' }, { value: 'none', label: 'None' }, { value: 'list', label: 'These roles' }]}
              value={effectiveDefaultMode}
              disabled={locked}
              onchange={(v) => (defaultMode = v as 'all' | 'none' | 'list')}
            />
            <Input size="sm" value={effectiveDefaultList} disabled={locked || effectiveDefaultMode !== 'list'} placeholder="role_a, role_b" oninput={(e) => (defaultList = e.currentTarget.value)} />
            <Button size="sm" variant="outline" disabled={locked || (defaultMode === null && defaultList === null)} onclick={stageDefaults}>Stage</Button>
          </div>
        </section>
      {/if}

      {#if staged.length > 0}
        <Panel title={`Staged for ${target.name} (${staged.length})`}>
          <ul class="space-y-1">
            {#each staged as s (s.index)}
              <li class="flex items-center gap-2 text-[13px] text-fg-2">
                <span class="flex-1">{describeOp(s.op)}</span>
                <Button size="xs" variant="ghost" onclick={() => onunstage(s.index)}>Remove</Button>
              </li>
            {/each}
          </ul>
        </Panel>
      {/if}
    </div>
  {/if}

  {#snippet footer()}
    {#if !target}
      <Button size="sm" variant="outline" onclick={onclose}>Cancel</Button>
      <Button size="sm" disabled={!newName.trim() || !canEdit} onclick={stageNewRole}>Stage role</Button>
    {:else}
      {#if target.type === 'role' && !locked}
        <Button size="sm" variant="ghost" class="mr-auto text-danger hover:text-danger" onclick={() => { onstage([{ kind: 'drop_role', name: target.name }]); onclose() }}>Drop role</Button>
      {/if}
      <Button size="sm" variant="outline" onclick={onclose}>Close</Button>
      <Button size="sm" disabled={pending.length === 0} onclick={onreview}>Review changes ({pending.length})</Button>
    {/if}
  {/snippet}
</Sheet>
