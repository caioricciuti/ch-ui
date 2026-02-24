<script lang="ts">
  import { onMount } from 'svelte'
  import type { AdminStats } from '../lib/types/api'
  import { apiGet, apiPut, apiDel, apiPost } from '../lib/api/client'
  import type { BrainModelOption, BrainProviderAdmin, BrainSkill } from '../lib/types/brain'
  import {
    adminBulkUpdateBrainModels,
    adminCreateBrainProvider,
    adminCreateBrainSkill,
    adminDeleteBrainProvider,
    adminListBrainModels,
    adminListBrainProviders,
    adminListBrainSkills,
    adminSyncBrainProviderModels,
    adminUpdateBrainModel,
    adminUpdateBrainProvider,
    adminUpdateBrainSkill,
  } from '../lib/api/brain'
  import { success as toastSuccess, error as toastError } from '../lib/stores/toast.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import Combobox from '../lib/components/common/Combobox.svelte'
  import type { ComboboxOption } from '../lib/components/common/Combobox.svelte'
  import Sheet from '../lib/components/common/Sheet.svelte'
  import HelpTip from '../lib/components/common/HelpTip.svelte'
  import ConfirmDialog from '../lib/components/common/ConfirmDialog.svelte'
  import { Shield, RefreshCw, Users, Database, Activity, LogIn, ChevronDown, ChevronRight, Brain, UserPlus, KeyRound, Trash2, Plus, Copy } from 'lucide-svelte'

  // Tab state
  type AdminTab = 'overview' | 'tunnels' | 'users' | 'brain'
  const adminTabIds: AdminTab[] = ['overview', 'tunnels', 'users', 'brain']
  let activeTab = $state<AdminTab>('overview')

  type TunnelConnection = {
    id: string
    name: string
    status: string
    online: boolean
    created_at: string
    last_seen?: string
    host_info?: {
      hostname?: string
      os?: string
    } | null
  }
  type TunnelTokenResponse = {
    tunnel_token: string
    setup_instructions?: {
      connect?: string
      service?: string
    }
    message?: string
    connection?: {
      id?: string
      name?: string
    }
  }

  // Overview
  let stats = $state<AdminStats | null>(null)
  let connections = $state<any[]>([])
  let statsLoading = $state(true)

  // Tunnels
  let tunnels = $state<TunnelConnection[]>([])
  let tunnelsLoading = $state(false)
  let tunnelCreateName = $state('')
  let tunnelCreateLoading = $state(false)
  let tunnelDeleteLoading = $state(false)
  let tunnelDeleteConfirmOpen = $state(false)
  let tunnelDeleteTarget = $state<TunnelConnection | null>(null)
  let tunnelTokenPreview = $state<{
    connectionId: string
    connectionName: string
    token: string
    connectCmd: string
    serviceCmd: string
  } | null>(null)

  // Users
  let users = $state<any[]>([])
  let usersSyncCheck = $state(false)
  let userRoles = $state<Record<string, string>>({})
  let chUsers = $state<any[]>([])
  let usersLoading = $state(true)
  let roleSavingUser = $state<string | null>(null)
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

  // Brain admin
  let brainLoading = $state(false)
  let brainProviders = $state<BrainProviderAdmin[]>([])
  let brainModels = $state<BrainModelOption[]>([])
  let brainSkills = $state<BrainSkill[]>([])
  let modelProviderFilter = $state('')
  let modelSearch = $state('')
  let modelShowOnlyActive = $state(false)
  let providerSheetOpen = $state(false)
  let skillSheetOpen = $state(false)


  const roleOptions: ComboboxOption[] = [
    { value: 'admin', label: 'admin' },
    { value: 'analyst', label: 'analyst' },
    { value: 'viewer', label: 'viewer' },
  ]

  const providerKindOptions: ComboboxOption[] = [
    { value: 'openai', label: 'openai' },
    { value: 'openai_compatible', label: 'openai_compatible' },
    { value: 'ollama', label: 'ollama' },
  ]
  const clickHouseAuthTypeOptions: ComboboxOption[] = [
    { value: 'sha256_password', label: 'sha256_password' },
    { value: 'plaintext_password', label: 'plaintext_password' },
    { value: 'double_sha1_password', label: 'double_sha1_password' },
    { value: 'no_password', label: 'no_password' },
  ]
  let providerForm = $state({
    name: '',
    kind: 'openai',
    baseUrl: '',
    apiKey: '',
    isActive: true,
    isDefault: false,
  })
  let skillForm = $state({
    name: 'Default Brain Skill',
    content: '',
    isActive: true,
    isDefault: true,
  })

  function normalizeAdminTab(value: string | null | undefined): AdminTab {
    const raw = (value ?? '').trim().toLowerCase()
    if ((adminTabIds as string[]).includes(raw)) return raw as AdminTab
    return 'overview'
  }

  function syncAdminTabParam(tab: AdminTab) {
    if (typeof window === 'undefined') return
    const url = new URL(window.location.href)
    if (url.searchParams.get('tab') === tab) return
    url.searchParams.set('tab', tab)
    history.replaceState(null, '', `${url.pathname}?${url.searchParams.toString()}`)
  }

  onMount(() => {
    loadStats()
    loadConnections()
    const initialTab = normalizeAdminTab(
      typeof window === 'undefined' ? null : new URLSearchParams(window.location.search).get('tab'),
    )
    switchTab(initialTab, true)
  })

  async function loadStats() {
    statsLoading = true
    try {
      stats = await apiGet<AdminStats>('/api/admin/stats')
    } catch (e: any) {
      toastError(e.message)
    } finally {
      statsLoading = false
    }
  }

  async function loadConnections() {
    try {
      connections = await apiGet<any[]>('/api/admin/connections')
    } catch (e: any) {
      toastError(e.message)
    }
  }

  async function loadUsers() {
    usersLoading = true
    try {
      const [usersResponse, roles] = await Promise.all([
        apiGet<any>('/api/admin/users'),
        apiGet<any[]>('/api/admin/user-roles'),
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
        map[r.username] = r.role
      }
      userRoles = map
    } catch (e: any) {
      toastError(e.message)
    } finally {
      usersLoading = false
    }
  }

  async function refreshUsersTab() {
    await Promise.all([loadUsers(), loadClickHouseUsers()])
  }

  async function loadClickHouseUsers() {
    try {
      const res = await apiGet<{ data: any[]; meta: any[] }>('/api/admin/clickhouse-users')
      chUsers = res.data ?? []
    } catch (e: any) {
      toastError(e.message)
    }
  }

  function formatCHDefaultRoles(row: any): string {
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
    createCHUserForm = {
      name: '',
      authType: 'sha256_password',
      password: '',
      defaultRoles: '',
      ifNotExists: true,
    }
  }

  function openCreateCHUserSheet() {
    resetCreateCHUserForm()
    createCHUserSheetOpen = true
  }

  function openUpdateCHUserPasswordSheet(username: string, authType: string | null | undefined) {
    selectedCHUserName = username
    updateCHUserPasswordForm = {
      authType: (authType || 'sha256_password').toLowerCase(),
      password: '',
      ifExists: true,
    }
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

    const defaultRoles = createCHUserForm.defaultRoles
      .split(',')
      .map(v => v.trim())
      .filter(Boolean)

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
    } catch (e: any) {
      createCHUserErrorMessage = e.message ?? 'Create user failed'
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
    const pass = createCHUserForm.password
    const ifNotExists = createCHUserForm.ifNotExists
    const roles = createCHUserForm.defaultRoles
      .split(',')
      .map((value) => value.trim())
      .filter(Boolean)

    const createParts: string[] = ['CREATE USER']
    if (ifNotExists) createParts.push('IF NOT EXISTS')
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
      updateCHUserPasswordForm = {
        authType: 'sha256_password',
        password: '',
        ifExists: true,
      }
      await loadClickHouseUsers()
    } catch (e: any) {
      toastError(e.message)
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
    } catch (e: any) {
      deleteCHUserLoading = false
      toastError(e.message)
    }
  }

  function setTunnelTokenPreview(connection: { id: string; name: string }, payload: TunnelTokenResponse) {
    tunnelTokenPreview = {
      connectionId: connection.id,
      connectionName: connection.name,
      token: payload.tunnel_token ?? '',
      connectCmd: payload.setup_instructions?.connect ?? '',
      serviceCmd: payload.setup_instructions?.service ?? '',
    }
  }

  async function loadTunnels() {
    tunnelsLoading = true
    try {
      tunnels = await apiGet<TunnelConnection[]>('/api/connections')
    } catch (e: any) {
      toastError(e.message)
    } finally {
      tunnelsLoading = false
    }
  }

  async function createTunnel() {
    const name = tunnelCreateName.trim()
    if (!name) {
      toastError('Tunnel name is required')
      return
    }
    tunnelCreateLoading = true
    try {
      const res = await apiPost<TunnelTokenResponse>('/api/connections', { name })
      toastSuccess(`Tunnel "${name}" created`)
      tunnelCreateName = ''
      await Promise.all([loadTunnels(), loadConnections(), loadStats()])
      const createdConn = res.connection
      if (createdConn?.id && createdConn?.name && res.tunnel_token) {
        setTunnelTokenPreview({ id: createdConn.id, name: createdConn.name }, res)
      }
    } catch (e: any) {
      toastError(e.message)
    } finally {
      tunnelCreateLoading = false
    }
  }

  function requestDeleteTunnel(conn: TunnelConnection) {
    tunnelDeleteTarget = conn
    tunnelDeleteConfirmOpen = true
  }

  function cancelDeleteTunnel() {
    tunnelDeleteConfirmOpen = false
    tunnelDeleteLoading = false
    tunnelDeleteTarget = null
  }

  async function confirmDeleteTunnel() {
    if (!tunnelDeleteTarget) return
    tunnelDeleteLoading = true
    const target = tunnelDeleteTarget
    try {
      await apiDel(`/api/connections/${encodeURIComponent(target.id)}`)
      toastSuccess(`Tunnel "${target.name}" deleted`)
      if (tunnelTokenPreview?.connectionId === target.id) {
        tunnelTokenPreview = null
      }
      cancelDeleteTunnel()
      await Promise.all([loadTunnels(), loadConnections(), loadStats()])
    } catch (e: any) {
      tunnelDeleteLoading = false
      toastError(e.message)
    }
  }

  async function viewTunnelToken(conn: TunnelConnection) {
    try {
      const res = await apiGet<TunnelTokenResponse>(`/api/connections/${encodeURIComponent(conn.id)}/token`)
      setTunnelTokenPreview(conn, res)
      toastSuccess(`Token loaded for "${conn.name}"`)
    } catch (e: any) {
      toastError(e.message)
    }
  }

  async function regenerateTunnelToken(conn: TunnelConnection) {
    try {
      const res = await apiPost<TunnelTokenResponse>(`/api/connections/${encodeURIComponent(conn.id)}/regenerate-token`)
      setTunnelTokenPreview(conn, res)
      toastSuccess(res.message || `Token regenerated for "${conn.name}"`)
    } catch (e: any) {
      toastError(e.message)
    }
  }

  async function copyText(value: string, label: string) {
    if (!value) {
      toastError(`${label} is empty`)
      return
    }
    try {
      await navigator.clipboard.writeText(value)
      toastSuccess(`${label} copied`)
    } catch {
      toastError('Clipboard unavailable')
    }
  }

  function switchTab(tab: AdminTab, syncUrl = true) {
    activeTab = tab
    if (syncUrl) syncAdminTabParam(tab)
    if (tab === 'tunnels' && !tunnelsLoading && tunnels.length === 0) {
      loadTunnels()
    }
    if (tab === 'users' && users.length === 0) {
      refreshUsersTab()
    }
    if (tab === 'brain' && !brainLoading && brainProviders.length === 0 && brainSkills.length === 0) {
      loadBrainAdmin()
    }
  }

  async function loadBrainAdmin() {
    brainLoading = true
    try {
      const [providers, models, skills] = await Promise.all([
        adminListBrainProviders(),
        adminListBrainModels(),
        adminListBrainSkills(),
      ])
      brainProviders = providers
      brainModels = models
      brainSkills = skills
      if (!modelProviderFilter && providers.length > 0) {
        modelProviderFilter = providers[0].id
      } else if (modelProviderFilter && !providers.some(p => p.id === modelProviderFilter)) {
        modelProviderFilter = providers[0]?.id ?? ''
      }
      if (skills.length > 0 && !skillForm.content) {
        const active = skills.find(s => s.is_active) ?? skills[0]
        skillForm = {
          name: active.name,
          content: active.content,
          isActive: active.is_active,
          isDefault: active.is_default,
        }
      }
    } catch (e: any) {
      toastError(e.message)
    } finally {
      brainLoading = false
    }
  }


  async function createProvider() {
    try {
      await adminCreateBrainProvider(providerForm)
      toastSuccess('Brain provider created')
      providerForm = { ...providerForm, name: '', apiKey: '', isDefault: false }
      providerSheetOpen = false
      await loadBrainAdmin()
    } catch (e: any) {
      toastError(e.message)
    }
  }

  async function toggleProvider(provider: BrainProviderAdmin, key: 'is_active' | 'is_default', value: boolean) {
    try {
      await adminUpdateBrainProvider(provider.id, {
        isActive: key === 'is_active' ? value : provider.is_active,
        isDefault: key === 'is_default' ? value : provider.is_default,
      })
      toastSuccess('Provider updated')
      await loadBrainAdmin()
    } catch (e: any) {
      toastError(e.message)
    }
  }

  async function syncProviderModels(provider: BrainProviderAdmin) {
    try {
      await adminSyncBrainProviderModels(provider.id)
      toastSuccess(`Synced models for ${provider.name}. Recommended model auto-selected.`)
      await loadBrainAdmin()
    } catch (e: any) {
      toastError(e.message)
    }
  }

  async function deleteProvider(provider: BrainProviderAdmin) {
    if (!confirm(`Delete provider \"${provider.name}\"?`)) return
    try {
      await adminDeleteBrainProvider(provider.id)
      toastSuccess('Provider deleted')
      await loadBrainAdmin()
    } catch (e: any) {
      toastError(e.message)
    }
  }

  async function updateModel(model: BrainModelOption, key: 'is_active' | 'is_default', value: boolean) {
    try {
      await adminUpdateBrainModel(model.id, {
        displayName: model.display_name || model.name,
        isActive: key === 'is_active' ? value : model.is_active,
        isDefault: key === 'is_default' ? value : model.is_default,
      })
      toastSuccess('Model updated')
      await loadBrainAdmin()
    } catch (e: any) {
      toastError(e.message)
    }
  }

  function filteredBrainModels(): BrainModelOption[] {
    const providerID = modelProviderFilter.trim()
    const term = modelSearch.trim().toLowerCase()
    return brainModels.filter(model => {
      if (providerID && model.provider_id !== providerID) return false
      if (modelShowOnlyActive && !model.is_active) return false
      if (!term) return true
      const candidate = `${model.display_name || ''} ${model.name} ${model.provider_name}`.toLowerCase()
      return candidate.includes(term)
    })
  }

  function modelsForProvider(providerId: string): BrainModelOption[] {
    const term = modelSearch.trim().toLowerCase()
    return brainModels.filter(model => {
      if (model.provider_id !== providerId) return false
      if (modelShowOnlyActive && !model.is_active) return false
      if (!term) return true
      const candidate = `${model.display_name || ''} ${model.name} ${model.provider_name}`.toLowerCase()
      return candidate.includes(term)
    })
  }

  function providerFilterOptions(): ComboboxOption[] {
    return [
      { value: '', label: 'All providers' },
      ...brainProviders.map(provider => ({ value: provider.id, label: provider.name, hint: provider.kind })),
    ]
  }

  function visibleProvidersForModels(): BrainProviderAdmin[] {
    if (!modelProviderFilter) return brainProviders
    return brainProviders.filter(p => p.id === modelProviderFilter)
  }

  async function runModelBulkAction(action: 'deactivate_all' | 'activate_all' | 'activate_recommended') {
    if (!modelProviderFilter) {
      toastError('Select a provider first')
      return
    }
    try {
      await adminBulkUpdateBrainModels({ providerId: modelProviderFilter, action })
      if (action === 'deactivate_all') toastSuccess('All models deactivated for selected provider')
      if (action === 'activate_all') toastSuccess('All models activated for selected provider')
      if (action === 'activate_recommended') toastSuccess('Recommended model activated and set as default')
      await loadBrainAdmin()
    } catch (e: any) {
      toastError(e.message)
    }
  }

  async function saveSkill() {
    try {
      if (!skillForm.content.trim() || !skillForm.name.trim()) {
        toastError('Skill name and content are required')
        return
      }
      const active = brainSkills.find(s => s.is_active)
      if (active) {
        await adminUpdateBrainSkill(active.id, {
          name: skillForm.name,
          content: skillForm.content,
          isActive: skillForm.isActive,
          isDefault: skillForm.isDefault,
        })
      } else {
        await adminCreateBrainSkill({
          name: skillForm.name,
          content: skillForm.content,
          isActive: skillForm.isActive,
          isDefault: skillForm.isDefault,
        })
      }
      toastSuccess('Brain skill saved')
      skillSheetOpen = false
      await loadBrainAdmin()
    } catch (e: any) {
      toastError(e.message)
    }
  }

  function openSkillSheet() {
    const active = brainSkills.find(s => s.is_active) ?? brainSkills[0]
    if (active) {
      skillForm = {
        name: active.name,
        content: active.content,
        isActive: active.is_active,
        isDefault: active.is_default,
      }
    }
    skillSheetOpen = true
  }

  async function setRole(username: string, role: string) {
    if (!username || roleSavingUser === username) return
    roleSavingUser = username
    try {
      await apiPut(`/api/admin/user-roles/${encodeURIComponent(username)}`, { role })
      userRoles = { ...userRoles, [username]: role }
      toastSuccess(`Role set to ${role} for ${username}`)
    } catch (e: any) {
      toastError(e.message)
    } finally {
      if (roleSavingUser === username) roleSavingUser = null
    }
  }

  async function removeRole(username: string) {
    try {
      await apiDel(`/api/admin/user-roles/${encodeURIComponent(username)}`)
      const { [username]: _, ...rest } = userRoles
      userRoles = rest
      toastSuccess(`Role override removed for ${username}`)
    } catch (e: any) {
      toastError(e.message)
    }
  }

  function formatTime(ts: string): string {
    try {
      return new Date(ts).toLocaleString()
    } catch {
      return ts
    }
  }

  function truncate(s: string, max = 80): string {
    return s.length > max ? s.slice(0, max) + '...' : s
  }
