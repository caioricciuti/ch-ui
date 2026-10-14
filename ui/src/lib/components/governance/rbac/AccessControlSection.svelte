<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { onMount } from 'svelte'
  import { Plus, RefreshCw, Search } from 'lucide-svelte'
  import type { RbacApplyResponse, RbacOp, RbacRowPolicy, RbacQuota, RbacSettingsProfile, RbacSnapshot, RbacTarget } from '../../../types/rbac'
  import { fetchRbacHistory, fetchRbacSnapshot } from '../../../api/rbac'
  import { setSection } from '../../../stores/nav.svelte'
  import { takeStagedRevert } from './history'
  import { success as toastSuccess, error as toastError } from '../../../stores/toast.svelte'
  import { assigneesLabel, membersOf } from './changes'
  import Select from '../../common/Select.svelte'
  import Input from '../../common/Input.svelte'
  import Button from '../../common/Button.svelte'
  import Badge from '../../common/Badge.svelte'
  import Spinner from '../../common/Spinner.svelte'
  import DataTable, { type DataColumn } from '../../common/DataTable.svelte'
  import type { ComboboxOption } from '../../common/Combobox.svelte'
  import PrincipalSheet from './PrincipalSheet.svelte'
  import RowPolicySheet from './RowPolicySheet.svelte'
  import QuotaSheet from './QuotaSheet.svelte'
  import ProfileSheet from './ProfileSheet.svelte'
  import ReviewSheet from './ReviewSheet.svelte'

  /**
   * Governance > Access control. One table per entity kind, picked in the
   * toolbar. Edits open in a sheet and are staged into a change set; the
   * review sheet previews the exact statements and applies them.
   */
  type Kind = 'roles' | 'users' | 'row_policies' | 'quotas' | 'profiles'
  type Row = Record<string, unknown>
  const asRows = <T,>(rows: T[]): Row[] => rows as unknown as Row[]

  let snapshot = $state<RbacSnapshot | null>(null)
  let loading = $state(true)
  let loadError = $state('')
  let kind = $state<Kind>('roles')
  let search = $state('')
  let pending = $state<RbacOp[]>([])
  // The access history row a staged revert goes back to.
  let revertOf = $state('')
  let outsideChanges = $state(0)

  let principal = $state<{ open: boolean; target: RbacTarget | null }>({ open: false, target: null })
  let policySheet = $state<{ open: boolean; policy: RbacRowPolicy | null }>({ open: false, policy: null })
  let quotaSheet = $state<{ open: boolean; quota: RbacQuota | null }>({ open: false, quota: null })
  let profileSheet = $state<{ open: boolean; profile: RbacSettingsProfile | null }>({ open: false, profile: null })
  let reviewOpen = $state(false)
  // Bumped on every open so each sheet starts from a fresh form.
  let sheetSeq = $state(0)

  onMount(() => {
    const wanted = new URLSearchParams(window.location.search).get('user')
    const staged = takeStagedRevert()
    fetchRbacHistory({ driftOnly: true, limit: 1 })
      .then((p) => (outsideChanges = p.outside_last_30d))
      .catch(() => (outsideChanges = 0))
    void load().then(() => {
      if (staged) {
        pending = staged.ops
        revertOf = staged.revertOf
        reviewOpen = true
        return
      }
      if (wanted && snapshot?.users?.some((u) => u.name === wanted)) {
        kind = 'users'
        sheetSeq++
        principal = { open: true, target: { type: 'user', name: wanted } }
      }
    })
  })

  async function load() {
    loading = true
    loadError = ''
    try {
      snapshot = await fetchRbacSnapshot()
    } catch (err: unknown) {
      loadError = err instanceof Error ? err.message : String(err)
    } finally {
      loading = false
    }
  }

  const caps = $derived(snapshot?.capabilities)
  const canManage = $derived({
    roles: !!caps?.manage_roles,
    users: !!(caps?.access_management || caps?.role_admin || caps?.grant_option),
    row_policies: !!caps?.manage_row_policies,
    quotas: !!caps?.manage_quotas,
    profiles: !!caps?.manage_settings_profiles,
  })

  const assigneeOptions = $derived<ComboboxOption[]>([
    ...(snapshot?.users ?? []).map((u) => ({ value: u.name, label: u.name, hint: 'user' })),
    ...(snapshot?.roles ?? []).map((r) => ({ value: r.name, label: r.name, hint: 'role' })),
  ])

  // ── Rows per kind ─────────────────────────────────────────────────

  const grantCount = (t: RbacTarget) =>
    (snapshot?.grants ?? []).filter((g) => (t.type === 'user' ? g.user_name === t.name : g.role_name === t.name)).length

  const roleRows = $derived(
    (snapshot?.roles ?? []).map((r) => {
      const members = membersOf(snapshot, r.name)
      return {
        name: r.name,
        storage: r.storage,
        profile: (snapshot?.role_profiles?.[r.name] ?? []).join(', '),
        assignees: members.join(', '),
        member_count: members.length,
        privileges: grantCount({ type: 'role', name: r.name }),
      }
    }),
  )
  const userRows = $derived(
    (snapshot?.users ?? []).map((u) => {
      const held = (snapshot?.role_grants ?? []).filter((g) => g.user_name === u.name).map((g) => g.granted_role)
      return {
        name: u.name,
        storage: u.storage,
        roles: held.join(', '),
        default_roles: u.default_roles_all ? 'ALL' : u.default_roles.join(', '),
        privileges: grantCount({ type: 'user', name: u.name }),
      }
    }),
  )
  const policyRows = $derived(
    (snapshot?.row_policies ?? []).map((p) => ({
      name: p.name, key: p.full_name, table: `${p.database}.${p.table}`, using: p.using,
      mode: p.restrictive ? 'restrictive' : 'permissive', assignees: assigneesLabel(p.to), storage: p.storage,
    })),
  )
  const quotaRows = $derived(
    (snapshot?.quotas ?? []).map((q) => ({
      name: q.name, keyed_by: q.keyed_by || 'not keyed', intervals: q.intervals.length,
      assignees: assigneesLabel(q.to), storage: q.storage,
    })),
  )
  const profileRows = $derived(
    (snapshot?.settings_profiles ?? []).map((p) => ({
      name: p.name, settings: p.settings.length, inherit: (p.inherit ?? []).join(', '),
      assignees: assigneesLabel(p.to), storage: p.storage,
    })),
  )

  const allRows = $derived<Record<Kind, Row[]>>({
    roles: asRows(roleRows), users: asRows(userRows), row_policies: asRows(policyRows), quotas: asRows(quotaRows), profiles: asRows(profileRows),
  })

  const visibleRows = $derived.by(() => {
    const term = search.trim().toLowerCase()
    const rows = allRows[kind]
    if (!term) return rows
    return rows.filter((r) => Object.values(r).some((v) => String(v ?? '').toLowerCase().includes(term)))
  })

  const columns: Record<Kind, DataColumn<Row>[]> = {
    roles: [
      { key: 'name', label: 'Role' },
      { key: 'storage', label: 'Storage', width: '140px' },
      { key: 'assignees', label: 'Granted to', truncate: true },
      { key: 'profile', label: 'Profile', format: (v) => (v ? String(v) : '-') },
      { key: 'member_count', label: 'Members', align: 'right', width: '90px' },
      { key: 'privileges', label: 'Privileges', align: 'right', width: '100px' },
    ],
    users: [
      { key: 'name', label: 'User' },
      { key: 'storage', label: 'Storage', width: '140px' },
      { key: 'roles', label: 'Roles', truncate: true, format: (v) => (v ? String(v) : '-') },
      { key: 'default_roles', label: 'Default roles', truncate: true, format: (v) => (v ? String(v) : '-') },
      { key: 'privileges', label: 'Privileges', align: 'right', width: '100px' },
    ],
    row_policies: [
      { key: 'name', label: 'Policy' },
      { key: 'table', label: 'Table', mono: true },
      { key: 'using', label: 'Condition', mono: true, truncate: true },
      { key: 'mode', label: 'Mode', width: '110px' },
      { key: 'assignees', label: 'Applies to', truncate: true },
      { key: 'storage', label: 'Storage', width: '140px' },
    ],
    quotas: [
      { key: 'name', label: 'Quota' },
      { key: 'keyed_by', label: 'Counted by', mono: true },
      { key: 'intervals', label: 'Intervals', align: 'right', width: '90px' },
      { key: 'assignees', label: 'Applies to', truncate: true },
      { key: 'storage', label: 'Storage', width: '140px' },
    ],
    profiles: [
      { key: 'name', label: 'Profile' },
      { key: 'settings', label: 'Settings', align: 'right', width: '90px' },
      { key: 'inherit', label: 'Inherits', format: (v) => (v ? String(v) : '-') },
      { key: 'assignees', label: 'Applies to', truncate: true },
      { key: 'storage', label: 'Storage', width: '140px' },
    ],
  }

  const kindOptions = $derived([
    { value: 'roles', label: `Roles (${roleRows.length})` },
    { value: 'users', label: `Users (${userRows.length})` },
    { value: 'row_policies', label: `Row policies (${policyRows.length})` },
    { value: 'quotas', label: `Quotas (${quotaRows.length})` },
    { value: 'profiles', label: `Settings profiles (${profileRows.length})` },
  ])

  const newLabel: Record<Kind, string> = { roles: 'New role', users: '', row_policies: 'New policy', quotas: 'New quota', profiles: 'New profile' }
  const emptyText: Record<Kind, string> = {
    roles: 'No roles yet', users: 'No users visible', row_policies: 'No row policies', quotas: 'No quotas', profiles: 'No settings profiles',
  }

  // ── Actions ───────────────────────────────────────────────────────

  function openNew() {
    sheetSeq++
    if (kind === 'roles') principal = { open: true, target: null }
    else if (kind === 'row_policies') policySheet = { open: true, policy: null }
    else if (kind === 'quotas') quotaSheet = { open: true, quota: null }
    else if (kind === 'profiles') profileSheet = { open: true, profile: null }
  }

  function openRow(row: Row) {
    sheetSeq++
    const name = String(row.name)
    if (kind === 'roles') principal = { open: true, target: { type: 'role', name } }
    else if (kind === 'users') principal = { open: true, target: { type: 'user', name } }
    else if (kind === 'row_policies') policySheet = { open: true, policy: (snapshot?.row_policies ?? []).find((p) => p.full_name === row.key) ?? null }
    else if (kind === 'quotas') quotaSheet = { open: true, quota: (snapshot?.quotas ?? []).find((q) => q.name === name) ?? null }
    else profileSheet = { open: true, profile: (snapshot?.settings_profiles ?? []).find((p) => p.name === name) ?? null }
  }

  function stage(ops: RbacOp[]) {
    pending = [...pending, ...ops]
  }

  function unstage(index: number) {
    pending = pending.filter((_, i) => i !== index)
    if (pending.length === 0) revertOf = ''
  }

  function storageOf(t: RbacTarget | null): string {
    if (!t) return ''
    const list = t.type === 'role' ? (snapshot?.roles ?? []) : (snapshot?.users ?? [])
    return list.find((e) => e.name === t.name)?.storage ?? ''
  }

  function openReview() {
    principal = { ...principal, open: false }
    reviewOpen = true
  }

  function applied(res: RbacApplyResponse) {
    pending = []
    revertOf = ''
    if (res.failed > 0) toastError(`${res.failed} statement failed; later ones were skipped`)
    else toastSuccess(`Applied ${res.results.length} statement${res.results.length === 1 ? '' : 's'}`)
    void load()
  }

  function rollback(ops: RbacOp[]) {
    pending = ops
    revertOf = ''
  }

  function storageTone(s: unknown): 'neutral' | 'info' | 'warning' {
    return s === 'replicated' ? 'info' : s === 'users_xml' ? 'warning' : 'neutral'
  }
