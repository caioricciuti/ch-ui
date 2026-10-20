<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { onMount } from 'svelte'
  import { getGitHubIntegration, saveGitHubIntegration, deleteGitHubIntegration, testGitHubConnection, triggerGitHubSync, getGitHubSyncLogs } from '../../api/github'
  import type { GitHubIntegration, GitHubSyncLog } from '../../types/models'
  import { success as toastSuccess, error as toastError, errorFrom as toastErrorFrom } from '../../stores/toast.svelte'
  import { isProActive } from '../../stores/license.svelte'
  import { getSession } from '../../stores/session.svelte'
  import { goTo } from '../../stores/router.svelte'
  import { formatDate } from '../../utils/format'
  import PageBody from '../common/PageBody.svelte'
  import SectionHeader from '../common/SectionHeader.svelte'
  import Panel from '../common/Panel.svelte'
  import Sheet from '../common/Sheet.svelte'
  import FormField from '../common/FormField.svelte'
  import Input from '../common/Input.svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Spinner from '../common/Spinner.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import { Lock } from 'lucide-svelte'

  // GitHub model sync: the repository is the source of truth for SQL models.
  // The section shows the stored link as read-only facts; editing happens in
  // a Sheet so the token and secret are only ever typed, never shown.
  let ghIntegration = $state<GitHubIntegration | null>(null)
  let ghLoading = $state(false)
  let ghSaving = $state(false)
  let ghTesting = $state(false)
  let ghTestResult = $state<{ success: boolean; error?: string } | null>(null)
  let ghSyncing = $state(false)
  let ghSyncLogs = $state<GitHubSyncLog[]>([])
  let ghForm = $state({ repo: '', branch: 'main', path: 'models/', pat: '', webhookSecret: '' })
  let editOpen = $state(false)
  let confirmRemoveOpen = $state(false)

  const licensedPro = $derived(isProActive())
  const webhookUrl = $derived(`${window.location.origin}/api/github/webhook/${getSession()?.connectionId ?? ''}`)
  const lastSync = $derived(ghSyncLogs[0] ?? null)

  onMount(() => {
    if (licensedPro) void loadGitHubTab()
  })

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
      resetForm()
    } catch (e: unknown) {
      toastErrorFrom(e, 'load GitHub integration')
    } finally {
      ghLoading = false
    }
  }

  function resetForm() {
    ghForm = ghIntegration
      ? { repo: ghIntegration.repo, branch: ghIntegration.branch, path: ghIntegration.path, pat: '', webhookSecret: '' }
      : { repo: '', branch: 'main', path: 'models/', pat: '', webhookSecret: '' }
  }

  function openEdit() {
    resetForm()
    editOpen = true
  }

  function closeEdit() {
    editOpen = false
    resetForm()
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
        webhook_secret: ghForm.webhookSecret || undefined,
      })
      toastSuccess('GitHub integration saved')
      editOpen = false
      await loadGitHubTab()
    } catch (e: unknown) {
      toastErrorFrom(e, 'save the GitHub settings')
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
      ghSyncLogs = []
      ghTestResult = null
      resetForm()
      toastSuccess('GitHub integration removed')
    } catch (e: unknown) {
      toastErrorFrom(e, 'remove the GitHub integration')
    } finally {
      confirmRemoveOpen = false
    }
  }

  function syncTone(status: string): 'success' | 'danger' | 'warning' | 'info' {
    if (status === 'success') return 'success'
    if (status === 'error') return 'danger'
    if (status === 'partial') return 'warning'
    return 'info'
  }

  function changesLabel(log: GitHubSyncLog): string {
    const parts: string[] = []
    if (log.models_created > 0) parts.push(`+${log.models_created}`)
    if (log.models_updated > 0) parts.push(`~${log.models_updated}`)
    if (log.models_deleted > 0) parts.push(`-${log.models_deleted}`)
    return parts.length > 0 ? parts.join(' ') : 'no changes'
  }

  type LogRow = Record<string, unknown> & GitHubSyncLog
  const logColumns: DataColumn<LogRow>[] = [
    { key: 'started_at', label: 'Started', width: '170px', format: (v) => formatDate(v) },
    { key: 'status', label: 'Status', width: '110px' },
    { key: 'changes', label: 'Changes', width: '120px', sortable: false, format: (_v, row) => changesLabel(row) },
    { key: 'triggered_by', label: 'Trigger', width: '110px', format: (v) => (v ? String(v) : '—') },
    { key: 'commit_sha', label: 'Commit', width: '110px', mono: true, format: (v) => (v ? String(v).slice(0, 7) : '—') },
    { key: 'error', label: 'Message', truncate: true, sortable: false, format: (v) => (v ? String(v) : '') },
  ]
  const logRows = $derived(ghSyncLogs.slice(0, 20) as LogRow[])
</script>

