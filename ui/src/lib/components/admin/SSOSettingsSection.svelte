<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { onMount } from 'svelte'
  import { fetchSSOSettings, updateSSOSettings } from '../../api/sso'
  import type { SSOSettings } from '../../api/sso'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import { isProActive } from '../../stores/license.svelte'
  import SectionHeader from '../common/SectionHeader.svelte'
  import Panel from '../common/Panel.svelte'
  import FormField from '../common/FormField.svelte'
  import Input from '../common/Input.svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Sheet from '../common/Sheet.svelte'
  import Spinner from '../common/Spinner.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import { KeyRound, Copy, Pencil } from 'lucide-svelte'

  // Admin > Settings > Single sign-on: wires an OIDC provider to CH-UI
  // roles. Shows the current state and edits through a Sheet.

  // ── SSO (OIDC) ─────────────────────────────────────────────
  let sso = $state<SSOSettings | null>(null)
  let ssoLoading = $state(false)
  let ssoSaving = $state(false)
  let ssoProRequired = $state(false)
  let ssoSheetOpen = $state(false)
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

  const ssoConfigured = $derived(!!sso && sso.config.issuer_url.trim().length > 0)
  const ssoGated = $derived(ssoProRequired || !isProActive())
  const ssoCanEdit = $derived(!!sso && !sso.env_managed && !ssoGated)

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

  function openSSOSheet() {
    if (sso) applySSOSettings(sso)
    ssoSheetOpen = true
  }

  async function saveSSO() {
    if (!ssoForm.issuer_url.trim() || !ssoForm.client_id.trim()) {
      toastError('Issuer URL and Client ID are required')
      return
    }
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
      ssoSheetOpen = false
      if (next.reload_error) {
        toastError(`Saved, but the SSO provider could not be initialized: ${next.reload_error}`)
      } else {
        toastSuccess('SSO settings saved and applied, no restart needed')
      }
    } catch (e: any) {
      toastError(e.message)
    } finally {
      ssoSaving = false
    }
  }

  // The callback the IdP must allow, previewed from the draft base URL.
  const callbackPreview = $derived.by(() => {
    const current = sso?.config.redirect_url ?? ''
    const base = ssoForm.redirect_base_url.trim().replace(/\/+$/, '')
    if (!base || !current) return current
    try {
      return `${base}${new URL(current).pathname}`
    } catch {
      return current
    }
  })

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

  onMount(() => {
    void loadSSO()
  })
</script>

