<script lang="ts">
  import { onMount } from 'svelte'
  import { getGitHubIntegration, saveGitHubIntegration, deleteGitHubIntegration, testGitHubConnection, triggerGitHubSync, getGitHubSyncLogs } from '../../api/github'
  import type { GitHubIntegration, GitHubSyncLog } from '../../types/models'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import { isProActive } from '../../stores/license.svelte'
  import { getSession } from '../../stores/session.svelte'
  import { goTo } from '../../stores/router.svelte'
  import { formatDate } from '../../utils/format'
  import PageBody from '../common/PageBody.svelte'
  import SectionHeader from '../common/SectionHeader.svelte'
  import Panel from '../common/Panel.svelte'
  import FormField from '../common/FormField.svelte'
  import Input from '../common/Input.svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Spinner from '../common/Spinner.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import { Check, X as XIcon, Lock } from 'lucide-svelte'

  // GitHub model sync: the repository is the source of truth for SQL models.
  let ghIntegration = $state<GitHubIntegration | null>(null)
  let ghLoading = $state(false)
  let ghSaving = $state(false)
  let ghTesting = $state(false)
  let ghTestResult = $state<{ success: boolean; error?: string } | null>(null)
  let ghSyncing = $state(false)
  let ghSyncLogs = $state<GitHubSyncLog[]>([])
  let ghForm = $state({ repo: '', branch: 'main', path: 'models/', pat: '' })
  let confirmRemoveOpen = $state(false)

  const licensedPro = $derived(isProActive())
  const webhookUrl = $derived(`${window.location.origin}/api/github/webhook/${getSession()?.connectionId ?? ''}`)

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
      ghSyncLogs = []
      ghForm = { repo: '', branch: 'main', path: 'models/', pat: '' }
      toastSuccess('GitHub integration removed')
    } catch (e: unknown) {
      toastError((e as Error).message || 'Failed to remove')
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
      {:else}
        <Panel padding="none">
          <div class="divide-y divide-edge-subtle px-4">
            <FormField layout="row" label="Repository" for="gh-repo" hint="owner/repo on github.com." required>
              <Input id="gh-repo" bind:value={ghForm.repo} placeholder="owner/repo" mono />
            </FormField>
            <FormField layout="row" label="Branch" for="gh-branch" hint="The branch that is synced.">
              <Input id="gh-branch" bind:value={ghForm.branch} placeholder="main" mono />
            </FormField>
            <FormField layout="row" label="Models path" for="gh-path" hint="Folder inside the repository that holds the .sql files.">
              <Input id="gh-path" bind:value={ghForm.path} placeholder="models/" mono />
            </FormField>
            <FormField
              layout="row"
              label="Personal access token"
              for="gh-pat"
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
          </div>
          <div class="flex flex-wrap items-center gap-2 border-t border-edge-subtle px-4 py-3">
            {#if ghTestResult}
              <span class="inline-flex items-center gap-1.5 text-xs {ghTestResult.success ? 'text-success' : 'text-danger'}">
                {#if ghTestResult.success}
                  <Check size={13} /> Connection successful
                {:else}
                  <XIcon size={13} /> {ghTestResult.error || 'Connection failed'}
                {/if}
              </span>
            {/if}
            <div class="ml-auto flex items-center gap-2">
              {#if ghIntegration}
                <Button size="sm" variant="ghost" class="text-danger hover:bg-danger-soft hover:text-danger" onclick={() => (confirmRemoveOpen = true)}>Remove</Button>
              {/if}
              {#if ghIntegration?.has_pat}
                <Button size="sm" variant="outline" onclick={handleGitHubTest} loading={ghTesting}>Test connection</Button>
              {/if}
              <Button size="sm" onclick={handleGitHubSave} loading={ghSaving} disabled={!ghForm.repo.trim()}>Save</Button>
            </div>
          </div>
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
            <FormField layout="row" label="Webhook URL" hint="Optional. Add it as a GitHub webhook (push events) to sync automatically." controlWidth="full">
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

<ConfirmDialog
  open={confirmRemoveOpen}
  title="Remove GitHub integration?"
  description="The repository link and token are deleted. Models already synced stay as they are."
  confirmLabel="Remove"
  destructive={true}
  onconfirm={handleGitHubDelete}
  oncancel={() => (confirmRemoveOpen = false)}
/>