</script>

<div class="flex h-full min-h-0 flex-col">
  <div class="flex h-10 shrink-0 items-center gap-2 px-5">
    <Select size="sm" class="w-52" options={kindOptions} value={kind} onchange={(v) => (kind = v as Kind)} />
    {#if caps && !caps.access_management}
      <Badge tone="warning" title="Your ClickHouse user lacks ACCESS MANAGEMENT. Actions it cannot run are disabled.">Limited access</Badge>
    {/if}
    {#if snapshot?.errors?.length}
      <Badge tone="warning" title={snapshot.errors.join('\n')}>Partial data</Badge>
    {/if}
    {#if outsideChanges > 0}
      <button type="button" class="rounded-sm" title="Changes made outside CH-UI in the last 30 days" onclick={() => setSection('access-history')}>
        <Badge tone="warning">{outsideChanges} outside change{outsideChanges === 1 ? '' : 's'}</Badge>
      </button>
    {/if}
    <div class="flex-1"></div>
    <div class="relative">
      <Search size={13} class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-4" />
      <Input size="sm" type="search" class="w-56 pl-8" placeholder="Search" bind:value={search} spellcheck={false} />
    </div>
    <span class="text-xs tabular-nums text-fg-4">{visibleRows.length} rows</span>
    <Button icon variant="ghost" size="sm" aria-label="Refresh" title="Refresh" onclick={() => void load()}>
      <RefreshCw size={14} />
    </Button>
    {#if pending.length > 0}
      <Button size="sm" variant="outline" onclick={openReview}>Pending changes ({pending.length})</Button>
    {/if}
    {#if newLabel[kind]}
      <Button size="sm" disabled={!snapshot || !canManage[kind]} onclick={openNew}><Plus size={14} /> {newLabel[kind]}</Button>
    {/if}
  </div>

  <div class="min-h-0 flex-1 px-5 pb-4">
    {#if loading && !snapshot}
      <div class="flex h-full items-center justify-center"><Spinner /></div>
    {:else if loadError}
      <div class="rounded-md bg-danger-soft px-3 py-2 text-xs text-danger">{loadError}</div>
    {:else}
      <div class="h-full overflow-hidden rounded-lg border border-edge-subtle bg-surface">
        {#key kind}
          <DataTable
            columns={columns[kind]}
            rows={visibleRows}
            fill
            rowKey={(r) => String(r.key ?? r.name)}
            sort={{ key: 'name', dir: 'asc' }}
            emptyTitle={allRows[kind].length === 0 ? emptyText[kind] : 'Nothing matches the search'}
            emptyDescription={allRows[kind].length === 0 && newLabel[kind] ? `Use ${newLabel[kind]} to stage one.` : undefined}
            onrowclick={openRow}
          >
            {#snippet cell(_row, col, value)}
              {#if col.key === 'storage'}
                <Badge tone={storageTone(value)}>{value}</Badge>
              {:else if col.key === 'mode'}
                <Badge tone={value === 'restrictive' ? 'warning' : 'neutral'}>{value}</Badge>
              {:else if col.key === 'name'}
                <span class="font-medium text-fg">{value}</span>
              {:else}
                {value}
              {/if}
            {/snippet}
          </DataTable>
        {/key}
      </div>
    {/if}
  </div>
</div>

{#key `${sheetSeq}:${principal.target?.type ?? ''}:${principal.target?.name ?? ''}`}
  <PrincipalSheet
    open={principal.open}
    target={principal.target}
    {snapshot}
    {pending}
    readOnly={storageOf(principal.target) === 'users_xml'}
    canEdit={principal.target?.type === 'user' ? canManage.users : canManage.roles}
    onstage={stage}
    onunstage={unstage}
    onretarget={(t) => (principal = { open: true, target: t })}
    onreview={openReview}
    onclose={() => (principal = { ...principal, open: false })}
  />
{/key}

{#key sheetSeq}
  <RowPolicySheet
    open={policySheet.open}
    policy={policySheet.policy}
    {assigneeOptions}
    readOnly={policySheet.policy?.storage === 'users_xml'}
    canEdit={canManage.row_policies}
    onstage={stage}
    onclose={() => (policySheet = { ...policySheet, open: false })}
  />
{/key}

{#key sheetSeq}
  <QuotaSheet
    open={quotaSheet.open}
    quota={quotaSheet.quota}
    {assigneeOptions}
    readOnly={quotaSheet.quota?.storage === 'users_xml'}
    canEdit={canManage.quotas}
    onstage={stage}
    onclose={() => (quotaSheet = { ...quotaSheet, open: false })}
  />
{/key}

{#key sheetSeq}
  <ProfileSheet
    open={profileSheet.open}
    profile={profileSheet.profile}
    profileNames={(snapshot?.settings_profiles ?? []).map((p) => p.name)}
    {assigneeOptions}
    readOnly={profileSheet.profile?.storage === 'users_xml'}
    canEdit={canManage.profiles}
    onstage={stage}
    onclose={() => (profileSheet = { ...profileSheet, open: false })}
  />
{/key}

<ReviewSheet
  open={reviewOpen}
  ops={pending}
  {snapshot}
  onunstage={unstage}
  onclose={() => (reviewOpen = false)}
  onapplied={applied}
  onrollback={rollback}
  {revertOf}
/>