<section>
  <SectionHeader
    title="Single sign-on (OIDC)"
    description="Let people sign in through Okta, Entra ID, Google or Keycloak. Group membership maps to CH-UI roles; queries run through the connection's ClickHouse service account. Pro feature."
  >
    {#snippet actions()}
      {#if ssoGated}
        <Badge tone="warning">Pro required</Badge>
      {:else if sso?.env_managed}
        <Badge tone="info">Managed by env</Badge>
      {:else if sso?.active}
        <Badge tone="success">Active</Badge>
      {:else if sso}
        <Badge>Inactive</Badge>
      {/if}
      {#if ssoCanEdit && ssoConfigured}
        <Button size="sm" variant="outline" onclick={openSSOSheet}>
          <Pencil size={13} /> Configure SSO
        </Button>
      {/if}
    {/snippet}
  </SectionHeader>

  {#if ssoGated}
    <Panel variant="muted">
      <div class="flex items-center gap-3">
        <KeyRound size={16} class="shrink-0 text-fg-4" />
        <p class="text-[13px] text-fg-3">SSO lets your team sign in through your identity provider. Activate a Pro license under License to configure it.</p>
      </div>
    </Panel>
  {:else if ssoLoading}
    <div class="flex items-center justify-center py-6"><Spinner /></div>
  {:else if sso && !ssoConfigured && !sso.env_managed}
    <Panel padding="none">
      <EmptyState
        icon={KeyRound}
        title="SSO is not configured"
        description="Point CH-UI at your OpenID Connect provider and map its groups to admin, analyst and viewer."
        primary={{ label: 'Configure SSO', onclick: openSSOSheet }}
      />
    </Panel>
  {:else if sso}
    <div class="space-y-3">
      {#if sso.env_managed}
        <Panel variant="muted" padding="sm">
          <p class="text-xs text-fg-3">
            SSO is configured through environment variables or the server config file, which take precedence. Edit the server config and restart to change it.
          </p>
        </Panel>
      {/if}
      <Panel padding="none">
        <dl class="divide-y divide-edge-subtle text-[13px]">
          <div class="flex items-center gap-4 px-4 py-2.5">
            <dt class="w-40 shrink-0 text-fg-3">Status</dt>
            <dd class="flex min-w-0 flex-1 items-center gap-2">
              {#if sso.active}<Badge tone="success">Active</Badge>{:else}<Badge>Inactive</Badge>{/if}
              {#if sso.config.enabled && !sso.active}<span class="text-xs text-fg-3">Enabled, but the provider is not serving logins.</span>{/if}
              {#if sso.reload_error}<span class="text-xs text-danger">{sso.reload_error}</span>{/if}
            </dd>
          </div>
          <div class="flex items-center gap-4 px-4 py-2.5">
            <dt class="w-40 shrink-0 text-fg-3">Issuer</dt>
            <dd class="min-w-0 flex-1 truncate font-mono text-xs text-fg">{sso.config.issuer_url}</dd>
          </div>
          <div class="flex items-center gap-4 px-4 py-2.5">
            <dt class="w-40 shrink-0 text-fg-3">Client ID</dt>
            <dd class="min-w-0 flex-1 truncate font-mono text-xs text-fg">{sso.config.client_id || '—'}</dd>
          </div>
          <div class="flex items-center gap-4 px-4 py-2.5">
            <dt class="w-40 shrink-0 text-fg-3">Client secret</dt>
            <dd class="min-w-0 flex-1">
              {#if sso.config.has_secret}<Badge tone="success">Set</Badge>{:else}<Badge tone="warning">Not set</Badge>{/if}
            </dd>
          </div>
          <div class="flex items-center gap-4 px-4 py-2.5">
            <dt class="w-40 shrink-0 text-fg-3">Callback URL</dt>
            <dd class="flex min-w-0 flex-1 items-center gap-1.5">
              <code class="min-w-0 flex-1 truncate font-mono text-xs text-fg">{sso.config.redirect_url}</code>
              <Button icon size="xs" variant="ghost" aria-label="Copy callback URL" title="Copy callback URL" onclick={() => copyText(sso!.config.redirect_url, 'Callback URL')}><Copy size={13} /></Button>
            </dd>
          </div>
          {#each [
            { label: 'Admin groups', values: sso.config.admin_groups },
            { label: 'Analyst groups', values: sso.config.analyst_groups },
            { label: 'Viewer groups', values: sso.config.viewer_groups },
            { label: 'Allowed domains', values: sso.config.allowed_domains },
          ] as row (row.label)}
            <div class="flex items-center gap-4 px-4 py-2.5">
              <dt class="w-40 shrink-0 text-fg-3">{row.label}</dt>
              <dd class="flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
                {#if row.values.length === 0}
                  <span class="text-xs text-fg-4">{row.label === 'Allowed domains' ? 'any' : 'none'}</span>
                {:else}
                  {#each row.values as v (v)}<Badge>{v}</Badge>{/each}
                {/if}
              </dd>
            </div>
          {/each}
        </dl>
      </Panel>
    </div>
  {:else}
    <p class="text-[13px] text-fg-3">Could not load SSO settings.</p>
  {/if}

  <!-- Configure SSO -->
  <Sheet
    open={ssoSheetOpen}
    title="Configure SSO"
    description="Changes apply immediately; the provider is re-initialized on save, no restart needed."
    size="lg"
    onclose={() => (ssoSheetOpen = false)}
  >
    <div class="space-y-6">
      <div class="space-y-3">
        <h3 class="text-[13px] font-semibold text-fg">Provider</h3>
        <FormField label="Issuer URL" for="sso-issuer_url" required hint="Your identity provider's OpenID Connect issuer." controlWidth="full">
          <Input id="sso-issuer_url" autocomplete="off" placeholder="https://accounts.google.com" bind:value={ssoForm.issuer_url} />
        </FormField>
        <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
          <FormField label="Client ID" for="sso-client_id" required controlWidth="full">
            <Input id="sso-client_id" autocomplete="off" placeholder="your-client-id" bind:value={ssoForm.client_id} />
          </FormField>
          <FormField label="Client secret" for="sso-secret" controlWidth="full" hint={sso?.config.has_secret ? 'A secret is stored. Leave empty to keep it.' : 'Stored encrypted; never shown again.'}>
            <Input id="sso-secret" type="password" autocomplete="new-password" placeholder={sso?.config.has_secret ? '••••••••' : 'client secret'} bind:value={ssoSecret} />
          </FormField>
        </div>
      </div>

      <div class="space-y-3">
        <h3 class="text-[13px] font-semibold text-fg">Redirect</h3>
        <FormField label="Redirect base URL" for="sso-redirect_base_url" controlWidth="full" hint="Defaults to the server URL. Set it when CH-UI sits behind a proxy.">
          <Input id="sso-redirect_base_url" autocomplete="off" placeholder="https://ch-ui.yourcompany.com" bind:value={ssoForm.redirect_base_url} />
        </FormField>
        <div class="flex items-center gap-2 rounded-md bg-surface-2 px-3 py-2">
          <span class="shrink-0 text-xs text-fg-3">Callback</span>
          <code class="min-w-0 flex-1 truncate font-mono text-xs text-fg-2">{callbackPreview}</code>
          <Button icon size="xs" variant="ghost" aria-label="Copy callback URL" title="Copy callback URL" onclick={() => copyText(callbackPreview, 'Callback URL')}><Copy size={13} /></Button>
        </div>
        <p class="text-xs text-fg-4">Register this redirect URI in your identity provider.</p>
      </div>

      <div class="space-y-3">
        <h3 class="text-[13px] font-semibold text-fg">Role mapping</h3>
        <p class="text-xs text-fg-3">Comma-separated group names from the provider. The first list a user matches wins; users in none of them are viewers.</p>
        <FormField label="Admin groups" for="sso-admin_groups" controlWidth="full">
          <Input id="sso-admin_groups" autocomplete="off" placeholder="ch-ui-admins, platform" bind:value={ssoForm.admin_groups} />
        </FormField>
        <FormField label="Analyst groups" for="sso-analyst_groups" controlWidth="full" hint="Can create and edit shared objects.">
          <Input id="sso-analyst_groups" autocomplete="off" placeholder="data-analysts" bind:value={ssoForm.analyst_groups} />
        </FormField>
        <FormField label="Viewer groups" for="sso-viewer_groups" controlWidth="full">
          <Input id="sso-viewer_groups" autocomplete="off" placeholder="everyone" bind:value={ssoForm.viewer_groups} />
        </FormField>
      </div>

      <div class="space-y-3">
        <h3 class="text-[13px] font-semibold text-fg">Access</h3>
        <FormField label="Allowed email domains" for="sso-allowed_domains" controlWidth="full" hint="Empty allows every domain the provider returns.">
          <Input id="sso-allowed_domains" autocomplete="off" placeholder="yourcompany.com" bind:value={ssoForm.allowed_domains} />
        </FormField>
        <label class="ds-checkbox-label">
          <input type="checkbox" class="ds-checkbox" bind:checked={ssoForm.enabled} />
          Enable sign-in through the identity provider
        </label>
      </div>
    </div>
    {#snippet footer()}
      <Button size="sm" variant="ghost" onclick={() => (ssoSheetOpen = false)}>Cancel</Button>
      <Button size="sm" onclick={() => saveSSO()} loading={ssoSaving}>Save changes</Button>
    {/snippet}
  </Sheet>
</section>
