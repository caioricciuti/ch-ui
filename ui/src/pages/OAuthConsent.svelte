<script lang="ts">
  import { onMount } from 'svelte'
  import { getOAuthConsent, approveOAuthConsent, denyOAuthConsent, type OAuthConsentInfo } from '../lib/api/oauth'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import Button from '../lib/components/common/Button.svelte'
  import { ShieldCheck, Database, Eye, PencilLine } from 'lucide-svelte'
  
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

<div class="flex min-h-full items-center justify-center bg-canvas p-6">
  <div class="w-full max-w-md space-y-5 rounded-lg border border-edge-subtle bg-surface p-6">
    <div class="flex items-center gap-3">
      <div class="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-accent-soft text-accent">
        <ShieldCheck size={18} strokeWidth={1.75} />
      </div>
      <div class="min-w-0">
        <h1 class="text-[15px] font-semibold tracking-[-0.01em] text-fg">Connect an AI client to CH-UI</h1>
        <p class="text-xs text-fg-3">Model Context Protocol access request</p>
      </div>
    </div>

    {#if error}
      <p class="text-[13px] text-danger">{error}</p>
      <p class="text-xs text-fg-3">Go back to your MCP client and start the connection again.</p>
    {:else if !info}
      <div class="flex items-center gap-2 text-xs text-fg-3"><Spinner size="sm" /> Loading request…</div>
    {:else}
      <p class="text-[13px] leading-relaxed text-fg-2">
        <span class="font-semibold text-fg">{info.client_name}</span>
        <span class="text-fg-3">({info.redirect_host})</span>
        wants to query ClickHouse through CH-UI as
        <span class="font-mono text-fg">{info.user}</span>.
      </p>

      <div class="flex items-center gap-2 rounded-md bg-surface-2 px-3 py-2.5 text-xs text-fg-2">
        <Database size={14} class="text-accent" />
        Connection: <span class="font-medium text-fg">{info.connection}</span>
      </div>

      <ul class="space-y-2.5">
        {#each info.scopes as s}
          {@const t = scopeText[s] ?? { label: s, detail: '' }}
          <li class="flex items-start gap-2.5 text-xs">
            {#if s === 'write'}<PencilLine size={14} class="mt-0.5 shrink-0 text-accent" />{:else}<Eye size={14} class="mt-0.5 shrink-0 text-accent" />{/if}
            <div>
              <div class="font-medium text-fg">{t.label}</div>
              <div class="mt-0.5 leading-relaxed text-fg-3">{t.detail}</div>
            </div>
          </li>
        {/each}
      </ul>

      <p class="flex items-start gap-1.5 text-[11px] leading-relaxed text-fg-3">
        <ShieldCheck size={13} class="mt-0.5 shrink-0" />
        Every query the client runs is recorded in query history and the audit log under your name. Access lasts one hour and renews while the client is in use; an admin can revoke it in Admin → MCP Server.
      </p>

      <div class="flex justify-end gap-2">
        <Button size="sm" variant="ghost" onclick={() => decide(false)} disabled={busy}>Deny</Button>
        <Button size="sm" onclick={() => decide(true)} loading={busy}>Allow access</Button>
      </div>
    {/if}
  </div>
</div>
