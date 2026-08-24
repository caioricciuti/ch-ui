<script lang="ts">
  import { onMount } from 'svelte'
  import MCPKeysPanel from '../lib/components/admin/MCPKeysPanel.svelte'
  import type { AdminStats } from '../lib/types/api'
  import { apiGet, apiPut, apiDel, apiPost, ApiError } from '../lib/api/client'
  import { formatDate } from '../lib/utils/format'
  import { fetchClusterInfo, fetchNodeInfo } from '../lib/api/query'
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
  import { Shield, RefreshCw, Users, Database, Activity, LogIn, ChevronDown, ChevronRight, Brain, UserPlus, KeyRound, Trash2, Plus, Copy, GitBranch, CloudDownload, Check, X as XIcon } from 'lucide-svelte'
  import { isProActive } from '../lib/stores/license.svelte'
  import { getSession } from '../lib/stores/session.svelte'
  import { getGitHubIntegration, saveGitHubIntegration, deleteGitHubIntegration, testGitHubConnection, triggerGitHubSync, getGitHubSyncLogs } from '../lib/api/github'
  import type { GitHubIntegration, GitHubSyncLog } from '../lib/types/models'
  import { fetchRetentionSettings, updateRetentionSettings } from '../lib/api/retention'
  import type { RetentionConfig, RetentionSettings } from '../lib/api/retention'
  import { fetchSSOSettings, updateSSOSettings } from '../lib/api/sso'
  import type { SSOSettings } from '../lib/api/sso'

  // Tab state
  type AdminTab = 'overview' | 'tunnels' | 'users' | 'brain' | 'github' | 'mcp'
  const adminTabIds: AdminTab[] = ['overview', 'tunnels', 'users', 'brain', 'github', 'mcp']
  let activeTab = $state<AdminTab>('overview')

  type TunnelConnection = {
    id: string
    name: string
    type?: 'direct' | 'tunnel'
    clickhouse_url?: string
    is_embedded?: boolean
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

  // Cluster info
  type ClusterNode = { shard_num: number; replica_num: number; host_name: string; host_address: string; port: number; is_local: number }
  type ClusterDetail = { name: string; shards: number; replicas: number; total_nodes: number; nodes: ClusterNode[] }
  let clusterInfo = $state<{ is_cluster: boolean; clusters: ClusterDetail[] }>({ is_cluster: false, clusters: [] })
  let currentNode = $state<Record<string, unknown> | null>(null)
  let clusterLoading = $state(false)

  // Connections (direct + tunnel)
  let tunnels = $state<TunnelConnection[]>([])
  let tunnelsLoading = $state(false)
  let tunnelCreateName = $state('')
  let tunnelCreateMode = $state<'direct' | 'tunnel'>('direct')
  let tunnelCreateUrl = $state('')
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
  let usersError = $state<string | null>(null)
  let chUsersError = $state<string | null>(null)
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
  let deletingProvider = $state<BrainProviderAdmin | null>(null)

  const roleOptions: ComboboxOption[] = [
    { value: 'admin', label: 'admin' },
    { value: 'analyst', label: 'analyst' },
    { value: 'viewer', label: 'viewer' },
  ]

  const providerKindOptions: ComboboxOption[] = [
    { value: 'openai', label: 'OpenAI' },
    { value: 'openai_compatible', label: 'OpenAI Compatible' },
    { value: 'ollama', label: 'Ollama (local)' },
  ]
  const providerKindDescriptions: Record<string, string> = {
    openai: 'Official OpenAI API — GPT-4o, o3, etc.',
    openai_compatible: 'Any provider with an OpenAI-compatible API (Together, Groq, Azure, etc.)',
    ollama: 'Local Ollama instance — no API key needed',
  }
  const providerBaseUrls: Record<string, string> = {
    openai: 'https://api.openai.com/v1',
    openai_compatible: '',
    ollama: 'http://localhost:11434',
  }
  const clickHouseAuthTypeOptions: ComboboxOption[] = [
    { value: 'sha256_password', label: 'sha256_password' },
    { value: 'plaintext_password', label: 'plaintext_password' },
    { value: 'double_sha1_password', label: 'double_sha1_password' },
    { value: 'no_password', label: 'no_password' },
  ]
  let providerForm = $state({
    name: '',
    kind: 'openai',
    baseUrl: 'https://api.openai.com/v1',
    apiKey: '',
    isActive: true,
    isDefault: false,
  })
  let providerCreating = $state(false)
  let providerError = $state('')
  let skillForm = $state({
    name: 'Default Brain Skill',
    content: '',
    isActive: true,
    isDefault: true,
  })

  // GitHub integration state
  let ghIntegration = $state<GitHubIntegration | null>(null)
  let ghLoading = $state(false)
  let ghSaving = $state(false)
  let ghTesting = $state(false)
  let ghTestResult = $state<{ success: boolean; error?: string } | null>(null)
  let ghSyncing = $state(false)
  let ghSyncLogs = $state<GitHubSyncLog[]>([])
  let ghForm = $state({ repo: '', branch: 'main', path: 'models/', pat: '' })

  async function loadGitHubTab() {
    const session = getSession()
    if (!session) return
    ghLoading = true
    try {
      const [integration, logs] = await Promise.all([
        getGitHubIntegration(session.connectionId),
        getGitHubSyncLogs(session.connectionId).catch(() => []),
      ])
      ghIntegration = integration
      ghSyncLogs = logs
      if (integration) {
        ghForm.repo = integration.repo
        ghForm.branch = integration.branch
        ghForm.path = integration.path
        ghForm.pat = ''
      }
    } catch (e: unknown) {
      toastError((e as Error).message || 'Failed to load GitHub integration')
    } finally {
      ghLoading = false
    }
  }

  async function handleGitHubSave() {
    const session = getSession()
    if (!session) return
    ghSaving = true
    try {
      await saveGitHubIntegration(session.connectionId, {
        repo: ghForm.repo,
        branch: ghForm.branch,
        path: ghForm.path,
        pat: ghForm.pat || undefined,
      })
      toastSuccess('GitHub integration saved')
      ghForm.pat = ''
      await loadGitHubTab()
    } catch (e: unknown) {
      toastError((e as Error).message || 'Failed to save')
    } finally {
      ghSaving = false
    }
  }

  async function handleGitHubTest() {
    const session = getSession()
    if (!session) return
    ghTesting = true
    ghTestResult = null
    try {
      ghTestResult = await testGitHubConnection(session.connectionId)
    } catch (e: unknown) {
      ghTestResult = { success: false, error: (e as Error).message }
    } finally {
      ghTesting = false
    }
  }

  async function handleGitHubSync() {
    const session = getSession()
    if (!session) return
    ghSyncing = true
    try {
      const result = await triggerGitHubSync(session.connectionId)
      const parts: string[] = []
      if (result.created > 0) parts.push(`${result.created} created`)
      if (result.updated > 0) parts.push(`${result.updated} updated`)
      if (result.deleted > 0) parts.push(`${result.deleted} deleted`)
      toastSuccess(parts.length > 0 ? `Sync: ${parts.join(', ')}` : 'Already up to date')
      await loadGitHubTab()
    } catch (e: unknown) {
      toastError((e as Error).message || 'Sync failed')
    } finally {
      ghSyncing = false
    }
  }

  async function handleGitHubDelete() {
    const session = getSession()
    if (!session) return
    try {
      await deleteGitHubIntegration(session.connectionId)
      ghIntegration = null
      ghForm = { repo: '', branch: 'main', path: 'models/', pat: '' }
      toastSuccess('GitHub integration removed')
    } catch (e: unknown) {
      toastError((e as Error).message || 'Failed to remove')
    }
  }

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

  async function loadClusterInfo() {
    clusterLoading = true
    try {
      const [cluster, node] = await Promise.all([fetchClusterInfo(), fetchNodeInfo()])
      clusterInfo = cluster
      currentNode = node.node ?? null
    } catch {
      clusterInfo = { is_cluster: false, clusters: [] }
      currentNode = null
    } finally {
      clusterLoading = false
    }
  }

  const isAdminRole = $derived(getSession()?.role === 'admin')

  onMount(() => {
    if (!isAdminRole) return
    loadStats()
    loadConnections()
    loadClusterInfo()
    loadRetention()
    loadSSO()
    const initialTab = normalizeAdminTab(
      typeof window === 'undefined' ? null : new URLSearchParams(window.location.search).get('tab'),
    )
    switchTab(initialTab, true)
  })

  // Data retention (SQLite history pruning)
  const retentionTables: Array<{ key: keyof RetentionConfig; label: string }> = [
    { key: 'audit_logs', label: 'Audit logs' },
    { key: 'alert_events', label: 'Alert events' },
    { key: 'alert_dispatch_jobs', label: 'Alert dispatch jobs' },
    { key: 'schedule_runs', label: 'Schedule runs' },
    { key: 'pipeline_runs', label: 'Pipeline runs' },
    { key: 'pipeline_run_logs', label: 'Pipeline run logs' },
    { key: 'model_runs', label: 'Model runs' },
    { key: 'model_run_results', label: 'Model run results' },
    { key: 'github_sync_logs', label: 'GitHub sync logs' },
    { key: 'gov_schema_changes', label: 'Schema changes' },
  ]
  let retention = $state<RetentionSettings | null>(null)
  let retentionLoading = $state(false)
  let retentionSaving = $state(false)

  async function loadRetention() {
    retentionLoading = true
    try {
      retention = await fetchRetentionSettings()
    } catch (e: any) {
      toastError(e.message)
    } finally {
      retentionLoading = false
    }
  }

  async function saveRetention() {
    if (!retention) return
    retentionSaving = true
    try {
      retention = await updateRetentionSettings(retention.config)
      toastSuccess('Retention settings saved')
    } catch (e: any) {
      toastError(e.message)
    } finally {
      retentionSaving = false
    }
  }

  // SSO (OIDC) configuration
  let sso = $state<SSOSettings | null>(null)
  let ssoLoading = $state(false)
  let ssoSaving = $state(false)
  let ssoProRequired = $state(false)
  let ssoSecret = $state('')
  let ssoForm = $state({
    enabled: false,
    issuer_url: '',
    client_id: '',
    redirect_base_url: '',
    admin_groups: '',
    analyst_groups: '',
    viewer_groups: '',
    allowed_domains: '',
  })

  function splitCsv(value: string): string[] {
    return value
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s.length > 0)
  }

  function applySSOSettings(next: SSOSettings) {
    sso = next
    ssoForm = {
      enabled: next.config.enabled,
      issuer_url: next.config.issuer_url,
      client_id: next.config.client_id,
      redirect_base_url: next.config.redirect_base_url,
      admin_groups: next.config.admin_groups.join(', '),
      analyst_groups: next.config.analyst_groups.join(', '),
      viewer_groups: next.config.viewer_groups.join(', '),
      allowed_domains: next.config.allowed_domains.join(', '),
    }
    ssoSecret = ''
  }

  async function loadSSO() {
    ssoLoading = true
    try {
      applySSOSettings(await fetchSSOSettings())
      ssoProRequired = false
    } catch (e: any) {
      if (String(e?.message ?? '').toLowerCase().includes('pro license')) {
        ssoProRequired = true
      } else {
        toastError(e.message)
      }
    } finally {
      ssoLoading = false
    }
  }

  async function saveSSO() {
    ssoSaving = true
    try {
      const next = await updateSSOSettings({
        enabled: ssoForm.enabled,
        issuer_url: ssoForm.issuer_url.trim(),
        client_id: ssoForm.client_id.trim(),
        client_secret: ssoSecret.trim() || undefined,
        redirect_base_url: ssoForm.redirect_base_url.trim(),
        admin_groups: splitCsv(ssoForm.admin_groups),
        analyst_groups: splitCsv(ssoForm.analyst_groups),
        viewer_groups: splitCsv(ssoForm.viewer_groups),
        allowed_domains: splitCsv(ssoForm.allowed_domains),
      })
      applySSOSettings(next)
      if (next.reload_error) {
        toastError(`Saved, but the SSO provider could not be initialized: ${next.reload_error}`)
      } else {
        toastSuccess('SSO settings saved and applied — no restart needed')
      }
    } catch (e: any) {
      toastError(e.message)
    } finally {
      ssoSaving = false
    }
  }

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

  // Turns a failed users-API call into copy that names the actual problem
  // instead of a generic "connection offline" guess.
  function describeUsersError(e: unknown): string {
    const message = e instanceof Error ? e.message : String(e)
    const status = e instanceof ApiError ? e.status : 0
    if (status === 403) return 'You need the CH-UI admin role to manage users.'
    if (/access_denied|not enough privileges|grant show users/i.test(message)) {
      return 'Your ClickHouse user lacks the SHOW USERS grant — ask your ClickHouse admin for it (GRANT SHOW USERS ON *.*).'
    }
    if (status === 503 || /tunnel.*offline|connection.*offline/i.test(message)) {
      return 'The connection tunnel is offline — bring the agent back online and refresh.'
    }
    return message
  }

  async function loadUsers() {
    usersLoading = true
    usersError = null
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
    } catch (e: unknown) {
      usersError = describeUsersError(e)
    } finally {
      usersLoading = false
    }
  }

  async function refreshUsersTab() {
    await Promise.all([loadUsers(), loadClickHouseUsers()])
  }

  async function loadClickHouseUsers() {
    chUsersError = null
    try {
      const res = await apiGet<{ data: any[]; meta: any[] }>('/api/admin/clickhouse-users')
      chUsers = res.data ?? []
    } catch (e: unknown) {
      chUsersError = describeUsersError(e)
    }
  }

  // ClickHouse >= 24.9 returns auth_type as Array(Enum8) (multiple auth methods),
  // older versions return a scalar Enum8 string. Normalize both shapes.
  const CH_PASSWORD_AUTH_TYPES = [
    'no_password',
    'plaintext_password',
    'sha256_password',
    'double_sha1_password',
  ]

  function formatCHAuthType(value: unknown): string {
    if (Array.isArray(value)) {
      const parts = value.filter(v => typeof v === 'string' && v.trim())
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

  function openUpdateCHUserPasswordSheet(username: string, authType: unknown) {
    selectedCHUserName = username
    updateCHUserPasswordForm = {
      authType: normalizeCHAuthTypeForForm(authType),
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
      toastError('Connection name is required')
      return
    }
    const url = tunnelCreateUrl.trim()
    if (tunnelCreateMode === 'direct' && !url) {
      toastError('ClickHouse URL is required for a direct connection')
      return
    }
    tunnelCreateLoading = true
    try {
      const body =
        tunnelCreateMode === 'direct'
          ? { name, type: 'direct', clickhouse_url: url }
          : { name }
      const res = await apiPost<TunnelTokenResponse>('/api/connections', body)
      toastSuccess(
        tunnelCreateMode === 'direct'
          ? `Connection "${name}" created and connecting to ${url}`
          : `Tunnel "${name}" created`
      )
      tunnelCreateName = ''
      tunnelCreateUrl = ''
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
    if (tab === 'github' && !ghLoading && !ghIntegration) {
      loadGitHubTab()
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


  function resetProviderForm() {
    providerForm = { name: '', kind: 'openai', baseUrl: 'https://api.openai.com/v1', apiKey: '', isActive: true, isDefault: false }
    providerError = ''
    providerCreating = false
  }

  async function createProvider() {
    providerError = ''
    providerCreating = true
    try {
      await adminCreateBrainProvider(providerForm)
      toastSuccess('Brain provider created')
      resetProviderForm()
      providerSheetOpen = false
      await loadBrainAdmin()
    } catch (e: any) {
      providerError = e.message || 'Failed to create provider'
    } finally {
      providerCreating = false
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

  function deleteProvider(provider: BrainProviderAdmin) {
    deletingProvider = provider
  }

  async function confirmDeleteProvider() {
    if (!deletingProvider) return
    const provider = deletingProvider
    deletingProvider = null
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

  function truncate(s: string, max = 80): string {
    return s.length > max ? s.slice(0, max) + '...' : s
  }
</script>

{#if !isAdminRole}
  <div class="flex flex-col items-center justify-center h-full gap-3 px-6 text-center">
    <Shield size={28} class="text-gray-400" />
    <h1 class="ds-page-title">Admin access required</h1>
    <p class="text-sm text-gray-500 dark:text-gray-400 max-w-md">
      You are signed in as <span class="font-semibold">{getSession()?.role ?? 'viewer'}</span>.
      Ask a CH-UI admin to grant you the admin role (Admin &rarr; Users), then log in again.
    </p>
  </div>
{:else}
<div class="flex flex-col h-full">
  <div class="border-b border-gray-200 dark:border-gray-800">
    <div class="flex flex-col gap-2 px-4 py-3 md:flex-row md:items-center md:gap-4">
      <div class="flex items-center gap-3">
        <Shield size={18} class="text-ch-blue" />
        <h1 class="ds-page-title">Admin Panel</h1>
      </div>
      <nav class="ds-tabs border-0 px-0 pt-0 gap-1 overflow-x-auto whitespace-nowrap" aria-label="Admin Tabs">
        {#each [['overview', 'Overview'], ['tunnels', 'Connections'], ['users', 'Users'], ['brain', 'Brain'], ['github', 'GitHub'], ['mcp', 'MCP Server']] as [key, label]}
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
  title="Add AI Provider"
  size="lg"
  onclose={() => { providerSheetOpen = false; resetProviderForm() }}
>
  <form
    class="space-y-5"
    onsubmit={(e) => {
      e.preventDefault()
      void createProvider()
    }}
  >
    <!-- Kind selection -->
    <div>
      <span class="ds-form-label">Provider Type</span>
      <div class="grid grid-cols-3 gap-2 mt-1">
        {#each providerKindOptions as opt}
          <button
            type="button"
            class="text-left rounded-lg border p-3 transition-all {providerForm.kind === opt.value
              ? 'border-ch-blue bg-orange-50 dark:bg-orange-950/20 ring-1 ring-ch-blue/30'
              : 'border-gray-200 dark:border-gray-700 hover:border-gray-300 dark:hover:border-gray-600'}"
            onclick={() => { providerForm = { ...providerForm, kind: opt.value, baseUrl: providerBaseUrls[opt.value] ?? '' }; providerError = '' }}
          >
            <span class="text-sm font-medium text-gray-900 dark:text-gray-100">{opt.label}</span>
            <p class="text-[11px] text-gray-500 mt-0.5 leading-snug">{providerKindDescriptions[opt.value]}</p>
          </button>
        {/each}
      </div>
    </div>

    <!-- Connection details -->
    <div class="space-y-3">
      <label class="block space-y-1">
        <span class="ds-form-label">Display Name</span>
        <input class="ds-input-sm" placeholder={providerForm.kind === 'ollama' ? 'Local Ollama' : providerForm.kind === 'openai' ? 'OpenAI' : 'My Provider'} bind:value={providerForm.name} required />
      </label>

      <label class="block space-y-1">
        <span class="ds-form-label">Base URL</span>
        <input
          class="ds-input-sm font-mono text-xs"
          placeholder={providerForm.kind === 'openai_compatible' ? 'https://your-gateway.example.com/v1' : ''}
          bind:value={providerForm.baseUrl}
        />
        {#if providerForm.kind !== 'openai_compatible'}
          <p class="text-[11px] text-gray-400 mt-1">Pre-filled for {providerKindOptions.find(o => o.value === providerForm.kind)?.label}. Change only if using a proxy.</p>
        {:else}
          <p class="text-[11px] text-gray-400 mt-1">The OpenAI-compatible /v1 endpoint of your provider.</p>
        {/if}
      </label>

      {#if providerForm.kind !== 'ollama'}
        <label class="block space-y-1">
          <span class="ds-form-label">API Key</span>
          <input class="ds-input-sm font-mono text-xs" type="password" placeholder="sk-..." bind:value={providerForm.apiKey} />
        </label>
      {:else}
        <div class="ds-panel-muted p-3 rounded-lg">
          <p class="text-xs text-gray-500">Ollama runs locally — no API key needed. Make sure Ollama is running on the configured URL.</p>
        </div>
      {/if}
    </div>

    <!-- Options -->
    <div class="flex items-center gap-5 pt-1">
      <label class="ds-checkbox-label text-xs">
        <input type="checkbox" class="ds-checkbox" bind:checked={providerForm.isActive} />
        Active
      </label>
      <label class="ds-checkbox-label text-xs">
        <input type="checkbox" class="ds-checkbox" bind:checked={providerForm.isDefault} />
        Set as default
      </label>
    </div>

    <!-- Error -->
    {#if providerError}
      <div class="rounded-lg bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-800 p-3">
        <p class="text-xs font-medium text-red-700 dark:text-red-300">{providerError}</p>
      </div>
    {/if}

    <!-- Actions -->
    <div class="flex items-center justify-end gap-2 pt-3 border-t border-gray-200 dark:border-gray-800">
      <button type="button" class="ds-btn-outline" onclick={() => { providerSheetOpen = false; resetProviderForm() }}>Cancel</button>
      <button type="submit" class="ds-btn-primary" disabled={!providerForm.name.trim() || providerCreating}>
        {providerCreating ? 'Creating...' : 'Create Provider'}
      </button>
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

<ConfirmDialog
  open={deletingProvider !== null}
  title="Delete provider?"
  description={deletingProvider ? `Delete "${deletingProvider.name}" and all its models? This cannot be undone.` : ''}
  confirmLabel="Delete Provider"
  destructive
  onconfirm={confirmDeleteProvider}
  oncancel={() => deletingProvider = null}
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

      <!-- Cluster Topology -->
      <div class="mt-6">
        <div class="flex items-center gap-2 mb-2">
          <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-300">Cluster Topology</h2>
          <button class="ds-btn-ghost p-1" onclick={() => loadClusterInfo()} title="Refresh cluster info">
            <RefreshCw size={12} />
          </button>
        </div>

        {#if clusterLoading}
          <div class="flex items-center justify-center py-6"><Spinner /></div>
        {:else if !clusterInfo.is_cluster}
          <div class="ds-panel p-3">
            <p class="text-sm text-gray-500">Single-node setup detected (no cluster configuration found).</p>
            {#if currentNode}
              <div class="flex items-center gap-2 mt-2">
                <span class="text-xs text-gray-400">Node:</span>
                <span class="text-xs font-mono text-gray-700 dark:text-gray-300">{currentNode.hostname}</span>
                <span class="text-[10px] text-gray-400">v{currentNode.version}</span>
              </div>
            {/if}
          </div>
        {:else}
          {#if currentNode}
            <div class="ds-panel p-3 mb-3 flex items-center gap-3">
              <div class="w-2 h-2 rounded-full bg-green-500 shrink-0"></div>
              <div>
                <span class="text-xs text-gray-400">Current node: </span>
                <span class="text-xs font-mono font-semibold text-gray-800 dark:text-gray-200">{currentNode.hostname}</span>
                <span class="text-[10px] ml-2 text-gray-400">v{currentNode.version}</span>
              </div>
            </div>
          {/if}

          {#each clusterInfo.clusters as cluster}
            <div class="ds-panel p-3 mb-2">
              <div class="flex items-center gap-2 mb-2">
                <span class="text-sm font-semibold text-gray-800 dark:text-gray-200">{cluster.name}</span>
                <span class="text-[10px] px-1.5 py-0.5 rounded bg-ch-blue/10 text-ch-blue font-medium">{cluster.shards} shard{cluster.shards !== 1 ? 's' : ''}</span>
                <span class="text-[10px] px-1.5 py-0.5 rounded bg-orange-100 dark:bg-orange-500/15 text-ch-orange font-medium">{cluster.replicas} replica{cluster.replicas !== 1 ? 's' : ''}</span>
                <span class="text-[10px] text-gray-400">{cluster.total_nodes} node{cluster.total_nodes !== 1 ? 's' : ''}</span>
              </div>
              <div class="ds-table-wrap">
                <table class="ds-table">
                  <thead>
                    <tr class="ds-table-head-row">
                      <th class="ds-table-th">Host</th>
                      <th class="ds-table-th">Address</th>
                      <th class="ds-table-th">Port</th>
                      <th class="ds-table-th">Shard</th>
                      <th class="ds-table-th">Replica</th>
                      <th class="ds-table-th">Local</th>
                    </tr>
                  </thead>
                  <tbody>
                    {#each cluster.nodes as node}
                      <tr class="ds-table-row {node.is_local ? 'bg-green-50/50 dark:bg-green-500/5' : ''}">
                        <td class="ds-td-mono font-semibold">{node.host_name}</td>
                        <td class="ds-td-mono">{node.host_address}</td>
                        <td class="ds-td-mono">{node.port}</td>
                        <td class="ds-td">{node.shard_num}</td>
                        <td class="ds-td">{node.replica_num}</td>
                        <td class="ds-td">
                          {#if node.is_local}
                            <span class="w-2 h-2 rounded-full bg-green-500 inline-block" title="This node"></span>
                          {:else}
                            <span class="text-gray-400">—</span>
                          {/if}
                        </td>
                      </tr>
                    {/each}
                  </tbody>
                </table>
              </div>
            </div>
          {/each}
        {/if}
      </div>

      <!-- Data retention -->
      <div class="mt-6">
        <div class="flex items-center gap-2 mb-2">
          <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-300">Data Retention</h2>
          <HelpTip text="History tables (audit logs, alert events, run logs...) are pruned automatically after the configured number of days to keep the metadata database small. 0 keeps rows forever." />
        </div>

        {#if retentionLoading}
          <div class="flex items-center justify-center py-6"><Spinner /></div>
        {:else if retention}
          <div class="ds-panel p-3">
            <div class="grid grid-cols-2 md:grid-cols-4 gap-3">
              {#each retentionTables as t}
                <label class="block space-y-1">
                  <span class="ds-form-label">{t.label}</span>
                  <div class="flex items-center gap-1.5">
                    <input
                      class="ds-input-sm w-20 tabular-nums"
                      type="number"
                      min="0"
                      max="3650"
                      bind:value={retention.config[t.key]}
                    />
                    <span class="text-[11px] text-gray-400">days</span>
                  </div>
                  {#if retention.last_run && (retention.last_run.rows_deleted[t.key] ?? 0) > 0}
                    <p class="text-[10px] text-gray-400">-{retention.last_run.rows_deleted[t.key]} rows last run</p>
                  {/if}
                </label>
              {/each}
            </div>
            <div class="flex flex-wrap items-center gap-3 mt-3 pt-3 border-t border-gray-200 dark:border-gray-800">
              <button class="ds-btn-primary" onclick={() => saveRetention()} disabled={retentionSaving}>
                {retentionSaving ? 'Saving...' : 'Save'}
              </button>
              {#if retention.last_run}
                <span class="text-xs text-gray-500">
                  Last cleanup: {new Date(retention.last_run.last_run_at).toLocaleString()}
                  · {retention.last_run.total_deleted} row{retention.last_run.total_deleted !== 1 ? 's' : ''} deleted
                  · {retention.last_run.duration_ms} ms
                </span>
                {#if retention.last_run.last_error}
                  <span class="text-xs text-red-600 dark:text-red-400">{retention.last_run.last_error}</span>
                {/if}
              {:else}
                <span class="text-xs text-gray-400">Cleanup has not run yet — it runs hourly.</span>
              {/if}
            </div>
          </div>
        {:else}
          <p class="text-sm text-gray-500">Could not load retention settings.</p>
        {/if}
      </div>

      <!-- SSO (OIDC) -->
      <div class="mt-6">
        <div class="flex items-center gap-2 mb-2">
          <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-300">Single Sign-On (OIDC)</h2>
          <HelpTip text="Let users sign in through your identity provider (Okta, Entra ID, Google, Keycloak). Users in an admin or analyst group get that role; everyone else is a viewer. Queries run via the connection's ClickHouse service account. Pro feature." />
          {#if sso}
            {#if sso.active}
              <span class="ds-badge ds-badge-success">Active</span>
            {:else}
              <span class="ds-badge ds-badge-neutral">Inactive</span>
            {/if}
          {/if}
        </div>

        {#if ssoProRequired || !isProActive()}
          <div class="ds-panel p-3 flex items-center gap-3">
            <KeyRound size={16} class="text-gray-400 shrink-0" />
            <p class="text-sm text-gray-500">SSO lets your team sign in through your identity provider. Requires a Pro license.</p>
          </div>
        {:else if ssoLoading}
          <div class="flex items-center justify-center py-6"><Spinner /></div>
        {:else if sso}
          <div class="ds-panel p-3 space-y-3">
            {#if sso.env_managed}
              <p class="text-xs px-2 py-1.5 rounded bg-amber-50 dark:bg-amber-500/10 text-amber-700 dark:text-amber-400">
                SSO is configured via environment variables / the server config file, which always take precedence over settings saved here.
                The values below are read-only — edit the server config (and restart) to change them.
              </p>
            {/if}

            <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
              <label class="block space-y-1">
                <span class="ds-form-label">Issuer URL</span>
                <input class="ds-input-sm w-full" type="text" placeholder="https://accounts.google.com"
                  bind:value={ssoForm.issuer_url} disabled={sso.env_managed} />
              </label>
              <label class="block space-y-1">
                <span class="ds-form-label">Client ID</span>
                <input class="ds-input-sm w-full" type="text" placeholder="your-client-id"
                  bind:value={ssoForm.client_id} disabled={sso.env_managed} />
              </label>
              <label class="block space-y-1">
                <span class="ds-form-label">
                  Client Secret
                  {#if sso.config.has_secret}
                    <span class="text-green-500 font-normal ml-1">(configured)</span>
                  {/if}
                </span>
                <input class="ds-input-sm w-full" type="password" autocomplete="off"
                  placeholder={sso.config.has_secret ? 'Leave blank to keep current secret' : 'client secret'}
                  bind:value={ssoSecret} disabled={sso.env_managed} />
              </label>
              <label class="block space-y-1">
                <span class="ds-form-label">Redirect Base URL</span>
                <input class="ds-input-sm w-full" type="text" placeholder="https://ch-ui.yourcompany.com (defaults to the server URL)"
                  bind:value={ssoForm.redirect_base_url} disabled={sso.env_managed} />
              </label>
              <label class="block space-y-1">
                <span class="ds-form-label">Admin Groups</span>
                <input class="ds-input-sm w-full" type="text" placeholder="ch-ui-admins, platform"
                  bind:value={ssoForm.admin_groups} disabled={sso.env_managed} />
              </label>
              <label class="block space-y-1">
                <span class="ds-form-label">Analyst Groups</span>
                <input class="ds-input-sm w-full" type="text" placeholder="data-analysts"
                  bind:value={ssoForm.analyst_groups} disabled={sso.env_managed} />
              </label>
              <label class="block space-y-1">
                <span class="ds-form-label">Viewer Groups</span>
                <input class="ds-input-sm w-full" type="text" placeholder="everyone (users in no mapped group are viewers)"
                  bind:value={ssoForm.viewer_groups} disabled={sso.env_managed} />
              </label>
              <label class="block space-y-1">
                <span class="ds-form-label">Allowed Email Domains</span>
                <input class="ds-input-sm w-full" type="text" placeholder="yourcompany.com (empty = allow all)"
                  bind:value={ssoForm.allowed_domains} disabled={sso.env_managed} />
              </label>
            </div>

            <p class="text-[11px] text-gray-400">Groups and domains are comma-separated. Users in none of the groups get the viewer role.</p>

            <div class="flex items-center gap-2">
              <span class="text-[11px] text-gray-500">Callback URL (register in your IdP):</span>
              <code class="text-[11px] font-mono text-gray-700 dark:text-gray-300 truncate">{sso.config.redirect_url}</code>
              <button class="ds-btn-ghost p-1" onclick={() => copyText(sso!.config.redirect_url, 'Callback URL')} title="Copy callback URL">
                <Copy size={12} />
              </button>
            </div>

            <div class="flex flex-wrap items-center gap-3 pt-3 border-t border-gray-200 dark:border-gray-800">
              {#if !sso.env_managed}
                <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
                  <input type="checkbox" bind:checked={ssoForm.enabled} />
                  Enable SSO
                </label>
                <button class="ds-btn-primary" onclick={() => saveSSO()} disabled={ssoSaving}>
                  {ssoSaving ? 'Saving...' : 'Save'}
                </button>
                <span class="text-xs text-gray-400">Changes apply immediately — no restart needed.</span>
              {/if}
              {#if sso.reload_error}
                <span class="text-xs text-red-600 dark:text-red-400">Provider init failed: {sso.reload_error}</span>
              {/if}
            </div>
          </div>
        {:else}
          <p class="text-sm text-gray-500">Could not load SSO settings.</p>
        {/if}
      </div>

    {:else if activeTab === 'tunnels'}
      <div class="ds-panel p-3 mb-3 space-y-2">
        <div class="flex gap-1">
          <button
            class="ds-tab {tunnelCreateMode === 'direct' ? 'ds-tab-active' : ''}"
            onclick={() => (tunnelCreateMode = 'direct')}
          >
            Direct URL
          </button>
          <button
            class="ds-tab {tunnelCreateMode === 'tunnel' ? 'ds-tab-active' : ''}"
            onclick={() => (tunnelCreateMode = 'tunnel')}
          >
            Remote Agent
          </button>
        </div>
        <p class="text-[11px] text-gray-500">
          {tunnelCreateMode === 'direct'
            ? 'Connect to a ClickHouse server this CH-UI instance can reach over HTTP(S). No agent needed; connects immediately.'
            : 'For ClickHouse servers behind a firewall: creates a token, then run the ch-ui agent next to that server.'}
        </p>
        <div class="flex flex-wrap items-end gap-2">
          <label class="space-y-1">
            <span class="text-xs text-gray-500">Connection Name</span>
            <input
              class="ds-input-sm w-56"
              placeholder="warehouse-prod"
              bind:value={tunnelCreateName}
              onkeydown={(e) => e.key === 'Enter' && createTunnel()}
            />
          </label>
          {#if tunnelCreateMode === 'direct'}
            <label class="space-y-1">
              <span class="text-xs text-gray-500">ClickHouse URL</span>
              <input
                class="ds-input-sm w-72"
                placeholder="http://clickhouse-host:8123"
                bind:value={tunnelCreateUrl}
                onkeydown={(e) => e.key === 'Enter' && createTunnel()}
              />
            </label>
          {/if}
          <button
            class="ds-btn-primary"
            onclick={() => createTunnel()}
            disabled={tunnelCreateLoading || !tunnelCreateName.trim() || (tunnelCreateMode === 'direct' && !tunnelCreateUrl.trim())}
          >
            <Plus size={14} />
            {tunnelCreateLoading ? 'Creating...' : tunnelCreateMode === 'direct' ? 'Add Connection' : 'Create Tunnel'}
          </button>
          <button class="ds-btn-ghost" onclick={() => loadTunnels()} title="Refresh connections">
            <RefreshCw size={14} />
          </button>
        </div>
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
        <p class="text-sm text-gray-500">No connections configured</p>
      {:else}
        <div class="ds-table-wrap">
          <table class="ds-table">
            <thead>
              <tr class="ds-table-head-row">
                <th class="ds-table-th">Name</th>
                <th class="ds-table-th">Type</th>
                <th class="ds-table-th">Target</th>
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
                  <td class="ds-td">
                    <span class="ds-badge">
                      {conn.is_embedded ? 'Embedded' : conn.type === 'direct' ? 'Direct' : 'Agent'}
                    </span>
                  </td>
                  <td class="ds-td-mono">{conn.clickhouse_url || '—'}</td>
                  <td class="ds-td">
                    <span class="inline-flex items-center gap-2">
                      <span class="w-2 h-2 rounded-full {conn.online ? 'bg-green-500' : 'bg-gray-400'}"></span>
                      <span class={conn.online ? 'text-green-600' : 'text-gray-500'}>{conn.online ? 'Online' : 'Offline'}</span>
                    </span>
                  </td>
                  <td class="ds-td-mono">{conn.host_info?.hostname || '—'}</td>
                  <td class="ds-td-mono whitespace-nowrap">{formatDate(conn.created_at)}</td>
                  <td class="ds-td-right">
                    <div class="flex justify-end gap-2">
                      {#if conn.type !== 'direct'}
                        <button class="ds-btn-outline px-2.5 py-1.5" onclick={() => viewTunnelToken(conn)}>Token</button>
                        <button class="ds-btn-outline px-2.5 py-1.5" onclick={() => regenerateTunnelToken(conn)}>Regenerate</button>
                      {/if}
                      {#if conn.is_embedded}
                        <span class="text-[11px] text-gray-500 self-center">Managed by server config</span>
                      {:else}
                        <button
                          class="ds-btn-outline px-2.5 py-1.5 border-red-300/80 text-red-600 hover:text-red-700 hover:border-red-500"
                          onclick={() => requestDeleteTunnel(conn)}
                        >
                          <Trash2 size={12} />
                          Delete
                        </button>
                      {/if}
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
        {#if usersError}
          <div class="mb-2 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-700 dark:text-red-300">
            {usersError}
          </div>
        {:else if !usersSyncCheck}
          <div class="mb-2 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-300">
            The ClickHouse user check could not run, so this list may include stale session users.
          </div>
        {/if}
        {#if users.length === 0}
          {#if !usersError}
            <p class="text-sm text-gray-500 mb-4">No users found</p>
          {/if}
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
	                    <td class="ds-td-mono">{user.last_login ? formatDate(user.last_login) : '—'}</td>
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

        {#if chUsersError}
          <div class="rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-700 dark:text-red-300">
            {chUsersError}
          </div>
        {:else if chUsers.length === 0}
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
                    <td class="ds-td-mono">{formatCHAuthType(row.auth_type)}</td>
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
        <!-- Header -->
        <div class="flex flex-col gap-1 mb-6">
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-2.5">
              <div class="flex items-center justify-center w-8 h-8 rounded-lg bg-orange-100 dark:bg-orange-500/15">
                <Brain size={16} class="text-ch-blue" />
              </div>
              <div>
                <h2 class="text-base font-semibold text-gray-900 dark:text-gray-100">Brain Control Center</h2>
                <p class="text-xs text-gray-500">Manage AI providers, models, and system prompt</p>
              </div>
            </div>
            <div class="flex items-center gap-2">
              <button class="ds-btn-ghost" onclick={() => loadBrainAdmin()} title="Refresh">
                <RefreshCw size={14} />
              </button>
            </div>
          </div>
        </div>

        <!-- Stats -->
        <div class="grid grid-cols-2 md:grid-cols-4 gap-3 mb-8">
          <div class="ds-stat-card">
            <div class="flex items-center gap-2 text-gray-500 text-xs mb-1">
              <div class="w-1.5 h-1.5 rounded-full bg-ch-blue"></div>
              Providers
            </div>
            <div class="text-2xl font-bold text-gray-900 dark:text-gray-100">{brainProviders.length}</div>
            <div class="text-[11px] text-gray-400 mt-0.5">{brainProviders.filter(p => p.is_active).length} active</div>
          </div>
          <div class="ds-stat-card">
            <div class="flex items-center gap-2 text-gray-500 text-xs mb-1">
              <div class="w-1.5 h-1.5 rounded-full bg-green-500"></div>
              Models
            </div>
            <div class="text-2xl font-bold text-gray-900 dark:text-gray-100">{brainModels.length}</div>
            <div class="text-[11px] text-gray-400 mt-0.5">{brainModels.filter(m => m.is_active).length} active</div>
          </div>
          <div class="ds-stat-card">
            <div class="flex items-center gap-2 text-gray-500 text-xs mb-1">
              <div class="w-1.5 h-1.5 rounded-full {brainProviders.some(p => p.is_default) ? 'bg-green-500' : 'bg-yellow-500'}"></div>
              Default Provider
            </div>
            <div class="text-sm font-semibold text-gray-900 dark:text-gray-100 mt-1">{brainProviders.find(p => p.is_default)?.name || 'None'}</div>
          </div>
          <div class="ds-stat-card">
            <div class="flex items-center gap-2 text-gray-500 text-xs mb-1">
              <div class="w-1.5 h-1.5 rounded-full {skillForm.content ? 'bg-green-500' : 'bg-yellow-500'}"></div>
              System Prompt
            </div>
            <div class="text-sm font-semibold text-gray-900 dark:text-gray-100 mt-1">{skillForm.isActive ? 'Active' : 'Inactive'}</div>
          </div>
        </div>

        <!-- Providers Section -->
        <div class="mb-8">
          <div class="flex items-center justify-between mb-3">
            <h3 class="text-sm font-semibold text-gray-800 dark:text-gray-200">Providers</h3>
            <button class="ds-btn-primary" onclick={() => providerSheetOpen = true}>
              <Plus size={14} />
              Add Provider
            </button>
          </div>

          {#if brainProviders.length === 0}
            <div class="ds-empty">
              <Brain size={24} class="mx-auto mb-3 text-gray-400" />
              <p class="text-sm text-gray-500 mb-1">No providers configured</p>
              <p class="text-xs text-gray-400 mb-4">Add an OpenAI-compatible provider to enable Brain</p>
              <button class="ds-btn-primary" onclick={() => providerSheetOpen = true}>Create First Provider</button>
            </div>
          {:else}
            <div class="space-y-2">
              {#each brainProviders as provider}
                <div class="ds-card p-4 flex flex-col md:flex-row md:items-center gap-3">
                  <div class="flex items-center gap-3 flex-1 min-w-0">
                    <div class="flex items-center justify-center w-9 h-9 rounded-lg bg-gray-100 dark:bg-gray-800 shrink-0">
                      <span class="text-sm font-bold text-gray-600 dark:text-gray-300">{provider.name.charAt(0).toUpperCase()}</span>
                    </div>
                    <div class="min-w-0">
                      <div class="flex items-center gap-2">
                        <span class="font-medium text-sm text-gray-900 dark:text-gray-100">{provider.name}</span>
                        <span class="ds-badge ds-badge-neutral">{provider.kind}</span>
                        {#if provider.is_default}
                          <span class="ds-badge ds-badge-brand">Default</span>
                        {/if}
                      </div>
                      <div class="text-xs text-gray-500 font-mono truncate mt-0.5">{provider.base_url || 'Default endpoint'}</div>
                    </div>
                  </div>

                  <div class="flex items-center gap-4 shrink-0">
                    <div class="flex items-center gap-1.5">
                      {#if provider.has_api_key}
                        <span class="ds-badge ds-badge-success">Key set</span>
                      {:else}
                        <span class="ds-badge ds-badge-danger">No key</span>
                      {/if}
                    </div>

                    <label class="ds-checkbox-label text-xs gap-1.5">
                      <input
                        type="checkbox"
                        class="ds-checkbox"
                        checked={provider.is_active}
                        onchange={(e) => toggleProvider(provider, 'is_active', (e.target as HTMLInputElement).checked)}
                      />
                      Active
                    </label>

                    <label class="ds-checkbox-label text-xs gap-1.5">
                      <input
                        type="radio"
                        class="ds-radio"
                        name="default-brain-provider"
                        checked={provider.is_default}
                        onchange={() => toggleProvider(provider, 'is_default', true)}
                      />
                      Default
                    </label>

                    <div class="flex items-center gap-1 border-l border-gray-200 dark:border-gray-700 pl-3">
                      <button class="ds-btn-outline text-xs" onclick={() => syncProviderModels(provider)}>Sync Models</button>
                      <button class="ds-icon-btn text-red-500 hover:text-red-600 hover:bg-red-50 dark:hover:bg-red-950/30" onclick={() => deleteProvider(provider)} title="Delete provider">
                        <Trash2 size={14} />
                      </button>
                    </div>
                  </div>
                </div>
              {/each}
            </div>
          {/if}
        </div>

        <!-- Models Section -->
        <div class="mb-8">
          <div class="flex items-center justify-between mb-3">
            <div class="flex items-center gap-2">
              <h3 class="text-sm font-semibold text-gray-800 dark:text-gray-200">Models</h3>
              <HelpTip text="Activate models to make them available in Brain chat. Set one default per provider." />
            </div>
            <div class="flex items-center gap-2">
              <button class="ds-btn-outline text-xs" onclick={() => runModelBulkAction('activate_recommended')}>Activate Recommended</button>
              <button class="ds-btn-outline text-xs" onclick={() => runModelBulkAction('activate_all')}>Activate All</button>
              <button class="ds-btn-outline text-xs" onclick={() => runModelBulkAction('deactivate_all')}>Deactivate All</button>
            </div>
          </div>

          <div class="flex items-center gap-2 mb-3">
            <div class="w-40">
              <Combobox
                options={providerFilterOptions()}
                value={modelProviderFilter}
                onChange={(v) => modelProviderFilter = v}
              />
            </div>
            <input
              class="ds-input-sm flex-1"
              placeholder="Search models..."
              bind:value={modelSearch}
            />
            <label class="ds-checkbox-label text-xs whitespace-nowrap">
              <input type="checkbox" class="ds-checkbox ds-checkbox-sm" bind:checked={modelShowOnlyActive} />
              Active only
            </label>
          </div>

          {#if brainModels.length === 0}
            <p class="text-sm text-gray-500 py-4 text-center">No models synced yet. Add a provider and click "Sync Models".</p>
          {:else}
            <div class="space-y-2">
              {#each visibleProvidersForModels() as provider}
                {@const providerModels = modelsForProvider(provider.id)}
                <details class="ds-card overflow-hidden" open={modelProviderFilter === provider.id || (!modelProviderFilter && provider.is_default)}>
                  <summary class="cursor-pointer list-none px-4 py-3 flex items-center justify-between bg-gray-50 dark:bg-gray-900 hover:bg-gray-100 dark:hover:bg-gray-800/80 transition-colors">
                    <div class="flex items-center gap-2.5">
                      <div class="flex items-center justify-center w-6 h-6 rounded bg-gray-200 dark:bg-gray-800">
                        <span class="text-[10px] font-bold text-gray-600 dark:text-gray-400">{provider.name.charAt(0).toUpperCase()}</span>
                      </div>
                      <span class="font-medium text-sm text-gray-900 dark:text-gray-100">{provider.name}</span>
                      <span class="ds-badge ds-badge-neutral">{provider.kind}</span>
                    </div>
                    <div class="flex items-center gap-3 text-xs">
                      <span class="ds-badge ds-badge-success">{providerModels.filter(m => m.is_active).length} active</span>
                      <span class="text-gray-500">{providerModels.length} total</span>
                    </div>
                  </summary>
                  <div class="max-h-[40vh] overflow-auto border-t border-gray-200 dark:border-gray-800">
                    {#if providerModels.length > 0}
                      <table class="ds-table">
                        <thead>
                          <tr class="ds-table-head-row sticky top-0 bg-gray-50 dark:bg-gray-900 z-10">
                            <th class="ds-table-th">Model</th>
                            <th class="ds-table-th w-20 text-center">Active</th>
                            <th class="ds-table-th w-20 text-center">Default</th>
                          </tr>
                        </thead>
                        <tbody>
                          {#each providerModels as model}
                            <tr class="ds-table-row">
                              <td class="ds-td-mono">{model.display_name || model.name}</td>
                              <td class="ds-td text-center">
                                <input
                                  type="checkbox"
                                  class="ds-checkbox"
                                  checked={model.is_active}
                                  onchange={(e) => updateModel(model, 'is_active', (e.target as HTMLInputElement).checked)}
                                />
                              </td>
                              <td class="ds-td text-center">
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
                      <p class="text-xs text-gray-500 px-4 py-6 text-center">No models match current filters.</p>
                    {/if}
                  </div>
                </details>
              {/each}
            </div>
          {/if}
        </div>

        <!-- System Prompt Section -->
        <div>
          <div class="flex items-center justify-between mb-3">
            <div class="flex items-center gap-2">
              <h3 class="text-sm font-semibold text-gray-800 dark:text-gray-200">System Prompt</h3>
              {#if skillForm.isActive}
                <span class="ds-badge ds-badge-success">Active</span>
              {:else}
                <span class="ds-badge ds-badge-neutral">Inactive</span>
              {/if}
            </div>
            <button class="ds-btn-outline" onclick={() => openSkillSheet()}>Edit Prompt</button>
          </div>
          <div class="ds-card overflow-hidden">
            <div class="px-4 py-3 border-b border-gray-200 dark:border-gray-800 bg-gray-50/50 dark:bg-gray-900/50">
              <div class="flex items-center justify-between">
                <span class="text-xs font-medium text-gray-700 dark:text-gray-300">{skillForm.name || 'Default Skill'}</span>
                <span class="text-[11px] text-gray-400">Steers SQL safety, artifacts, and tool behavior</span>
              </div>
            </div>
            <pre class="text-[11px] leading-relaxed whitespace-pre-wrap text-gray-600 dark:text-gray-400 max-h-44 overflow-auto p-4">{truncate(skillForm.content || 'No system prompt configured.', 1500)}</pre>
          </div>
        </div>
      {/if}

    {:else if activeTab === 'github'}
      {#if !isProActive()}
        <div class="flex flex-col items-center justify-center py-16 text-center">
          <GitBranch size={32} class="text-gray-400 mb-3" />
          <h2 class="text-lg font-semibold text-gray-700 dark:text-gray-300 mb-1">GitHub Model Sync</h2>
          <p class="text-sm text-gray-500 max-w-md">Connect a GitHub repository and sync your SQL models. Requires a Pro license.</p>
        </div>
      {:else if ghLoading}
        <div class="flex items-center justify-center py-12"><Spinner /></div>
      {:else}
        <div class="max-w-2xl mx-auto space-y-6">
          <!-- Header -->
          <div class="flex items-center gap-3">
            <GitBranch size={20} class="text-gray-600 dark:text-gray-400" />
            <div>
              <h2 class="text-base font-semibold text-gray-800 dark:text-gray-200">GitHub Model Sync</h2>
              <p class="text-xs text-gray-500">Pull models from a GitHub repository. GitHub is the source of truth.</p>
            </div>
          </div>

          <!-- Form -->
          <div class="space-y-4 p-4 rounded-lg border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900/50">
            <div>
              <label for="gh-repo" class="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Repository</label>
              <input id="gh-repo" type="text" bind:value={ghForm.repo} placeholder="owner/repo"
                class="w-full px-3 py-2 text-sm rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 text-gray-800 dark:text-gray-200 focus:ring-2 focus:ring-ch-blue/40 outline-none" />
            </div>
            <div class="grid grid-cols-2 gap-4">
              <div>
                <label for="gh-branch" class="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Branch</label>
                <input id="gh-branch" type="text" bind:value={ghForm.branch} placeholder="main"
                  class="w-full px-3 py-2 text-sm rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 text-gray-800 dark:text-gray-200 focus:ring-2 focus:ring-ch-blue/40 outline-none" />
              </div>
              <div>
                <label for="gh-path" class="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Models path</label>
                <input id="gh-path" type="text" bind:value={ghForm.path} placeholder="models/"
                  class="w-full px-3 py-2 text-sm rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 text-gray-800 dark:text-gray-200 focus:ring-2 focus:ring-ch-blue/40 outline-none" />
              </div>
            </div>
            <div>
              <label for="gh-pat" class="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">
                Personal Access Token
                {#if ghIntegration?.has_pat}
                  <span class="text-green-500 font-normal ml-1">(configured)</span>
                {/if}
              </label>
              <input id="gh-pat" type="password" bind:value={ghForm.pat} placeholder={ghIntegration?.has_pat ? 'Leave blank to keep current token' : 'ghp_...'}
                class="w-full px-3 py-2 text-sm rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 text-gray-800 dark:text-gray-200 focus:ring-2 focus:ring-ch-blue/40 outline-none" />
              <p class="text-[10px] text-gray-400 mt-1">Needs <code>repo</code> scope (or <code>contents:read</code> for fine-grained tokens)</p>
            </div>

            <div class="flex items-center gap-2 pt-2">
              <button onclick={handleGitHubSave} disabled={ghSaving || !ghForm.repo}
                class="px-4 py-2 text-xs font-medium rounded-lg bg-ch-blue text-white hover:bg-ch-blue/90 disabled:opacity-50 transition-colors">
                {ghSaving ? 'Saving...' : 'Save'}
              </button>
              {#if ghIntegration?.has_pat}
                <button onclick={handleGitHubTest} disabled={ghTesting}
                  class="px-4 py-2 text-xs font-medium rounded-lg border border-gray-300 dark:border-gray-600 text-gray-600 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-800 disabled:opacity-50 transition-colors">
                  {ghTesting ? 'Testing...' : 'Test Connection'}
                </button>
              {/if}
              {#if ghIntegration}
                <button onclick={handleGitHubDelete}
                  class="px-4 py-2 text-xs font-medium rounded-lg text-red-500 hover:bg-red-50 dark:hover:bg-red-900/20 transition-colors ml-auto">
                  Remove
                </button>
              {/if}
            </div>

            {#if ghTestResult}
              <div class="flex items-center gap-2 px-3 py-2 rounded-lg text-xs {ghTestResult.success ? 'bg-green-50 dark:bg-green-900/20 text-green-700 dark:text-green-400' : 'bg-red-50 dark:bg-red-900/20 text-red-700 dark:text-red-400'}">
                {#if ghTestResult.success}
                  <Check size={14} /> Connection successful
                {:else}
                  <XIcon size={14} /> {ghTestResult.error || 'Connection failed'}
                {/if}
              </div>
            {/if}
          </div>

          <!-- Sync controls -->
          {#if ghIntegration?.has_pat}
            <div class="flex items-center gap-3 p-4 rounded-lg border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900/50">
              <CloudDownload size={16} class="text-gray-500 shrink-0" />
              <div class="flex-1 min-w-0">
                <p class="text-sm font-medium text-gray-700 dark:text-gray-300">Sync models from GitHub</p>
                <p class="text-[11px] text-gray-400">Pull .sql files from <code class="text-[10px]">{ghIntegration.repo}/{ghIntegration.path}</code> on <code class="text-[10px]">{ghIntegration.branch}</code></p>
              </div>
              <button onclick={handleGitHubSync} disabled={ghSyncing}
                class="px-4 py-2 text-xs font-medium rounded-lg bg-purple-500 text-white hover:bg-purple-600 disabled:opacity-50 transition-colors shrink-0">
                {ghSyncing ? 'Syncing...' : 'Sync Now'}
              </button>
            </div>

            <!-- Webhook URL -->
            <div class="p-4 rounded-lg border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900/50">
              <p class="text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Webhook URL (optional)</p>
              <p class="text-[10px] text-gray-400 mb-2">Add this as a GitHub webhook to auto-sync on push.</p>
              <code class="block text-[11px] bg-gray-100 dark:bg-gray-800 px-3 py-2 rounded text-gray-600 dark:text-gray-400 break-all select-all">
                {window.location.origin}/api/github/webhook/{getSession()?.connectionId ?? ''}
              </code>
            </div>
          {/if}

          <!-- Sync history -->
          {#if ghSyncLogs.length > 0}
            <div>
              <h3 class="text-xs font-medium text-gray-500 dark:text-gray-400 mb-2 uppercase tracking-wide">Sync History</h3>
              <div class="space-y-1">
                {#each ghSyncLogs.slice(0, 10) as log (log.id)}
                  <div class="flex items-center gap-3 px-3 py-2 rounded-lg text-xs bg-white dark:bg-gray-900/50 border border-gray-200 dark:border-gray-700">
                    <span class="w-2 h-2 rounded-full shrink-0 {log.status === 'success' ? 'bg-green-400' : log.status === 'error' ? 'bg-red-400' : log.status === 'partial' ? 'bg-yellow-400' : 'bg-blue-400'}"></span>
                    <span class="text-gray-600 dark:text-gray-400 tabular-nums">{new Date(log.started_at).toLocaleString()}</span>
                    <span class="text-gray-500">
                      {log.models_created > 0 ? `+${log.models_created}` : ''}
                      {log.models_updated > 0 ? ` ~${log.models_updated}` : ''}
                      {log.models_deleted > 0 ? ` -${log.models_deleted}` : ''}
                      {log.models_created === 0 && log.models_updated === 0 && log.models_deleted === 0 ? 'no changes' : ''}
                    </span>
                    {#if log.commit_sha}
                      <span class="ml-auto text-[10px] text-gray-400 font-mono">{log.commit_sha.slice(0, 7)}</span>
                    {/if}
                    {#if log.triggered_by}
                      <span class="text-[10px] text-gray-400">{log.triggered_by}</span>
                    {/if}
                  </div>
                {/each}
              </div>
            </div>
          {/if}

          <!-- File format help -->
          <div class="p-4 rounded-lg border border-dashed border-gray-300 dark:border-gray-600">
            <p class="text-xs font-medium text-gray-600 dark:text-gray-400 mb-2">Model file format</p>
            <pre class="text-[11px] text-gray-500 dark:text-gray-400 whitespace-pre leading-relaxed">---
materialization: table
target_database: default
table_engine: MergeTree
order_by: (id, created_at)
description: My model
---
SELECT id, count() as events
FROM $ref(raw_events)
GROUP BY id</pre>
            <p class="text-[10px] text-gray-400 mt-2">Filename becomes the model name. YAML header is optional (defaults: materialization=view, target_database=default).</p>
          </div>
        </div>
      {/if}

    {:else if activeTab === 'mcp'}
      <MCPKeysPanel />

    {/if}
  </div>
</div>
{/if}