<PageBody width="md">
  <div class="space-y-8">
    <div>
      <SectionHeader
        title="GitHub model sync"
        description="Pull SQL models from a GitHub repository. The repository is the source of truth: files become models, and a push can trigger a sync through the webhook."
      />

      {#if !licensedPro}
        <Panel variant="muted">
          <div class="flex items-start gap-3">
            <Lock size={16} class="mt-0.5 shrink-0 text-accent" />
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-2">
                <span class="text-[13px] font-medium text-fg">GitHub sync needs a Pro license</span>
                <Badge tone="brand">Pro</Badge>
              </div>
              <p class="mt-1 text-[13px] leading-relaxed text-fg-3">
                Activate a license to connect a repository and keep your models in version control.
              </p>
            </div>
            <Button size="sm" variant="outline" onclick={() => goTo('settings', 'License')}>Manage license</Button>
          </div>
        </Panel>
      {:else if ghLoading}
        <div class="flex items-center justify-center py-12"><Spinner /></div>
      {:else if !ghIntegration}
        <Panel padding="none">
          <EmptyState
            size="compact"
            title="No repository connected"
            description="Link a GitHub repository and a token to start pulling models."
            primary={{ label: 'Connect repository', onclick: openEdit }}
          />
        </Panel>
      {:else}
        <Panel padding="none">
          {#snippet actions()}
            <Button size="sm" variant="ghost" class="text-danger hover:bg-danger-soft hover:text-danger" onclick={() => (confirmRemoveOpen = true)}>Remove</Button>
            {#if ghIntegration?.has_pat}
              <Button size="sm" variant="outline" onclick={handleGitHubTest} loading={ghTesting}>Test connection</Button>
            {/if}
            <Button size="sm" variant="outline" onclick={openEdit}>Edit</Button>
          {/snippet}
          <dl class="divide-y divide-edge-subtle text-[13px]">
            <div class="flex items-center gap-4 px-4 py-2.5">
              <dt class="w-40 shrink-0 text-fg-3">Repository</dt>
              <dd class="min-w-0 flex-1 truncate font-mono text-xs text-fg">{ghIntegration.repo}</dd>
            </div>
            <div class="flex items-center gap-4 px-4 py-2.5">
              <dt class="w-40 shrink-0 text-fg-3">Branch</dt>
              <dd class="min-w-0 flex-1 truncate font-mono text-xs text-fg">{ghIntegration.branch}</dd>
            </div>
            <div class="flex items-center gap-4 px-4 py-2.5">
              <dt class="w-40 shrink-0 text-fg-3">Models path</dt>
              <dd class="min-w-0 flex-1 truncate font-mono text-xs text-fg">{ghIntegration.path}</dd>
            </div>
            <div class="flex items-center gap-4 px-4 py-2.5">
              <dt class="w-40 shrink-0 text-fg-3">Personal access token</dt>
              <dd class="min-w-0 flex-1">
                {#if ghIntegration.has_pat}<Badge tone="success">Set</Badge>{:else}<Badge tone="warning">Not set</Badge>{/if}
              </dd>
            </div>
            <div class="flex items-center gap-4 px-4 py-2.5">
              <dt class="w-40 shrink-0 text-fg-3">Webhook secret</dt>
              <dd class="flex min-w-0 flex-1 items-center gap-2">
                {#if ghIntegration.has_webhook_secret}
                  <Badge tone="success">Set</Badge>
                {:else}
                  <Badge tone="warning">Not set</Badge>
                  <span class="text-xs text-fg-3">The push webhook refuses every request until one is saved.</span>
                {/if}
              </dd>
            </div>
            <div class="flex items-center gap-4 px-4 py-2.5">
              <dt class="w-40 shrink-0 text-fg-3">Last sync</dt>
              <dd class="flex min-w-0 flex-1 items-center gap-2">
                {#if lastSync}
                  <Badge tone={syncTone(lastSync.status)}>{lastSync.status}</Badge>
                  <span class="text-xs text-fg-3">{formatDate(lastSync.started_at)}</span>
                  {#if lastSync.commit_sha}<span class="font-mono text-xs text-fg-3">{lastSync.commit_sha.slice(0, 7)}</span>{/if}
                {:else}
                  <span class="text-fg-3">Never</span>
                {/if}
              </dd>
            </div>
            {#if ghTestResult}
              <div class="flex items-center gap-4 px-4 py-2.5">
                <dt class="w-40 shrink-0 text-fg-3">Connection</dt>
                <dd class="flex min-w-0 flex-1 items-center gap-2">
                  {#if ghTestResult.success}
                    <Badge tone="success">Connected</Badge>
                  {:else}
                    <Badge tone="danger">Failed</Badge>
                    <span class="min-w-0 truncate text-xs text-danger">{ghTestResult.error || 'Connection failed'}</span>
                  {/if}
                </dd>
              </div>
            {/if}
          </dl>
        </Panel>
      {/if}
    </div>

    {#if licensedPro && !ghLoading && ghIntegration?.has_pat}
      <div>
        <SectionHeader title="Sync" description="Pull the repository now, or let GitHub call the webhook on every push.">
          {#snippet actions()}
            <Button size="sm" variant="outline" onclick={handleGitHubSync} loading={ghSyncing}>Sync now</Button>
          {/snippet}
        </SectionHeader>
        <Panel padding="none">
          <div class="divide-y divide-edge-subtle px-4">
            <FormField layout="row" label="Source" hint="Where models are read from." controlWidth="full">
              <p class="font-mono text-xs text-fg-2">{ghIntegration.repo}/{ghIntegration.path} on {ghIntegration.branch}</p>
            </FormField>
            <FormField
              layout="row"
              label="Webhook URL"
              hint={ghIntegration.has_webhook_secret
                ? 'Optional. Add it as a GitHub webhook (push events) with the webhook secret saved above to sync on every push.'
                : 'Save a webhook secret above first. Until then the webhook refuses every request.'}
              controlWidth="full"
            >
              <code class="block select-all break-all rounded-md bg-surface-2 px-3 py-2 font-mono text-xs text-fg-2">{webhookUrl}</code>
            </FormField>
          </div>
        </Panel>
      </div>

      <div>
        <SectionHeader title="Recent syncs" description="The last runs, newest first." />
        <DataTable
          columns={logColumns}
          rows={logRows}
          rowKey={(row) => row.id}
          sort={{ key: 'started_at', dir: 'desc' }}
          emptyTitle="No syncs yet"
          emptyDescription="Run a sync or push to the repository."
        >
          {#snippet cell(row, col, value)}
            {#if col.key === 'status'}
              <Badge tone={syncTone(row.status)}>{row.status}</Badge>
            {:else}
              {value}
            {/if}
          {/snippet}
        </DataTable>
      </div>
    {/if}

    {#if licensedPro && !ghLoading}
      <div>
        <SectionHeader title="Model file format" description="The filename becomes the model name. The YAML header is optional; defaults are materialization=view and target_database=default." />
        <Panel variant="muted">
          <pre class="whitespace-pre font-mono text-xs leading-relaxed text-fg-2">---
materialization: table
target_database: default
table_engine: MergeTree
order_by: (id, created_at)
description: My model
---
SELECT id, count() as events
FROM $ref(raw_events)
GROUP BY id</pre>
        </Panel>
      </div>
    {/if}
  </div>
</PageBody>

<Sheet open={editOpen} title="GitHub sync" description="The repository and credentials used to pull models. Stored secrets are never shown again." size="md" onclose={closeEdit}>
  <form
    class="space-y-4"
    onsubmit={(e) => {
      e.preventDefault()
      void handleGitHubSave()
    }}
  >
    <FormField label="Repository" for="gh-repo" hint="owner/repo on github.com." required controlWidth="full">
      <Input id="gh-repo" bind:value={ghForm.repo} placeholder="owner/repo" mono />
    </FormField>
    <FormField label="Branch" for="gh-branch" hint="The branch that is synced." controlWidth="full">
      <Input id="gh-branch" bind:value={ghForm.branch} placeholder="main" mono />
    </FormField>
    <FormField label="Models path" for="gh-path" hint="Folder inside the repository that holds the .sql files." controlWidth="full">
      <Input id="gh-path" bind:value={ghForm.path} placeholder="models/" mono />
    </FormField>
    <FormField
      label="Personal access token"
      for="gh-pat"
      controlWidth="full"
      hint={ghIntegration?.has_pat
        ? 'A token is stored. Leave empty to keep it. Needs the repo scope, or contents:read for fine-grained tokens.'
        : 'Needs the repo scope, or contents:read for fine-grained tokens.'}
    >
      <Input
        id="gh-pat"
        type="password"
        bind:value={ghForm.pat}
        placeholder={ghIntegration?.has_pat ? 'Leave empty to keep the current token' : 'ghp_...'}
        autocomplete="new-password"
      />
    </FormField>
    <FormField
      label="Webhook secret"
      for="gh-webhook-secret"
      controlWidth="full"
      hint={ghIntegration?.has_webhook_secret
        ? 'A secret is stored. Leave empty to keep it. Use the same value in the GitHub webhook.'
        : 'At least 16 characters. The push webhook is refused until one is saved; use the same value in the GitHub webhook.'}
    >
      <Input
        id="gh-webhook-secret"
        type="password"
        bind:value={ghForm.webhookSecret}
        placeholder={ghIntegration?.has_webhook_secret ? 'Leave empty to keep the current secret' : 'A long random string'}
        autocomplete="new-password"
      />
    </FormField>
  </form>
  {#snippet footer()}
    <Button size="sm" variant="ghost" onclick={closeEdit}>Cancel</Button>
    <Button size="sm" onclick={() => void handleGitHubSave()} loading={ghSaving} disabled={!ghForm.repo.trim()}>Save</Button>
  {/snippet}
</Sheet>

<ConfirmDialog
  open={confirmRemoveOpen}
  title="Remove GitHub integration?"
  description="The repository link and token are deleted. Models already synced stay as they are."
  confirmLabel="Remove"
  destructive={true}
  onconfirm={handleGitHubDelete}
  oncancel={() => (confirmRemoveOpen = false)}
/>
