<script lang="ts">
  import { onMount } from 'svelte'
  import { getOAuthConsent, approveOAuthConsent, denyOAuthConsent, type OAuthConsentInfo } from '../lib/api/oauth'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import { ShieldCheck, Database, Eye, PencilLine } from 'lucide-svelte'
  import logo from '../assets/logo.png'

  interface Props {
    requestId: string
  }
  let { requestId }: Props = $props()

  let info = $state<OAuthConsentInfo | null>(null)
  let error = $state<string | null>(null)
  let busy = $state(false)

  const scopeText: Record<string, { label: string; detail: string }> = {
    read: { label: 'Read', detail: 'Browse schemas and run read-only queries as you, with your ClickHouse grants and CH-UI guardrails.' },
    write: { label: 'Create drafts', detail: 'Save queries and create draft dashboards, models and pipelines in CH-UI. Nothing runs against ClickHouse.' },
  }

  onMount(async () => {
    try {
      info = await getOAuthConsent(requestId)
    } catch (e) {
      error = e instanceof Error ? e.message : 'This authorization request is no longer valid.'
    }
  })

  async function decide(approve: boolean) {
    busy = true
    try {
      const res = approve ? await approveOAuthConsent(requestId) : await denyOAuthConsent(requestId)
      window.location.assign(res.redirect_url)
    } catch (e) {
      error = e instanceof Error ? e.message : 'Could not complete the request.'
      busy = false
    }
  }
</script>

<div class="min-h-full flex items-center justify-center p-6">
  <div class="ds-panel w-full max-w-md p-6 space-y-5">
    <div class="flex items-center gap-3">
      <img src={logo} alt="CH-UI" class="w-9 h-9 rounded-lg ring-1 ring-white/20" />
      <div>
        <h1 class="text-sm font-semibold text-gray-900 dark:text-gray-100">Connect an AI client to CH-UI</h1>
        <p class="text-xs text-gray-500 dark:text-gray-400">Model Context Protocol access request</p>
      </div>
    </div>

    {#if error}
      <p class="text-sm text-red-600 dark:text-red-400">{error}</p>
      <p class="text-xs text-gray-500 dark:text-gray-400">Go back to your MCP client and start the connection again.</p>
    {:else if !info}
      <div class="flex items-center gap-2 text-xs text-gray-500"><Spinner size="sm" /> Loading request…</div>
    {:else}
      <p class="text-sm text-gray-700 dark:text-gray-300">
        <span class="font-semibold text-gray-900 dark:text-gray-100">{info.client_name}</span>
        <span class="text-gray-500 dark:text-gray-400">({info.redirect_host})</span>
        wants to query ClickHouse through CH-UI as
        <span class="font-mono text-gray-900 dark:text-gray-100">{info.user}</span>.
      </p>

      <div class="ds-panel-muted p-3 flex items-center gap-2 text-xs text-gray-600 dark:text-gray-300">
        <Database size={14} class="text-ch-orange" />
        Connection: <span class="font-medium text-gray-900 dark:text-gray-100">{info.connection}</span>
      </div>

      <ul class="space-y-2">
        {#each info.scopes as s}
          {@const t = scopeText[s] ?? { label: s, detail: '' }}
          <li class="flex items-start gap-2 text-xs">
            {#if s === 'write'}<PencilLine size={14} class="mt-0.5 text-ch-orange" />{:else}<Eye size={14} class="mt-0.5 text-ch-orange" />{/if}
            <div>
              <div class="font-medium text-gray-900 dark:text-gray-100">{t.label}</div>
              <div class="text-gray-500 dark:text-gray-400">{t.detail}</div>
            </div>
          </li>
        {/each}
      </ul>

      <p class="text-[11px] text-gray-500 dark:text-gray-400 flex items-start gap-1.5">
        <ShieldCheck size={13} class="mt-0.5 shrink-0" />
        Every query the client runs is recorded in query history and the audit log under your name. Access lasts one hour and renews while the client is in use; an admin can revoke it in Admin → MCP Server.
      </p>

      <div class="flex justify-end gap-2">
        <button class="ds-btn-ghost px-3 py-1.5" onclick={() => decide(false)} disabled={busy}>Deny</button>
        <button class="ds-btn-primary px-3 py-1.5" onclick={() => decide(true)} disabled={busy}>
          {busy ? 'Working…' : 'Allow access'}
        </button>
      </div>
    {/if}
  </div>
</div>
