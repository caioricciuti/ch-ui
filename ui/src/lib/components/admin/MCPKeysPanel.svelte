<script lang="ts">
  import { onMount } from 'svelte'
  import { apiGet } from '../../api/client'
  import {
    listMCPKeys,
    createMCPKey,
    rotateMCPKey,
    revokeMCPKey,
    type MCPKey,
  } from '../../api/mcp'
  import { success, error as toastError } from '../../stores/toast.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import { Plug, Plus, Copy, Trash2, RefreshCw } from 'lucide-svelte'
  import { formatDate } from '../../utils/format'

  interface ConnectionRow {
    id: string
    name: string
    online?: boolean
  }

  let keys = $state<MCPKey[]>([])
  let connections = $state<ConnectionRow[]>([])
  let loading = $state(true)
  let creating = $state(false)
  let showForm = $state(false)

  let name = $state('')
  let connectionId = $state('')
  let chUser = $state('')
  let chPassword = $state('')
  let allowedDatabases = $state('')
  let allowWrite = $state(false)
  let expiresInDays = $state(90)

  const expiryOptions = [
    { days: 30, label: '30 days' },
    { days: 90, label: '90 days' },
    { days: 365, label: '1 year' },
    { days: 0, label: 'Never' },
  ]

  // One-time reveal of the freshly created key.
  let revealed = $state<{ secret: string; name: string } | null>(null)

  let revokeTarget = $state<MCPKey | null>(null)
  let revoking = $state(false)
  let rotateTarget = $state<MCPKey | null>(null)
  let rotating = $state(false)

  const mcpUrl = $derived(`${window.location.origin}/mcp`)
  const claudeCodeCmd = $derived(
    revealed
      ? `claude mcp add --transport http ch-ui ${mcpUrl} --header "Authorization: Bearer ${revealed.secret}"`
      : ''
  )

  async function load() {
    loading = true
    try {
      const [keysRes, conns] = await Promise.all([
        listMCPKeys(),
        apiGet<ConnectionRow[]>('/api/connections'),
      ])
      keys = keysRes.keys
      connections = conns
      if (!connectionId && conns.length > 0) connectionId = conns[0].id
    } catch (e) {
      toastError(e instanceof Error ? e.message : 'Failed to load MCP keys')
    } finally {
      loading = false
    }
  }

  onMount(load)

  async function submit() {
    if (!name.trim() || !connectionId || !chUser.trim()) {
      toastError('Name, connection and ClickHouse user are required')
      return
    }
    creating = true
    try {
      const res = await createMCPKey({
        name: name.trim(),
        connection_id: connectionId,
        ch_user: chUser.trim(),
        ch_password: chPassword,
        scopes: allowWrite ? 'read_write' : 'read',
        allowed_databases: allowedDatabases.trim(),
        expires_in_days: expiresInDays,
      })
      revealed = { secret: res.secret, name: res.key.name }
      name = ''
      chUser = ''
      chPassword = ''
      allowedDatabases = ''
      allowWrite = false
      expiresInDays = 90
      showForm = false
      success('MCP key created')
      await load()
    } catch (e) {
      toastError(e instanceof Error ? e.message : 'Failed to create MCP key')
    } finally {
      creating = false
    }
  }

  async function confirmRevoke() {
    if (!revokeTarget) return
    revoking = true
    try {
      await revokeMCPKey(revokeTarget.id)
      success(`Key "${revokeTarget.name}" revoked`)
      revokeTarget = null
      await load()
    } catch (e) {
      toastError(e instanceof Error ? e.message : 'Failed to revoke key')
    } finally {
      revoking = false
    }
  }

  async function confirmRotate() {
    if (!rotateTarget) return
    rotating = true
    try {
      const res = await rotateMCPKey(rotateTarget.id)
      revealed = { secret: res.secret, name: res.key.name }
      success(`Key "${rotateTarget.name}" rotated; the old secret no longer works`)
      rotateTarget = null
      await load()
    } catch (e) {
      toastError(e instanceof Error ? e.message : 'Failed to rotate key')
    } finally {
      rotating = false
    }
  }

  function isExpired(k: MCPKey): boolean {
    return !!k.expires_at && new Date(k.expires_at).getTime() <= Date.now()
  }

  async function copyText(value: string, label: string) {
    try {
      await navigator.clipboard.writeText(value)
      success(`${label} copied`)
    } catch {
      toastError('Clipboard unavailable')
    }
  }

  function connectionName(id: string): string {
    return connections.find((c) => c.id === id)?.name ?? id
  }