</script>

<div class="flex flex-col h-full">
  <div class="border-b border-gray-200 dark:border-gray-800">
    <div class="flex flex-col gap-2 px-4 py-3 md:flex-row md:items-center md:gap-4">
      <div class="flex items-center gap-3">
        <Shield size={18} class="text-ch-blue" />
        <h1 class="ds-page-title">Admin Panel</h1>
      </div>
      <nav class="ds-tabs border-0 px-0 pt-0 gap-1 overflow-x-auto whitespace-nowrap" aria-label="Admin Tabs">
        {#each [['overview', 'Overview'], ['tunnels', 'Tunnels'], ['users', 'Users'], ['brain', 'Brain']] as [key, label]}
          <button
            class="ds-tab {activeTab === key ? 'ds-tab-active' : ''}"
            onclick={() => switchTab(key as AdminTab)}
          >
            {label}
          </button>
        {/each}
      </nav>
</div>
</div>

<Sheet
  open={providerSheetOpen}
  title="Create Brain Provider"
  size="lg"
  onclose={() => providerSheetOpen = false}
>
  <form
    class="space-y-4"
    onsubmit={(e) => {
      e.preventDefault()
      void createProvider()
    }}
  >
    <div class="flex items-center gap-2">
      <p class="text-xs text-gray-500">Provider controls which model catalog is available to all users.</p>
      <HelpTip text="OpenAI works with managed API keys. OpenAI-compatible is for custom gateways. Ollama usually uses local/base URL endpoints." />
    </div>

    <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
      <label class="space-y-1">
        <span class="text-xs text-gray-500">Provider Name</span>
        <input class="ds-input-sm" placeholder="OpenAI Prod" bind:value={providerForm.name} required />
      </label>
      <label class="space-y-1">
        <span class="text-xs text-gray-500">Provider Kind</span>
        <Combobox
          options={providerKindOptions}
          value={providerForm.kind}
          onChange={(v) => providerForm = { ...providerForm, kind: v }}
        />
      </label>
      <label class="space-y-1 md:col-span-2">
        <span class="text-xs text-gray-500">Base URL</span>
        <input
          class="ds-input-sm"
          placeholder={providerForm.kind === 'ollama' ? 'http://localhost:11434/v1' : 'https://api.openai.com'}
          bind:value={providerForm.baseUrl}
        />
      </label>
      <label class="space-y-1 md:col-span-2">
        <span class="text-xs text-gray-500">API Key</span>
        <input class="ds-input-sm" type="password" placeholder="sk-..." bind:value={providerForm.apiKey} />
      </label>
    </div>

    <div class="flex flex-wrap items-center gap-4">
      <label class="ds-checkbox-label text-xs">
        <input type="checkbox" class="ds-checkbox" bind:checked={providerForm.isActive} />
        Active
      </label>
      <label class="ds-checkbox-label text-xs">
        <input type="checkbox" class="ds-checkbox" bind:checked={providerForm.isDefault} />
        Default provider
      </label>
    </div>

    <div class="flex items-center justify-end gap-2 pt-2 border-t border-gray-200 dark:border-gray-800">
      <button type="button" class="ds-btn-outline" onclick={() => providerSheetOpen = false}>Cancel</button>
      <button type="submit" class="ds-btn-primary" disabled={!providerForm.name.trim()}>Create Provider</button>
    </div>
  </form>
</Sheet>

<Sheet
  open={skillSheetOpen}
  title="Global Brain Skill"
  size="xl"
  onclose={() => skillSheetOpen = false}
>
  <form
    class="space-y-4"
    onsubmit={(e) => {
      e.preventDefault()
      void saveSkill()
    }}
  >
    <div class="flex items-center gap-2">
      <p class="text-xs text-gray-500">This prompt steers SQL safety, artifact usage, and tool behavior for every chat.</p>
      <HelpTip text="Keep this instruction set practical: SQL guardrails, artifact expectations, and when to ask clarifying questions." />
    </div>

    <label class="space-y-1">
      <span class="text-xs text-gray-500">Skill Name</span>
      <input class="ds-input-sm" bind:value={skillForm.name} required />
    </label>

    <label class="space-y-1">
      <span class="text-xs text-gray-500">Skill Content</span>
      <textarea
        class="ds-input-sm min-h-[58vh] font-mono text-[12px] leading-relaxed resize-y"
        bind:value={skillForm.content}
        placeholder="You are Brain, a senior ClickHouse copilot..."
      ></textarea>
    </label>

    <div class="flex flex-wrap items-center gap-4">
      <label class="ds-checkbox-label text-xs">
        <input type="checkbox" class="ds-checkbox" bind:checked={skillForm.isActive} />
        Active
      </label>
      <label class="ds-checkbox-label text-xs">
        <input type="checkbox" class="ds-checkbox" bind:checked={skillForm.isDefault} />
        Default
      </label>
    </div>

    <div class="flex items-center justify-end gap-2 pt-2 border-t border-gray-200 dark:border-gray-800">
      <button type="button" class="ds-btn-outline" onclick={() => skillSheetOpen = false}>Cancel</button>
      <button type="submit" class="ds-btn-primary" disabled={!skillForm.name.trim() || !skillForm.content.trim()}>Save Skill</button>
    </div>
  </form>
</Sheet>

<Sheet
  open={createCHUserSheetOpen}
  title="Create ClickHouse User"
  size="lg"
  onclose={() => createCHUserSheetOpen = false}
>
  <form
    class="space-y-4"
    onsubmit={(e) => {
      e.preventDefault()
      void createClickHouseUser()
    }}
  >
    <div class="flex items-center gap-2">
      <p class="text-xs text-gray-500">Create users directly in ClickHouse for the active connection.</p>
      <HelpTip text="Use strong passwords. The selected auth type controls how ClickHouse stores and verifies credentials." />
    </div>

    <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
      <label class="space-y-1">
        <span class="text-xs text-gray-500">Username</span>
        <input class="ds-input-sm" placeholder="analytics_reader" bind:value={createCHUserForm.name} required />
      </label>
      <label class="space-y-1">
        <span class="text-xs text-gray-500">Auth Type</span>
        <Combobox
          options={clickHouseAuthTypeOptions}
          value={createCHUserForm.authType}
          onChange={(v) => createCHUserForm = { ...createCHUserForm, authType: v }}
        />
      </label>
      <label class="space-y-1 md:col-span-2">
        <span class="text-xs text-gray-500">Password</span>
        <input
          class="ds-input-sm"
          type="password"
          placeholder={createCHUserForm.authType === 'no_password' ? 'Not required for no_password' : 'Enter password'}
          bind:value={createCHUserForm.password}
          disabled={createCHUserForm.authType === 'no_password'}
        />
      </label>
      <label class="space-y-1 md:col-span-2">
        <span class="text-xs text-gray-500">Default Roles (optional)</span>
        <input class="ds-input-sm" placeholder="role_reader, role_writer" bind:value={createCHUserForm.defaultRoles} />
      </label>
    </div>

    <label class="ds-checkbox-label text-xs">
      <input type="checkbox" class="ds-checkbox" bind:checked={createCHUserForm.ifNotExists} />
      Use IF NOT EXISTS
    </label>

    <div class="ds-panel-muted p-3">
      <p class="text-xs font-semibold text-gray-700 dark:text-gray-200 mb-1">Command Preview</p>
      <pre class="text-[11px] max-h-36 overflow-auto whitespace-pre-wrap break-all text-gray-600 dark:text-gray-300">{buildCreateCHUserCommandPreview()}</pre>
    </div>

    {#if createCHUserErrorMessage}
      <div class="rounded-lg border border-red-500/40 bg-red-500/10 p-3">
        <p class="text-xs font-semibold text-red-200 mb-1">Create User Error</p>
        <pre class="text-[11px] whitespace-pre-wrap break-words max-h-36 overflow-auto text-red-100">{createCHUserErrorMessage}</pre>
      </div>
    {/if}

    {#if createCHUserExecutedCommands.length > 0}
      <div class="ds-panel p-3 space-y-2">
        <p class="text-xs font-semibold text-gray-700 dark:text-gray-200">Executed Commands</p>
        {#each createCHUserExecutedCommands as sql}
          <pre class="text-[11px] max-h-24 overflow-auto whitespace-pre-wrap break-all text-gray-600 dark:text-gray-300">{sql}</pre>
        {/each}
      </div>
    {/if}

    <div class="flex items-center justify-end gap-2 pt-2 border-t border-gray-200 dark:border-gray-800">
      <button type="button" class="ds-btn-outline" onclick={() => createCHUserSheetOpen = false}>Cancel</button>
      <button type="submit" class="ds-btn-primary" disabled={!createCHUserForm.name.trim()}>Create User</button>
    </div>
  </form>
</Sheet>

<Sheet
  open={editCHUserPasswordSheetOpen}
  title="Change ClickHouse User Password"
  size="md"
  onclose={() => editCHUserPasswordSheetOpen = false}
>
  <form
    class="space-y-4"
    onsubmit={(e) => {
      e.preventDefault()
      void updateClickHouseUserPassword()
    }}
  >
    <div class="ds-panel-muted p-3">
      <p class="text-xs text-gray-500">User</p>
      <p class="text-sm font-semibold text-gray-800 dark:text-gray-200">{selectedCHUserName || '—'}</p>
    </div>

    <label class="space-y-1">
      <span class="text-xs text-gray-500">Auth Type</span>
      <Combobox
        options={clickHouseAuthTypeOptions}
        value={updateCHUserPasswordForm.authType}
        onChange={(v) => updateCHUserPasswordForm = { ...updateCHUserPasswordForm, authType: v }}
      />
    </label>

    <label class="space-y-1">
      <span class="text-xs text-gray-500">New Password</span>
      <input
        class="ds-input-sm"
        type="password"
        placeholder={updateCHUserPasswordForm.authType === 'no_password' ? 'Not required for no_password' : 'Enter new password'}
        bind:value={updateCHUserPasswordForm.password}
        disabled={updateCHUserPasswordForm.authType === 'no_password'}
      />
    </label>

    <label class="ds-checkbox-label text-xs">
      <input type="checkbox" class="ds-checkbox" bind:checked={updateCHUserPasswordForm.ifExists} />
      Use IF EXISTS
    </label>

    <div class="flex items-center justify-end gap-2 pt-2 border-t border-gray-200 dark:border-gray-800">
      <button type="button" class="ds-btn-outline" onclick={() => editCHUserPasswordSheetOpen = false}>Cancel</button>
      <button type="submit" class="ds-btn-primary" disabled={!selectedCHUserName}>Save Password</button>
    </div>
  </form>
</Sheet>

<ConfirmDialog
  open={deleteCHUserConfirmOpen}
  title="Delete ClickHouse user?"
  description={selectedCHUserName ? `Delete "${selectedCHUserName}" from ClickHouse? This cannot be undone.` : 'This action cannot be undone.'}
  confirmLabel="Delete User"
  destructive={true}
  loading={deleteCHUserLoading}
  onconfirm={confirmDeleteCHUser}
  oncancel={cancelDeleteCHUser}
/>

<ConfirmDialog
  open={tunnelDeleteConfirmOpen}
  title="Delete tunnel?"
  description={tunnelDeleteTarget ? `Delete "${tunnelDeleteTarget.name}"? This disconnects the agent and cannot be undone.` : 'This action cannot be undone.'}
  confirmLabel="Delete Tunnel"
  destructive={true}
  loading={tunnelDeleteLoading}
  onconfirm={confirmDeleteTunnel}
  oncancel={cancelDeleteTunnel}
/>

  <!-- Content -->
  <div class="flex-1 overflow-auto p-4">
    {#if activeTab === 'overview'}
      <!-- Stats cards -->
      {#if statsLoading}
        <div class="flex items-center justify-center py-12"><Spinner /></div>
      {:else if stats}
	        <div class="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
	          <div class="ds-stat-card">
	            <div class="flex items-center gap-2 text-gray-500 text-xs mb-1"><Users size={14} /> Users</div>
	            <div class="text-2xl font-bold text-gray-900 dark:text-gray-100">{stats.users_count}</div>
	          </div>
	          <div class="ds-stat-card">
	            <div class="flex items-center gap-2 text-gray-500 text-xs mb-1"><Database size={14} /> Connections</div>
	            <div class="text-2xl font-bold text-gray-900 dark:text-gray-100">{stats.online} <span class="text-sm font-normal text-gray-500">/ {stats.connections}</span></div>
	          </div>
	          <div class="ds-stat-card">
	            <div class="flex items-center gap-2 text-gray-500 text-xs mb-1"><Activity size={14} /> Queries</div>
	            <div class="text-2xl font-bold text-gray-900 dark:text-gray-100">{stats.query_count}</div>
	          </div>
	          <div class="ds-stat-card">
	            <div class="flex items-center gap-2 text-gray-500 text-xs mb-1"><LogIn size={14} /> Logins</div>
	            <div class="text-2xl font-bold text-gray-900 dark:text-gray-100">{stats.login_count}</div>
	          </div>
	        </div>
      {/if}

      <!-- Connections -->
      <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-2">Connections</h2>
      {#if connections.length === 0}
        <p class="text-sm text-gray-500">No connections found</p>
      {:else}
	        <div class="flex flex-col gap-2">
	          {#each connections as conn}
	            <div class="ds-panel flex items-center gap-3 p-3">
	              <span class="w-2 h-2 rounded-full {conn.online ? 'bg-green-500' : 'bg-gray-400'}"></span>
	              <span class="text-sm font-medium text-gray-800 dark:text-gray-200">{conn.name}</span>
	              <span class="text-xs text-gray-500">{conn.id}</span>
              <span class="ml-auto text-xs {conn.online ? 'text-green-600' : 'text-gray-400'}">{conn.online ? 'Online' : 'Offline'}</span>
            </div>
          {/each}
        </div>
      {/if}

    {:else if activeTab === 'tunnels'}
      <div class="flex flex-wrap items-end gap-2 mb-3">
        <label class="space-y-1">
          <span class="text-xs text-gray-500">Tunnel Name</span>
          <input
            class="ds-input-sm w-72"
            placeholder="warehouse-prod"
            bind:value={tunnelCreateName}
            onkeydown={(e) => e.key === 'Enter' && createTunnel()}
          />
        </label>
        <button
          class="ds-btn-primary"
          onclick={() => createTunnel()}
          disabled={tunnelCreateLoading || !tunnelCreateName.trim()}
        >
          <Plus size={14} />
          {tunnelCreateLoading ? 'Creating...' : 'Create Tunnel'}
        </button>
        <button class="ds-btn-ghost" onclick={() => loadTunnels()} title="Refresh tunnels">
          <RefreshCw size={14} />
        </button>
      </div>

      {#if tunnelTokenPreview}
        {@const preview = tunnelTokenPreview}
        <div class="ds-panel p-3 mb-3 space-y-2">
          <div class="flex flex-wrap items-center gap-2">
            <p class="text-xs font-semibold text-gray-700 dark:text-gray-200">Latest Token: {preview.connectionName}</p>
            <button class="ds-btn-outline px-2.5 py-1.5" onclick={() => copyText(preview.token, 'Tunnel token')}>
              <Copy size={12} />
              Copy token
            </button>
          </div>
          <pre class="text-[11px] p-2 rounded border border-gray-200 dark:border-gray-800 bg-gray-100 dark:bg-gray-900 overflow-x-auto">{preview.token}</pre>
          {#if preview.connectCmd}
            <div>
              <p class="text-[11px] text-gray-500 mb-1">Connect command</p>
              <div class="flex gap-2">
                <pre class="flex-1 text-[11px] p-2 rounded border border-gray-200 dark:border-gray-800 bg-gray-100 dark:bg-gray-900 overflow-x-auto">{preview.connectCmd}</pre>
                <button class="ds-btn-outline px-2.5 py-1.5" onclick={() => copyText(preview.connectCmd, 'Connect command')}>
                  <Copy size={12} />
                </button>
              </div>
            </div>
          {/if}
          {#if preview.serviceCmd}
            <div>
              <p class="text-[11px] text-gray-500 mb-1">Service command</p>
              <div class="flex gap-2">
                <pre class="flex-1 text-[11px] p-2 rounded border border-gray-200 dark:border-gray-800 bg-gray-100 dark:bg-gray-900 overflow-x-auto">{preview.serviceCmd}</pre>
                <button class="ds-btn-outline px-2.5 py-1.5" onclick={() => copyText(preview.serviceCmd, 'Service command')}>
                  <Copy size={12} />
                </button>
              </div>
            </div>
          {/if}
        </div>
      {/if}

      {#if tunnelsLoading}
        <div class="flex items-center justify-center py-12"><Spinner /></div>
      {:else if tunnels.length === 0}
        <p class="text-sm text-gray-500">No tunnels configured</p>
      {:else}
        <div class="ds-table-wrap">
          <table class="ds-table">
            <thead>
              <tr class="ds-table-head-row">
                <th class="ds-table-th">Name</th>
                <th class="ds-table-th">ID</th>
                <th class="ds-table-th">Status</th>
                <th class="ds-table-th">Host</th>
                <th class="ds-table-th">Created</th>
                <th class="ds-table-th-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              {#each tunnels as conn}
                <tr class="ds-table-row">
                  <td class="ds-td-strong">{conn.name}</td>
                  <td class="ds-td-mono">{conn.id}</td>
                  <td class="ds-td">
                    <span class="inline-flex items-center gap-2">
                      <span class="w-2 h-2 rounded-full {conn.online ? 'bg-green-500' : 'bg-gray-400'}"></span>
                      <span class={conn.online ? 'text-green-600' : 'text-gray-500'}>{conn.online ? 'Online' : 'Offline'}</span>
                    </span>
                  </td>
                  <td class="ds-td-mono">{conn.host_info?.hostname || '—'}</td>
                  <td class="ds-td-mono whitespace-nowrap">{formatTime(conn.created_at)}</td>
                  <td class="ds-td-right">
                    <div class="flex justify-end gap-2">
                      <button class="ds-btn-outline px-2.5 py-1.5" onclick={() => viewTunnelToken(conn)}>Token</button>
                      <button class="ds-btn-outline px-2.5 py-1.5" onclick={() => regenerateTunnelToken(conn)}>Regenerate</button>
                      <button
                        class="ds-btn-outline px-2.5 py-1.5 border-red-300/80 text-red-600 hover:text-red-700 hover:border-red-500"
                        onclick={() => requestDeleteTunnel(conn)}
                      >
                        <Trash2 size={12} />
                        Delete
                      </button>
                    </div>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}

    {:else if activeTab === 'users'}
      {#if usersLoading}
        <div class="flex items-center justify-center py-12"><Spinner /></div>
      {:else}
        <div class="flex items-center gap-2 mb-2">
          <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-300">Application Users</h2>
          <HelpTip text="Admin access is explicit in CH-UI (role override). ClickHouse grants alone do not grant Admin UI actions. Safety rule: the last admin override cannot be removed." />
        </div>
        {#if !usersSyncCheck}
          <div class="mb-2 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-300">
            User sync check is unavailable right now (connection offline or auth issue). Application users list may include stale session users.
          </div>
        {/if}
        {#if users.length === 0}
          <p class="text-sm text-gray-500 mb-4">No users found</p>
        {:else}
	          <div class="ds-table-wrap mb-6">
	            <table class="ds-table">
              <thead>
                <tr class="ds-table-head-row">
                  <th class="ds-table-th">Username</th>
                  <th class="ds-table-th">Role</th>
                  <th class="ds-table-th">Last Login</th>
                  <th class="ds-table-th-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                {#each users as user}
                  <tr class="ds-table-row">
	                    <td class="ds-td-mono">{user.username}</td>
	                    <td class="ds-td">
                      <div class="inline-flex items-center rounded-lg border border-gray-300/80 dark:border-gray-700/80 bg-gray-100/70 dark:bg-gray-900/65 p-1">
                        {#each roleOptions as roleOpt}
                          <button
                            type="button"
                            class="px-2.5 h-7 rounded-md text-xs transition-colors disabled:opacity-60 disabled:cursor-not-allowed
                              {(userRoles[user.username] ?? user.user_role ?? 'viewer') === roleOpt.value
                                ? 'bg-orange-100 dark:bg-orange-500/15 text-ch-orange'
                                : 'text-gray-500 hover:text-gray-700 dark:hover:text-gray-300'}"
                            disabled={(userRoles[user.username] ?? user.user_role ?? 'viewer') === roleOpt.value || roleSavingUser === user.username}
                            onclick={() => setRole(user.username, roleOpt.value)}
                          >
                            {roleOpt.label}
                          </button>
                        {/each}
                      </div>
                    </td>
	                    <td class="ds-td-mono">{user.last_login ? formatTime(user.last_login) : '—'}</td>
	                    <td class="ds-td-right">
                      {#if userRoles[user.username]}
                        <button
                          class="text-xs text-red-500 hover:text-red-700"
                          onclick={() => removeRole(user.username)}
                        >Remove Override</button>
                      {/if}
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}

        <div class="flex flex-col md:flex-row md:items-center md:justify-between gap-2 mb-2">
          <div class="flex items-center gap-2">
            <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-300">ClickHouse Users</h2>
            <HelpTip text="Direct user management in ClickHouse. Create users, rotate passwords, and remove users without leaving CH-UI." />
          </div>
          <div class="flex items-center gap-2">
            <button class="ds-btn-outline" onclick={() => refreshUsersTab()} title="Refresh">
              <RefreshCw size={14} />
            </button>
            <button class="ds-btn-primary" onclick={() => openCreateCHUserSheet()}>
              <UserPlus size={14} />
              New User
            </button>
          </div>
        </div>

        {#if chUsers.length === 0}
          <div class="ds-empty">
            <p class="text-sm text-gray-500 mb-2">No ClickHouse users loaded</p>
            <button class="ds-btn-primary" onclick={() => openCreateCHUserSheet()}>
              <UserPlus size={14} />
              Create First User
            </button>
          </div>
        {:else}
          <div class="ds-table-wrap rounded border border-gray-200 dark:border-gray-800 max-h-[42vh] overflow-auto">
            <table class="ds-table min-w-[980px]">
              <thead>
                <tr class="ds-table-head-row sticky top-0 bg-gray-50 dark:bg-gray-900 z-10">
                  <th class="ds-table-th">Name</th>
                  <th class="ds-table-th">Auth Type</th>
                  <th class="ds-table-th">Storage</th>
                  <th class="ds-table-th">Default Roles</th>
                  <th class="ds-table-th-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                {#each chUsers as row}
                  <tr class="ds-table-row">
                    <td class="ds-td-mono">{row.name}</td>
                    <td class="ds-td-mono">{row.auth_type ?? '—'}</td>
                    <td class="ds-td-mono">{row.storage ?? '—'}</td>
                    <td class="ds-td-mono truncate max-w-xs">{formatCHDefaultRoles(row)}</td>
                    <td class="ds-td-right">
                      <div class="flex justify-end gap-2">
                        <button
                          class="ds-btn-outline px-2.5 py-1.5"
                          onclick={() => openUpdateCHUserPasswordSheet(row.name, row.auth_type)}
                        >
                          <KeyRound size={12} />
                          Change Password
                        </button>
                        <button
                          class="ds-btn-outline px-2.5 py-1.5 border-red-300/80 text-red-600 hover:text-red-700 hover:border-red-500"
                          onclick={() => openDeleteCHUserConfirm(row.name)}
                        >
                          <Trash2 size={12} />
                          Delete
                        </button>
                      </div>
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      {/if}

    {:else if activeTab === 'brain'}
      {#if brainLoading}
        <div class="flex items-center justify-center py-12"><Spinner /></div>
      {:else}
        <div class="flex flex-col md:flex-row md:items-center md:justify-between gap-3 mb-4">
          <div class="flex items-center gap-2">
            <Brain size={16} class="text-ch-blue" />
            <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-300">Brain Control Center</h2>
            <HelpTip text="Manage AI providers and model availability for all users. Use provider accordions to keep large model lists manageable." />
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <button class="ds-btn-outline" onclick={() => providerSheetOpen = true}>Add Provider</button>
            <button class="ds-btn-outline" onclick={() => openSkillSheet()}>Edit Global Skill</button>
            <button class="ds-btn-outline" onclick={() => loadBrainAdmin()} title="Refresh">
              <RefreshCw size={14} />
            </button>
          </div>
        </div>

        <div class="grid grid-cols-2 md:grid-cols-4 gap-2 mb-4">
          <div class="ds-panel p-2.5">
            <div class="text-[11px] text-gray-500">Providers</div>
            <div class="text-lg font-semibold text-gray-900 dark:text-gray-100">{brainProviders.length}</div>
          </div>
          <div class="ds-panel p-2.5">
            <div class="text-[11px] text-gray-500">Active Providers</div>
            <div class="text-lg font-semibold text-gray-900 dark:text-gray-100">{brainProviders.filter(p => p.is_active).length}</div>
          </div>
          <div class="ds-panel p-2.5">
            <div class="text-[11px] text-gray-500">Models</div>
            <div class="text-lg font-semibold text-gray-900 dark:text-gray-100">{brainModels.length}</div>
          </div>
          <div class="ds-panel p-2.5">
            <div class="text-[11px] text-gray-500">Active Models</div>
            <div class="text-lg font-semibold text-gray-900 dark:text-gray-100">{brainModels.filter(m => m.is_active).length}</div>
          </div>
        </div>

        {#if brainProviders.length === 0}
          <div class="ds-empty">
            <p class="text-sm text-gray-500 mb-2">No Brain providers configured yet.</p>
            <button class="ds-btn-primary" onclick={() => providerSheetOpen = true}>Create First Provider</button>
          </div>
        {:else}
          <div class="ds-table-wrap mb-5 max-h-[32vh] overflow-auto rounded-lg border border-gray-200 dark:border-gray-800">
            <table class="ds-table">
              <thead>
                <tr class="ds-table-head-row sticky top-0 bg-gray-50 dark:bg-gray-900 z-10">
                  <th class="ds-table-th">Provider</th>
                  <th class="ds-table-th">Kind</th>
                  <th class="ds-table-th">Base URL</th>
                  <th class="ds-table-th">Key</th>
                  <th class="ds-table-th">Active</th>
                  <th class="ds-table-th">Default</th>
                  <th class="ds-table-th-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                {#each brainProviders as provider}
                  <tr class="ds-table-row">
                    <td class="ds-td-strong">{provider.name}</td>
                    <td class="ds-td-mono">{provider.kind}</td>
                    <td class="ds-td-mono max-w-sm truncate">{provider.base_url || '—'}</td>
                    <td class="ds-td">{provider.has_api_key ? 'Configured' : 'Missing'}</td>
                    <td class="ds-td">
                      <input
                        type="checkbox"
                        class="ds-checkbox"
                        checked={provider.is_active}
                        onchange={(e) => toggleProvider(provider, 'is_active', (e.target as HTMLInputElement).checked)}
                      />
                    </td>
                    <td class="ds-td">
                      <input
                        type="radio"
                        class="ds-radio"
                        name="default-brain-provider"
                        checked={provider.is_default}
                        onchange={() => toggleProvider(provider, 'is_default', true)}
                      />
                    </td>
                    <td class="ds-td-right">
                      <div class="flex justify-end gap-2">
                        <button class="ds-btn-outline" onclick={() => syncProviderModels(provider)}>Sync Models</button>
                        <button class="text-xs text-red-500 hover:text-red-700" onclick={() => deleteProvider(provider)}>Delete</button>
                      </div>
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}

        <div class="ds-panel p-3 mb-3">
          <div class="flex items-center gap-2 mb-2">
            <h3 class="text-sm font-semibold text-gray-800 dark:text-gray-200">Brain Models</h3>
            <HelpTip text="Models are grouped by provider. Expand a provider accordion to activate/deactivate models and choose defaults." />
          </div>
          <div class="grid grid-cols-1 md:grid-cols-4 gap-2">
            <Combobox
              options={providerFilterOptions()}
              value={modelProviderFilter}
              onChange={(v) => modelProviderFilter = v}
            />
            <input
              class="ds-input-sm md:col-span-2"
              placeholder="Search models..."
              bind:value={modelSearch}
            />
            <label class="ds-checkbox-label text-xs px-2">
              <input type="checkbox" class="ds-checkbox" bind:checked={modelShowOnlyActive} />
              Show only active
            </label>
          </div>
          <div class="mt-2 flex flex-wrap items-center gap-2">
            <button class="ds-btn-outline" onclick={() => runModelBulkAction('activate_recommended')}>Activate Recommended</button>
            <button class="ds-btn-outline" onclick={() => runModelBulkAction('activate_all')}>Activate All</button>
            <button class="ds-btn-outline" onclick={() => runModelBulkAction('deactivate_all')}>Deactivate All</button>
          </div>
        </div>

        {#if brainModels.length === 0}
          <p class="text-sm text-gray-500 mb-6">No models synced yet.</p>
        {:else}
          <div class="space-y-2 mb-6">
            {#each visibleProvidersForModels() as provider}
              {@const providerModels = modelsForProvider(provider.id)}
              <details class="ds-card overflow-hidden" open={modelProviderFilter === provider.id || (!modelProviderFilter && provider.is_default)}>
                <summary class="cursor-pointer list-none px-3 py-2.5 flex items-center justify-between bg-gray-50 dark:bg-gray-900">
                  <div class="flex items-center gap-2">
                    <span class="font-medium text-gray-900 dark:text-gray-100">{provider.name}</span>
                    <span class="text-[11px] text-gray-500">{provider.kind}</span>
                  </div>
                  <div class="flex items-center gap-2 text-xs">
                    <span class="ds-badge ds-badge-neutral">{providerModels.filter(m => m.is_active).length} active</span>
                    <span class="text-gray-500">{providerModels.length} total</span>
                  </div>
                </summary>
                <div class="max-h-[36vh] overflow-auto border-t border-gray-200 dark:border-gray-800">
                  {#if providerModels.length > 0}
                    <table class="ds-table">
                      <thead>
                        <tr class="ds-table-head-row sticky top-0 bg-gray-50 dark:bg-gray-900 z-10">
                          <th class="ds-table-th">Model</th>
                          <th class="ds-table-th">Active</th>
                          <th class="ds-table-th">Default</th>
                        </tr>
                      </thead>
                      <tbody>
                        {#each providerModels as model}
                          <tr class="ds-table-row">
                            <td class="ds-td-mono">{model.display_name || model.name}</td>
                            <td class="ds-td">
                              <input
                                type="checkbox"
                                class="ds-checkbox"
                                checked={model.is_active}
                                onchange={(e) => updateModel(model, 'is_active', (e.target as HTMLInputElement).checked)}
                              />
                            </td>
                            <td class="ds-td">
                              <input
                                type="radio"
                                class="ds-radio"
                                name={"default-model-" + model.provider_id}
                                checked={model.is_default}
                                onchange={() => updateModel(model, 'is_default', true)}
                              />
                            </td>
                          </tr>
                        {/each}
                      </tbody>
                    </table>
                  {:else}
                    <p class="text-xs text-gray-500 px-3 py-4">No models match current filters for this provider.</p>
                  {/if}
                </div>
              </details>
            {/each}
          </div>
        {/if}

        <div class="ds-card p-3">
          <div class="flex items-center justify-between mb-2">
            <h3 class="text-sm font-semibold text-gray-800 dark:text-gray-200">Global Brain Skill</h3>
            <button class="ds-btn-outline" onclick={() => openSkillSheet()}>Open Skill Sheet</button>
          </div>
          <p class="text-xs text-gray-500 mb-2">Active prompt preview</p>
          <pre class="text-[11px] leading-relaxed whitespace-pre-wrap text-gray-600 dark:text-gray-300 max-h-36 overflow-auto rounded border border-gray-200 dark:border-gray-800 bg-gray-50 dark:bg-gray-900 p-2">{truncate(skillForm.content || '', 1200)}</pre>
        </div>
      {/if}
    {/if}
  </div>
</div>
