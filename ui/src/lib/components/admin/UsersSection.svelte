<script lang="ts">
  import { errorText } from '../../utils/errors'
  import { onMount, untrack } from 'svelte'
  import { takePageIntent } from '../../stores/page-intent.svelte'
  import { apiGet, apiPut, apiDel, apiPost, ApiError } from '../../api/client'
  import { removeSSOAccess } from '../../api/ssoIdentity'
  import type { SSOClickHouseUser } from '../../api/ssoIdentity'
  import { formatDate, formatRelativeTime } from '../../utils/format'
  import { navigate } from '../../stores/router.svelte'
  import { getSession } from '../../stores/session.svelte'
  import { success as toastSuccess, error as toastError, errorFrom as toastErrorFrom } from '../../stores/toast.svelte'
  import Spinner from '../common/Spinner.svelte'
  import Sheet from '../common/Sheet.svelte'
  import HelpTip from '../common/HelpTip.svelte'
  import GuideLink from '../common/GuideLink.svelte'
  import FormField from '../common/FormField.svelte'
  import Tabs from '../common/Tabs.svelte'
  import Badge from '../common/Badge.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import Button from '../common/Button.svelte'
  import Panel from '../common/Panel.svelte'
  import Input from '../common/Input.svelte'
  import Select from '../common/Select.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import { RefreshCw, UserPlus, KeyRound, Trash2, Search } from 'lucide-svelte'

  // Admin > Users. Two lists: CH-UI application users (with the role
  // override that grants Admin UI access) and the users defined in the
  // active ClickHouse connection (create, rotate password, delete).
  // A ClickHouse user's role override applies on one connection, so the
  // application list is scoped to the current connection; an SSO person's
  // override applies on every connection.
  type View = 'app' | 'clickhouse'
  type Row = Record<string, unknown>

  /** One entry of GET /api/admin/users (database.SessionUser). */
  type AppUser = {
    username: string
    display_name: string
    clickhouse_user: string
    via_sso: boolean
    user_role: string
    last_login: string
    session_count: number
    exists_in_clickhouse?: boolean
    /** Per-person ClickHouse users of an SSO person, one per connection; empty otherwise. */
    clickhouse_users?: SSOClickHouseUser[]
  }
  type UsersResponse = { users?: AppUser[]; sync?: { clickhouse_user_check?: boolean } }
  /** One entry of GET /api/admin/user-roles. connection_id is '' for SSO people (all connections). */
  type UserRoleOverride = { username: string; connection_id: string; connection_name?: string; role: string; created_at: string }

  let view = $state<View>('app')
  let search = $state('')

  // ── Application users ─────────────────────────────────────
  let users = $state<AppUser[]>([])
  let usersSyncCheck = $state(false)
  let usersError = $state<string | null>(null)
  let userRoles = $state<Record<string, string>>({})
  let usersLoading = $state(true)
  let roleSavingUser = $state<string | null>(null)

  // Remove access: drops an SSO person's ClickHouse users on every
  // connection and ends their sessions.
  let removeAccessTarget = $state<AppUser | null>(null)
  let removeAccessLoading = $state(false)

  const currentConnectionId = $derived(getSession()?.connectionId ?? '')
  const currentConnectionName = $derived(getSession()?.connectionName || 'this connection')

  const roleOptions = [
    { value: 'admin', label: 'admin' },
    { value: 'analyst', label: 'analyst' },
    { value: 'viewer', label: 'viewer' },
  ]

  // ── ClickHouse users ──────────────────────────────────────
  let chUsers = $state<Row[]>([])
  let chUsersError = $state<string | null>(null)
  let createCHUserSheetOpen = $state(false)
  let editCHUserPasswordSheetOpen = $state(false)
  let deleteCHUserConfirmOpen = $state(false)
  let deleteCHUserLoading = $state(false)
  let selectedCHUserName = $state('')
  let createCHUserErrorMessage = $state('')
  let createCHUserExecutedCommands = $state<string[]>([])
  let createCHUserForm = $state({
    name: '',
    authType: 'sha256_password',
    password: '',
    defaultRoles: '',
    ifNotExists: true,
  })
  let updateCHUserPasswordForm = $state({
    authType: 'sha256_password',
    password: '',
    ifExists: true,
  })

  const clickHouseAuthTypeOptions = [
    { value: 'sha256_password', label: 'sha256_password' },
    { value: 'plaintext_password', label: 'plaintext_password' },
    { value: 'double_sha1_password', label: 'double_sha1_password' },
    { value: 'no_password', label: 'no_password' },
  ]

  function describeUsersError(e: unknown): string {
    const message = errorText(e)
    const status = e instanceof ApiError ? e.status : 0
    if (status === 403) return 'You need the CH-UI admin role to manage users.'
    if (/access_denied|not enough privileges|grant show users/i.test(message)) {
      return 'Your ClickHouse user lacks the SHOW USERS grant. Ask your ClickHouse admin for it (GRANT SHOW USERS ON *.*).'
    }
    if (status === 503 || /tunnel.*offline|connection.*offline/i.test(message)) {
      return 'The connection tunnel is offline. Bring the agent back online and refresh.'
    }
    return message
  }

  async function loadUsers() {
    usersLoading = true
    usersError = null
    try {
      const [usersResponse, roles] = await Promise.all([
        apiGet<UsersResponse | AppUser[]>('/api/admin/users'),
        apiGet<UserRoleOverride[]>('/api/admin/user-roles'),
      ])
      if (Array.isArray(usersResponse)) {
        users = usersResponse
        usersSyncCheck = false
      } else {
        users = usersResponse?.users ?? []
        usersSyncCheck = !!usersResponse?.sync?.clickhouse_user_check
      }
      const map: Record<string, string> = {}
      for (const r of roles ?? []) {
        if (r.connection_id === '' || r.connection_id === currentConnectionId) map[r.username] = r.role
      }
      userRoles = map
    } catch (e: unknown) {
      usersError = describeUsersError(e)
    } finally {
      usersLoading = false
    }
  }

  async function loadClickHouseUsers() {
    chUsersError = null
    try {
      const res = await apiGet<{ data: Row[]; meta: { name: string; type: string }[] }>('/api/admin/clickhouse-users')
      chUsers = res.data ?? []
    } catch (e: unknown) {
      chUsersError = describeUsersError(e)
    }
  }

  async function refreshUsersTab() {
    await Promise.all([loadUsers(), loadClickHouseUsers()])
  }

  onMount(() => {
    void refreshUsersTab()
  })

  // SSO people are keyed "sso:<email>" so they never collide with a
  // ClickHouse user; show the email.
  function displayUser(username: string): string {
    return username.startsWith('sso:') ? username.slice(4) : username
  }

  function isSSOKey(username: string): boolean {
    return username.startsWith('sso:')
  }

  /** Where an override for this user applies. */
  function roleScopeLabel(username: string): string {
    return isSSOKey(username) ? 'All connections' : currentConnectionName
  }

  async function setRole(username: string, role: string) {
    if (!username || roleSavingUser === username) return
    roleSavingUser = username
    try {
      const body = isSSOKey(username) ? { role } : { role, connection_id: currentConnectionId }
      await apiPut(`/api/admin/user-roles/${encodeURIComponent(username)}`, body)
      userRoles = { ...userRoles, [username]: role }
      toastSuccess(`Role set to ${role} for ${displayUser(username)} on ${isSSOKey(username) ? 'all connections' : currentConnectionName}`)
    } catch (e) {
      toastErrorFrom(e)
    } finally {
      if (roleSavingUser === username) roleSavingUser = null
    }
  }

  async function removeRole(username: string) {
    try {
      const query = isSSOKey(username) ? '' : `?connection_id=${encodeURIComponent(currentConnectionId)}`
      await apiDel(`/api/admin/user-roles/${encodeURIComponent(username)}${query}`)
      const { [username]: _, ...rest } = userRoles
      userRoles = rest
      toastSuccess(`Role override removed for ${displayUser(username)}`)
    } catch (e) {
      toastErrorFrom(e)
    }
  }

  async function confirmRemoveAccess() {
    if (!removeAccessTarget) return
    const target = removeAccessTarget
    const email = displayUser(target.username)
    removeAccessLoading = true
    try {
      const res = await removeSSOAccess(email)
      const dropped = res.dropped ?? 0
      const ended = res.sessions_ended ?? 0
      const failed = res.failed ?? []
      const summary = `${dropped} ClickHouse user${dropped === 1 ? '' : 's'} dropped, ${ended} session${ended === 1 ? '' : 's'} ended for ${email}`
      if (failed.length === 0) {
        toastSuccess(summary)
      } else {
        toastError(`${summary}. Failed on ${failed.map((f) => `${f.connection_name}: ${f.error}`).join('; ')}`)
      }
      removeAccessTarget = null
      await loadUsers()
    } catch (e) {
      toastErrorFrom(e)
    } finally {
      removeAccessLoading = false
    }
  }

  function chUsersOf(row: Row): SSOClickHouseUser[] {
    const list = row.clickhouse_users
    return Array.isArray(list) ? (list as SSOClickHouseUser[]) : []
  }

  // ClickHouse >= 24.9 returns auth_type as Array(Enum8) (multiple auth methods),
  // older versions return a scalar Enum8 string. Normalize both shapes.
  const CH_PASSWORD_AUTH_TYPES = ['no_password', 'plaintext_password', 'sha256_password', 'double_sha1_password']

  function formatCHAuthType(value: unknown): string {
    if (Array.isArray(value)) {
      const parts = value.filter((v) => typeof v === 'string' && v.trim())
      return parts.length > 0 ? parts.join(', ') : '—'
    }
    if (typeof value === 'string' && value.trim()) return value
    return '—'
  }

  function normalizeCHAuthTypeForForm(value: unknown): string {
    const raw = Array.isArray(value) ? value[0] : value
    const lowered = typeof raw === 'string' ? raw.toLowerCase() : ''
    return CH_PASSWORD_AUTH_TYPES.includes(lowered) ? lowered : 'sha256_password'
  }

  function formatCHDefaultRoles(row: Row): string {
    if (row?.default_roles_all === 1 || row?.default_roles_all === true) return 'ALL'
    const list = row?.default_roles_list
    if (Array.isArray(list) && list.length > 0) return list.join(', ')
    if (typeof list === 'string' && list.trim()) {
      const trimmed = list.trim()
      if (trimmed.startsWith('[') && trimmed.endsWith(']')) {
        try {
          const parsed = JSON.parse(trimmed)
          if (Array.isArray(parsed) && parsed.length > 0) return parsed.join(', ')
        } catch {
          return trimmed
        }
      }
      return trimmed
    }
    return '—'
  }

  function resetCreateCHUserForm() {
    createCHUserErrorMessage = ''
    createCHUserExecutedCommands = []
    createCHUserForm = { name: '', authType: 'sha256_password', password: '', defaultRoles: '', ifNotExists: true }
  }

  function openCreateCHUserSheet() {
    resetCreateCHUserForm()
    createCHUserSheetOpen = true
  }

  // "New ClickHouse user" from the command palette.
  $effect(() => {
    if (!takePageIntent('new-clickhouse-user')) return
    untrack(() => {
      view = 'clickhouse'
      openCreateCHUserSheet()
    })
  })

  function openUpdateCHUserPasswordSheet(username: string, authType: unknown) {
    selectedCHUserName = username
    updateCHUserPasswordForm = { authType: normalizeCHAuthTypeForForm(authType), password: '', ifExists: true }
    editCHUserPasswordSheetOpen = true
  }

  async function createClickHouseUser() {
    createCHUserErrorMessage = ''
    if (!createCHUserForm.name.trim()) {
      toastError('Username is required')
      return
    }
    if (createCHUserForm.authType !== 'no_password' && !createCHUserForm.password.trim()) {
      toastError('Password is required for selected auth type')
      return
    }
    const defaultRoles = createCHUserForm.defaultRoles.split(',').map((v) => v.trim()).filter(Boolean)
    try {
      const result = await apiPost<{ commands?: string[]; command?: string }>('/api/admin/clickhouse-users', {
        name: createCHUserForm.name.trim(),
        auth_type: createCHUserForm.authType,
        password: createCHUserForm.password,
        default_roles: defaultRoles,
        if_not_exists: createCHUserForm.ifNotExists,
      })
      createCHUserExecutedCommands = result?.commands ?? (result?.command ? [result.command] : [])
      toastSuccess(`ClickHouse user "${createCHUserForm.name.trim()}" created`)
      createCHUserSheetOpen = false
      resetCreateCHUserForm()
      await refreshUsersTab()
    } catch (e) {
      createCHUserErrorMessage = e instanceof Error ? e.message : 'Create user failed'
      toastError('Create user failed. See details in the sheet.')
    }
  }

  function escapeIdentifierForPreview(input: string): string {
    return `\`${input.replace(/`/g, '``')}\``
  }

  function escapeLiteralForPreview(input: string): string {
    return input.replace(/\\/g, '\\\\').replace(/'/g, "\\'")
  }

  function buildCreateCHUserCommandPreview(): string {
    const user = createCHUserForm.name.trim() || 'new_user'
    const auth = createCHUserForm.authType
    // The preview never shows the typed password.
    const pass = createCHUserForm.password ? '***' : ''
    const roles = createCHUserForm.defaultRoles.split(',').map((value) => value.trim()).filter(Boolean)

    const createParts: string[] = ['CREATE USER']
    if (createCHUserForm.ifNotExists) createParts.push('IF NOT EXISTS')
    createParts.push(escapeIdentifierForPreview(user))
    if (auth === 'plaintext_password') {
      createParts.push(`IDENTIFIED BY '${escapeLiteralForPreview(pass || 'password')}'`)
    } else if (auth !== 'no_password') {
      createParts.push(`IDENTIFIED WITH ${auth} BY '${escapeLiteralForPreview(pass || 'password')}'`)
    }

    const statements = [createParts.join(' ')]
    if (roles.length > 0) {
      const escapedRoles = roles.map((role) => escapeIdentifierForPreview(role)).join(', ')
      statements.push(`GRANT ${escapedRoles} TO ${escapeIdentifierForPreview(user)}`)
      statements.push(`ALTER USER ${escapeIdentifierForPreview(user)} DEFAULT ROLE ${escapedRoles}`)
    }
    return statements.join(';\n') + ';'
  }

  async function updateClickHouseUserPassword() {
    if (!selectedCHUserName) return
    if (updateCHUserPasswordForm.authType !== 'no_password' && !updateCHUserPasswordForm.password.trim()) {
      toastError('Password is required for selected auth type')
      return
    }
    try {
      await apiPut(`/api/admin/clickhouse-users/${encodeURIComponent(selectedCHUserName)}/password`, {
        auth_type: updateCHUserPasswordForm.authType,
        password: updateCHUserPasswordForm.password,
        if_exists: updateCHUserPasswordForm.ifExists,
      })
      toastSuccess(`Password updated for "${selectedCHUserName}"`)
      editCHUserPasswordSheetOpen = false
      selectedCHUserName = ''
      updateCHUserPasswordForm = { authType: 'sha256_password', password: '', ifExists: true }
      await loadClickHouseUsers()
    } catch (e) {
      toastErrorFrom(e)
    }
  }

  function openDeleteCHUserConfirm(username: string) {
    selectedCHUserName = username
    deleteCHUserConfirmOpen = true
  }

  function cancelDeleteCHUser() {
    deleteCHUserConfirmOpen = false
    deleteCHUserLoading = false
    selectedCHUserName = ''
  }

  async function confirmDeleteCHUser() {
    if (!selectedCHUserName) return
    deleteCHUserLoading = true
    try {
      await apiDel(`/api/admin/clickhouse-users/${encodeURIComponent(selectedCHUserName)}`)
      toastSuccess(`ClickHouse user "${selectedCHUserName}" deleted`)
      cancelDeleteCHUser()
      await refreshUsersTab()
    } catch (e) {
      deleteCHUserLoading = false
      toastErrorFrom(e)
    }
  }

  // ── Rows ──────────────────────────────────────────────────
  const term = $derived(search.trim().toLowerCase())

  const appRows = $derived.by<Row[]>(() =>
    users
      .map((u) => ({
        ...u,
        role: userRoles[u.username] ?? u.user_role ?? 'viewer',
        has_override: !!userRoles[u.username],
        scope_label: roleScopeLabel(u.username),
        last_login_label: u.last_login ? formatRelativeTime(u.last_login) : '—',
      }))
      .filter((r) => !term || String(r.display_name ?? r.username).toLowerCase().includes(term) || String(r.role).includes(term)),
  )

  const chRows = $derived.by<Row[]>(() =>
    chUsers
      .map((u): Row => ({
        ...u,
        auth_type_label: formatCHAuthType(u.auth_type),
        storage_label: u.storage ?? '—',
        default_roles_label: formatCHDefaultRoles(u),
      }))
      .filter((r) => {
        if (!term) return true
        return [r.name, r.auth_type_label, r.storage_label, r.default_roles_label]
          .some((v) => String(v).toLowerCase().includes(term))
      }),
  )
  const visibleCount = $derived(view === 'app' ? appRows.length : chRows.length)

  const appColumns: DataColumn<Row>[] = [
    { key: 'username', label: 'Username', mono: true, width: '30%' },
    { key: 'role', label: 'Role', sortable: false, width: '320px' },
    { key: 'scope_label', label: 'Applies to', sortable: false },
    { key: 'last_login_label', label: 'Last login', sortValue: (r) => String(r.last_login ?? '') },
  ]

  const chColumns: DataColumn<Row>[] = [
    { key: 'name', label: 'Name', mono: true, width: '26%' },
    { key: 'auth_type_label', label: 'Auth type', mono: true },
    { key: 'storage_label', label: 'Storage', mono: true },
    { key: 'default_roles_label', label: 'Default roles', mono: true, truncate: true, width: '30%' },
  ]

  const viewItems = $derived([
    { id: 'app', label: 'Application users', count: users.length },
    { id: 'clickhouse', label: 'ClickHouse users', count: chUsers.length },
  ])
</script>

<div class="flex h-full min-h-0 flex-col">
  <div class="flex h-10 shrink-0 items-center gap-3 px-5">
    <Tabs variant="segmented" size="sm" items={viewItems} value={view} onchange={(id) => (view = id as View)} />
    {#if view === 'app'}
      <HelpTip text="Admin access is explicit in CH-UI (role override). ClickHouse grants alone do not grant Admin UI actions. A ClickHouse user's role applies on the current connection only; an SSO person's role applies on all connections. Connections added from the login page never grant admin. The last admin override cannot be removed." />
    {:else}
      <HelpTip text="Direct user management in ClickHouse. Create users, rotate passwords, and remove users without leaving CH-UI." />
    {/if}
    <div class="relative">
      <Search size={13} class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-4" />
      <Input size="sm" type="search" class="w-64 pl-8" placeholder={view === 'app' ? 'Search users' : 'Search ClickHouse users'} bind:value={search} spellcheck={false} />
    </div>
    {#if view === 'app' && !usersError && !usersSyncCheck && !usersLoading}
      <Badge tone="warning" title="The ClickHouse user check could not run, so this list may include stale session users.">Unverified list</Badge>
    {/if}
    <div class="ml-auto flex items-center gap-2">
      <GuideLink guide="invite-teammate" label="How to invite a teammate" class="mr-2" />
      <span class="text-xs text-fg-4 tabular-nums">{visibleCount} {visibleCount === 1 ? 'user' : 'users'}</span>
      <Button icon variant="ghost" size="sm" aria-label="Refresh" title="Refresh" onclick={() => void refreshUsersTab()}>
        <RefreshCw size={14} />
      </Button>
      {#if view === 'clickhouse'}
        <Button size="sm" onclick={openCreateCHUserSheet}>
          <UserPlus size={14} /> New user
        </Button>
      {/if}
    </div>
  </div>

  <div class="min-h-0 flex-1 px-5 pb-4">
    {#if usersLoading}
      <div class="flex h-full items-center justify-center"><Spinner /></div>
    {:else if view === 'app' && usersError}
      <div class="rounded-md bg-danger-soft px-3 py-2 text-xs text-danger">{usersError}</div>
    {:else if view === 'clickhouse' && chUsersError}
      <div class="rounded-md bg-danger-soft px-3 py-2 text-xs text-danger">{chUsersError}</div>
    {:else}
      <div class="h-full overflow-hidden rounded-lg border border-edge-subtle bg-surface">
        {#if view === 'app'}
          <DataTable
            fill
            columns={appColumns}
            rows={appRows}
            rowKey={(r) => String(r.username)}
            sort={{ key: 'username', dir: 'asc' }}
            emptyTitle={term ? 'Nothing matches' : 'No users found'}
            emptyDescription={term ? 'Try another search.' : 'Users appear here after their first login.'}
          >
            {#snippet cell(row, col, value)}
              {#if col.key === 'role'}
                {@const username = String(row.username)}
                {@const currentRole = String(row.role)}
                <Tabs
                  variant="segmented"
                  size="sm"
                  items={roleOptions.map((r) => ({ id: r.value, label: r.label, disabled: roleSavingUser === username && r.value !== currentRole }))}
                  value={currentRole}
                  onchange={(id) => { if (id !== currentRole) void setRole(username, id) }}
                />
              {:else if col.key === 'username'}
                {@const chUsers = chUsersOf(row)}
                <div class="min-w-0">
                  <span class="inline-flex min-w-0 items-center gap-2">
                    <span class="truncate">{row.display_name ?? value}</span>
                    {#if row.via_sso}
                      <Badge tone="neutral">SSO</Badge>
                      {#if chUsers.length === 0}
                        <span class="truncate text-xs text-fg-3" title="ClickHouse account this person queries as">as {row.clickhouse_user}</span>
                      {/if}
                    {/if}
                  </span>
                  {#each chUsers as cu (cu.connection_id)}
                    <div class="flex items-center gap-1.5 text-xs text-fg-3">
                      <span class="truncate" title={cu.last_sync_at ? `Synced ${formatDate(cu.last_sync_at)}` : undefined}>
                        {cu.connection_name}: {cu.ch_user}{#if cu.roles.length > 0}, roles {cu.roles.join(', ')}{/if}{#if cu.last_sync_at}, synced {formatRelativeTime(cu.last_sync_at)}{/if}
                      </span>
                      {#if cu.locked}<Badge tone="warning">locked</Badge>{/if}
                    </div>
                  {/each}
                </div>
              {:else if col.key === 'scope_label'}
                <span class="text-xs text-fg-3" title={row.via_sso ? 'SSO roles follow the person to every connection' : 'ClickHouse user roles are set per connection'}>{value}</span>
              {:else if col.key === 'last_login_label'}
                <span title={row.last_login ? formatDate(row.last_login) : undefined}>{value}</span>
              {:else}
                {value}
              {/if}
            {/snippet}
            {#snippet actions(row)}
              {#if row.has_override}
                <Button size="xs" variant="ghost" class="text-danger hover:text-danger" onclick={() => void removeRole(String(row.username))}>
                  Remove override
                </Button>
              {/if}
              {#if row.via_sso}
                <Button size="xs" variant="ghost" class="text-danger hover:text-danger" title="Drop this person's ClickHouse users on every connection and sign them out" onclick={() => (removeAccessTarget = row as unknown as AppUser)}>
                  Remove access
                </Button>
              {/if}
            {/snippet}
          </DataTable>
        {:else}
          <DataTable
            fill
            columns={chColumns}
            rows={chRows}
            rowKey={(r) => String(r.name)}
            sort={{ key: 'name', dir: 'asc' }}
            emptyTitle={term ? 'Nothing matches' : 'No ClickHouse users loaded'}
            emptyDescription={term ? 'Try another search.' : 'Create the first user with the button above.'}
          >
            {#snippet actions(row)}
              <Button size="xs" variant="ghost" title="Edit this user's roles and privileges in Governance > Access control" onclick={() => navigate(`/governance?section=rbac&user=${encodeURIComponent(String(row.name))}`)}>
                Edit grants
              </Button>
              <Button icon variant="ghost" size="xs" aria-label="Change password" title="Change password" onclick={() => openUpdateCHUserPasswordSheet(String(row.name), row.auth_type)}>
                <KeyRound size={13} />
              </Button>
              <Button icon variant="ghost" size="xs" aria-label="Delete user" title="Delete" class="hover:text-danger" onclick={() => openDeleteCHUserConfirm(String(row.name))}>
                <Trash2 size={13} />
              </Button>
            {/snippet}
          </DataTable>
        {/if}
      </div>
    {/if}
  </div>
</div>

<Sheet
  open={createCHUserSheetOpen}
  title="Create ClickHouse user"
  description="Creates the user directly in ClickHouse on the active connection. The auth type controls how ClickHouse stores and verifies the credential."
  size="lg"
  onclose={() => (createCHUserSheetOpen = false)}
>
  <form id="ch-create-user-form" class="space-y-4" onsubmit={(e) => { e.preventDefault(); void createClickHouseUser() }}>
    <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
      <FormField label="Username" for="ch-user-name" required>
        <Input id="ch-user-name" size="sm" placeholder="analytics_reader" bind:value={createCHUserForm.name} required />
      </FormField>
      <FormField label="Auth type" for="ch-user-auth">
        <Select id="ch-user-auth" size="sm" options={clickHouseAuthTypeOptions} value={createCHUserForm.authType} onchange={(v) => (createCHUserForm = { ...createCHUserForm, authType: v })} />
      </FormField>
      <FormField label="Password" for="ch-user-password" class="md:col-span-2" controlWidth="full">
        <Input
          id="ch-user-password"
          size="sm"
          type="password"
          autocomplete="new-password"
          placeholder={createCHUserForm.authType === 'no_password' ? 'Not required for no_password' : 'Enter password'}
          bind:value={createCHUserForm.password}
          disabled={createCHUserForm.authType === 'no_password'}
        />
      </FormField>
      <FormField label="Default roles" hint="Optional, comma-separated." for="ch-user-roles" class="md:col-span-2" controlWidth="full">
        <Input id="ch-user-roles" size="sm" placeholder="role_reader, role_writer" bind:value={createCHUserForm.defaultRoles} />
      </FormField>
    </div>

    <label class="ds-checkbox-label text-xs">
      <input type="checkbox" class="ds-checkbox" bind:checked={createCHUserForm.ifNotExists} />
      Use IF NOT EXISTS
    </label>

    <Panel variant="muted" padding="sm" title="Command preview">
      <pre class="max-h-36 overflow-auto whitespace-pre-wrap break-all font-mono text-[11px] text-fg-2">{buildCreateCHUserCommandPreview()}</pre>
    </Panel>

    {#if createCHUserErrorMessage}
      <div class="rounded-md bg-danger-soft p-3">
        <p class="mb-1 text-xs font-semibold text-danger">Create user error</p>
        <pre class="max-h-36 overflow-auto whitespace-pre-wrap break-words font-mono text-[11px] text-fg-2">{createCHUserErrorMessage}</pre>
      </div>
    {/if}

    {#if createCHUserExecutedCommands.length > 0}
      <Panel padding="sm" title="Executed commands">
        <div class="space-y-2">
          {#each createCHUserExecutedCommands as sql}
            <pre class="max-h-24 overflow-auto whitespace-pre-wrap break-all font-mono text-[11px] text-fg-2">{sql}</pre>
          {/each}
        </div>
      </Panel>
    {/if}
  </form>
  {#snippet footer()}
    <Button size="sm" variant="ghost" onclick={() => (createCHUserSheetOpen = false)}>Cancel</Button>
    <Button size="sm" type="submit" form="ch-create-user-form" disabled={!createCHUserForm.name.trim()}>Create user</Button>
  {/snippet}
</Sheet>

<Sheet
  open={editCHUserPasswordSheetOpen}
  title="Change password"
  description={selectedCHUserName ? `ClickHouse user ${selectedCHUserName}` : ''}
  size="md"
  onclose={() => (editCHUserPasswordSheetOpen = false)}
>
  <form id="ch-edit-password-form" class="space-y-4" onsubmit={(e) => { e.preventDefault(); void updateClickHouseUserPassword() }}>
    <FormField label="Auth type" for="ch-user-edit-auth">
      <Select id="ch-user-edit-auth" size="sm" options={clickHouseAuthTypeOptions} value={updateCHUserPasswordForm.authType} onchange={(v) => (updateCHUserPasswordForm = { ...updateCHUserPasswordForm, authType: v })} />
    </FormField>

    <FormField label="New password" for="ch-user-new-password">
      <Input
        id="ch-user-new-password"
        size="sm"
        type="password"
        autocomplete="new-password"
        placeholder={updateCHUserPasswordForm.authType === 'no_password' ? 'Not required for no_password' : 'Enter new password'}
        bind:value={updateCHUserPasswordForm.password}
        disabled={updateCHUserPasswordForm.authType === 'no_password'}
      />
    </FormField>

    <label class="ds-checkbox-label text-xs">
      <input type="checkbox" class="ds-checkbox" bind:checked={updateCHUserPasswordForm.ifExists} />
      Use IF EXISTS
    </label>
  </form>
  {#snippet footer()}
    <Button size="sm" variant="ghost" onclick={() => (editCHUserPasswordSheetOpen = false)}>Cancel</Button>
    <Button size="sm" type="submit" form="ch-edit-password-form" disabled={!selectedCHUserName}>Save password</Button>
  {/snippet}
</Sheet>

<ConfirmDialog
  open={removeAccessTarget !== null}
  title="Remove ClickHouse access?"
  description={removeAccessTarget ? `Drop every ClickHouse user provisioned for ${displayUser(removeAccessTarget.username)} on every connection and sign them out of CH-UI. They can sign in again through SSO, which provisions fresh users from their current groups.` : ''}
  confirmLabel="Remove access"
  destructive={true}
  loading={removeAccessLoading}
  onconfirm={confirmRemoveAccess}
  oncancel={() => { if (!removeAccessLoading) removeAccessTarget = null }}
/>

<ConfirmDialog
  open={deleteCHUserConfirmOpen}
  title="Delete ClickHouse user?"
  description={selectedCHUserName ? `Delete "${selectedCHUserName}" from ClickHouse? This cannot be undone.` : 'This action cannot be undone.'}
  confirmLabel="Delete user"
  destructive={true}
  loading={deleteCHUserLoading}
  onconfirm={confirmDeleteCHUser}
  oncancel={cancelDeleteCHUser}
/>