</script>

<section class="ds-panel p-5 space-y-4">
  <div class="flex items-center justify-between gap-2">
    <div class="flex items-center gap-2">
      <Plug size={16} class="text-ch-orange" />
      <h2 class="text-sm font-semibold text-gray-800 dark:text-gray-200 uppercase tracking-wide">MCP Server</h2>
    </div>
    <button class="ds-btn-outline px-2.5 py-1.5 flex items-center gap-1.5" onclick={() => (showForm = !showForm)}>
      <Plus size={14} />
      New key
    </button>
  </div>

  <p class="text-xs text-gray-500 dark:text-gray-400">
    CH-UI embeds a Model Context Protocol server at <span class="font-mono">{mcpUrl}</span>. AI clients
    (Claude Code, Cursor, VS Code, Zed) connect with a key and get read-only, row-capped tools: schema
    browsing, SELECT queries, and query plans, all recorded in query history and the audit log.
    The key's ClickHouse user grants are the real permission boundary: use a locked-down user.
    Keys can expire and be rotated in place; rotating keeps the binding and reveals a new secret once.
  </p>

  {#if revealed}
    <div class="ds-panel-muted p-4 space-y-3 border border-orange-300/40 dark:border-orange-700/40 rounded-lg">
      <p class="text-sm font-semibold text-gray-900 dark:text-gray-100">
        Key "{revealed.name}" created — copy it now, it will not be shown again.
      </p>
      <div class="flex items-center gap-2">
        <pre class="ds-td-mono text-xs bg-gray-100 dark:bg-gray-900 rounded p-2 overflow-x-auto flex-1">{revealed.secret}</pre>
        <button class="ds-btn-outline px-2.5 py-1.5" onclick={() => copyText(revealed!.secret, 'MCP key')} title="Copy key">
          <Copy size={14} />
        </button>
      </div>
      <p class="text-xs text-gray-500 dark:text-gray-400">Add it to Claude Code:</p>
      <div class="flex items-center gap-2">
        <pre class="ds-td-mono text-xs bg-gray-100 dark:bg-gray-900 rounded p-2 overflow-x-auto flex-1">{claudeCodeCmd}</pre>
        <button class="ds-btn-outline px-2.5 py-1.5" onclick={() => copyText(claudeCodeCmd, 'Command')} title="Copy command">
          <Copy size={14} />
        </button>
      </div>
      <button class="ds-btn-ghost px-2.5 py-1.5 text-xs" onclick={() => (revealed = null)}>Dismiss</button>
    </div>
  {/if}

  {#if showForm}
    <div class="ds-panel-muted p-4 grid grid-cols-1 sm:grid-cols-2 gap-3">
      <label class="block">
        <span class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Key name</span>
        <input class="ds-input-sm mt-1 w-full" bind:value={name} placeholder="claude-code laptop" />
      </label>
      <label class="block">
        <span class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Connection</span>
        <select class="ds-input-sm mt-1 w-full" bind:value={connectionId}>
          {#each connections as c}
            <option value={c.id}>{c.name}</option>
          {/each}
        </select>
      </label>
      <label class="block">
        <span class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">ClickHouse user</span>
        <input class="ds-input-sm mt-1 w-full" bind:value={chUser} placeholder="mcp_readonly" autocomplete="off" />
      </label>
      <label class="block">
        <span class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">ClickHouse password</span>
        <input class="ds-input-sm mt-1 w-full" type="password" bind:value={chPassword} autocomplete="new-password" />
      </label>
      <label class="block">
        <span class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Database allowlist (optional, comma-separated)</span>
        <input class="ds-input-sm mt-1 w-full" bind:value={allowedDatabases} placeholder="analytics, logs" />
      </label>
      <label class="block">
        <span class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Expires</span>
        <select class="ds-input-sm mt-1 w-full" bind:value={expiresInDays}>
          {#each expiryOptions as o}
            <option value={o.days}>{o.label}</option>
          {/each}
        </select>
      </label>
      <label class="flex items-start gap-2 sm:col-span-2 cursor-pointer">
        <input type="checkbox" class="mt-0.5" bind:checked={allowWrite} />
        <span class="text-xs text-gray-600 dark:text-gray-300">
          <span class="font-medium">Allow write tools</span> — the key can create saved queries, dashboards, and draft models/pipelines in CH-UI.
          Nothing runs or touches ClickHouse data; read-only keys never see these tools.
        </span>
      </label>
      <div class="sm:col-span-2 flex gap-2">
        <button class="ds-btn-primary px-3 py-1.5" onclick={submit} disabled={creating}>
          {creating ? 'Creating…' : 'Create key'}
        </button>
        <button class="ds-btn-ghost px-3 py-1.5" onclick={() => (showForm = false)}>Cancel</button>
      </div>
    </div>
  {/if}

  {#if loading}
    <p class="text-xs text-gray-500 dark:text-gray-400">Loading…</p>
  {:else if keys.length === 0}
    <p class="text-xs text-gray-500 dark:text-gray-400">No MCP keys yet. Create one to connect an AI client.</p>
  {:else}
    <div class="ds-table-wrap">
      <table class="ds-table">
        <thead>
          <tr class="ds-table-head-row">
            <th class="ds-table-th">Name</th>
            <th class="ds-table-th">Key</th>
            <th class="ds-table-th">Connection</th>
            <th class="ds-table-th">CH user</th>
            <th class="ds-table-th">Scope</th>
            <th class="ds-table-th">Databases</th>
            <th class="ds-table-th">Last used</th>
            <th class="ds-table-th">Expires</th>
            <th class="ds-table-th">Status</th>
            <th class="ds-table-th ds-td-right"></th>
          </tr>
        </thead>
        <tbody>
          {#each keys as k (k.id)}
            <tr class="ds-table-row">
              <td class="ds-td ds-td-strong">{k.name}</td>
              <td class="ds-td ds-td-mono">{k.key_prefix}</td>
              <td class="ds-td">{connectionName(k.connection_id)}</td>
              <td class="ds-td ds-td-mono">{k.ch_user}</td>
              <td class="ds-td">
                {#if k.scopes === 'read_write'}
                  <span class="ds-badge ds-badge-brand">read + write</span>
                {:else}
                  <span class="ds-badge">read</span>
                {/if}
              </td>
              <td class="ds-td">{k.allowed_databases || 'all'}</td>
              <td class="ds-td">{k.last_used_at ? formatDate(k.last_used_at) : 'never'}</td>
              <td class="ds-td">{k.expires_at ? formatDate(k.expires_at) : 'never'}</td>
              <td class="ds-td">
                {#if k.revoked_at}
                  <span class="ds-badge ds-badge-danger">revoked</span>
                {:else if isExpired(k)}
                  <span class="ds-badge ds-badge-danger">expired</span>
                {:else}
                  <span class="ds-badge ds-badge-success">active</span>
                {/if}
              </td>
              <td class="ds-td ds-td-right">
                {#if !k.revoked_at}
                  <span class="inline-flex gap-1">
                    <button
                      class="ds-btn-outline px-2 py-1"
                      title="Rotate key (new secret, same binding)"
                      onclick={() => (rotateTarget = k)}
                    >
                      <RefreshCw size={13} />
                    </button>
                    <button
                      class="ds-btn-outline px-2 py-1"
                      title="Revoke key"
                      onclick={() => (revokeTarget = k)}
                    >
                      <Trash2 size={13} />
                    </button>
                  </span>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</section>

<ConfirmDialog
  open={rotateTarget !== null}
  title="Rotate MCP key"
  description={`Rotate "${rotateTarget?.name ?? ''}"? A new secret is issued with the same connection, user, scope and expiry; the current secret stops working immediately. Update every client that uses it.`}
  confirmLabel="Rotate"
  destructive={false}
  loading={rotating}
  onconfirm={confirmRotate}
  oncancel={() => (rotateTarget = null)}
/>

<ConfirmDialog
  open={revokeTarget !== null}
  title="Revoke MCP key"
  description={`Revoke "${revokeTarget?.name ?? ''}"? Clients using it lose access immediately. This cannot be undone.`}
  confirmLabel="Revoke"
  destructive={true}
  loading={revoking}
  onconfirm={confirmRevoke}
  oncancel={() => (revokeTarget = null)}
/>
