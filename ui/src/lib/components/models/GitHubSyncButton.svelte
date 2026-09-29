<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { onMount } from 'svelte'
  import { CloudDownload } from 'lucide-svelte'
  import { triggerGitHubSync, getGitHubIntegration } from '../../api/github'
  import { isProActive } from '../../stores/license.svelte'
  import { getSession } from '../../stores/session.svelte'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import Button from '../common/Button.svelte'

  interface Props {
    /** Called after a successful sync so the page can reload its models. */
    onsynced: () => Promise<void> | void
  }

  let { onsynced }: Props = $props()

  let syncing = $state(false)
  let hasGitHubIntegration = $state(false)

  onMount(() => {
    checkGitHubIntegration()
  })

  async function checkGitHubIntegration() {
    if (!isProActive()) return
    try {
      const session = getSession()
      if (!session) return
      const integration = await getGitHubIntegration(session.connectionId)
      hasGitHubIntegration = !!(integration?.enabled && integration?.has_pat)
    } catch { /* ignore */ }
  }

  async function handleGitHubSync() {
    const session = getSession()
    if (!session || syncing) return
    syncing = true
    try {
      const result = await triggerGitHubSync(session.connectionId)
      const parts: string[] = []
      if (result.created > 0) parts.push(`${result.created} created`)
      if (result.updated > 0) parts.push(`${result.updated} updated`)
      if (result.deleted > 0) parts.push(`${result.deleted} deleted`)
      if (result.unchanged > 0) parts.push(`${result.unchanged} unchanged`)
      toastSuccess(parts.length > 0 ? `Sync complete: ${parts.join(', ')}` : 'Already up to date')
      await onsynced()
    } catch (e: unknown) {
      toastError((e as Error).message || 'Sync failed')
    } finally {
      syncing = false
    }
  }
</script>

{#if hasGitHubIntegration}
  <Button size="sm" variant="outline" onclick={handleGitHubSync} disabled={syncing} title="Sync models from GitHub">
    <CloudDownload size={13} class={syncing ? 'animate-pulse' : ''} /> {syncing ? 'Syncing...' : 'Sync GitHub'}
  </Button>
{/if}
