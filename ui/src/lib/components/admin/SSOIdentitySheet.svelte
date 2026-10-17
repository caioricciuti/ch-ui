<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { onMount, tick } from 'svelte'
  import { ApiError } from '../../api/client'
  import {
    defaultSSOIdentityConfig,
    fetchSSOIdentity,
    fetchSSOSetupSQL,
    updateSSOAccount,
    updateSSOIdentity,
    verifySSOIdentity,
  } from '../../api/ssoIdentity'
  import type { SSOIdentityConfig, SSOIdentityMode, SSOVerifyCheck } from '../../api/ssoIdentity'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import { isProActive } from '../../stores/license.svelte'
  import Sheet from '../common/Sheet.svelte'
  import Panel from '../common/Panel.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import Button from '../common/Button.svelte'
  import Input from '../common/Input.svelte'
  import Select from '../common/Select.svelte'
  import FormField from '../common/FormField.svelte'
  import Badge from '../common/Badge.svelte'
  import Spinner from '../common/Spinner.svelte'
  import { Copy, Plus, Trash2, Check, X } from 'lucide-svelte'

  // Admin > Connections > SSO: how SSO people reach ClickHouse on this
  // connection. Shared mode uses one service account; per-person mode (Pro)
  // provisions a ClickHouse user per person from their IdP groups.

  let { connection, onclose }: {
    connection: { id: string; name: string }
    onclose: () => void
  } = $props()

  type HostMode = 'auto' | 'any' | 'custom'
  type MappingRow = { group: string; roles: string }

  let loading = $state(true)
  let loadError = $state('')
  let saving = $state(false)
  let savingAccount = $state(false)
  let verifying = $state(false)
  let loadingSQL = $state(false)
  /** The API answered 402: per-person needs Pro even if the license store disagrees. */
  let proRequired = $state(false)
  /** A 400 from PUT sso-identity, shown next to the form. */
  let formError = $state('')

  let provisionerUser = $state('')
  let provisionerSet = $state(false)

  let mode = $state<SSOIdentityMode>('shared')
  let userPrefix = $state('sso:')
  let settingsProfile = $state('')
  let hostMode = $state<HostMode>('auto')
  let hostCustom = $state('')
  let mappings = $state<MappingRow[]>([])
  let fallbackRoles = $state('')
  let lockAfterDays = $state('0')
  let dropAfterDays = $state('0')

  let accountUsername = $state('')
  let accountPassword = $state('')

  let verifyChecks = $state<SSOVerifyCheck[] | null>(null)
  let verifyOk = $state(false)

  let sqlOpen = $state(false)
  let sqlText = $state('')
  let sqlPre = $state<HTMLPreElement | undefined>()

  const proGated = $derived(proRequired || !isProActive())
  const perPerson = $derived(mode === 'per_person')
  const accountLabel = $derived(perPerson ? 'Provisioning account' : 'Service account')
  const busy = $derived(saving || savingAccount || verifying || loadingSQL)

  const hostOptions = [
    { value: 'auto', label: 'auto (this server\'s address)' },
    { value: 'any', label: 'any' },
    { value: 'custom', label: 'custom IP list' },
  ]

  function splitCsv(value: string): string[] {
    return value.split(',').map((s) => s.trim()).filter((s) => s.length > 0)
  }

  function parseDays(value: string): number {
    const n = Number(value)
    return Number.isFinite(n) && n > 0 ? Math.floor(n) : 0
  }

  function applyConfig(config: SSOIdentityConfig) {
    mode = config.mode
    userPrefix = config.user_prefix || 'sso:'
    settingsProfile = config.settings_profile ?? ''
    if (config.host === 'auto' || config.host === 'any' || !config.host) {
      hostMode = config.host === 'any' ? 'any' : 'auto'
      hostCustom = ''
    } else {
      hostMode = 'custom'
      hostCustom = config.host
    }
    mappings = (config.role_mappings ?? []).map((m) => ({ group: m.group, roles: m.roles.join(', ') }))
    fallbackRoles = (config.fallback_roles ?? []).join(', ')
    lockAfterDays = String(config.lock_after_days ?? 0)
    dropAfterDays = String(config.drop_after_days ?? 0)
  }

  function buildConfig(): SSOIdentityConfig {
    return {
      mode,
      user_prefix: userPrefix.trim() || 'sso:',
      settings_profile: settingsProfile.trim(),
      host: hostMode === 'custom' ? hostCustom.trim() : hostMode,
      role_mappings: mappings
        .map((m) => ({ group: m.group.trim(), roles: splitCsv(m.roles) }))
        .filter((m) => m.group.length > 0),
      fallback_roles: splitCsv(fallbackRoles),
      lock_after_days: parseDays(lockAfterDays),
      drop_after_days: parseDays(dropAfterDays),
    }
  }

  async function load(silent = false) {
    if (!silent) loading = true
    loadError = ''
    try {
      const res = await fetchSSOIdentity(connection.id)
      applyConfig(res.config ?? defaultSSOIdentityConfig())
      provisionerUser = res.provisioner_user ?? ''
      provisionerSet = !!res.provisioner_set
      accountUsername = provisionerUser
      accountPassword = ''
    } catch (e) {
      loadError = e instanceof Error ? e.message : 'Could not load SSO identity settings'
    } finally {
      loading = false
    }
  }

  onMount(() => {
    void load()
  })

  function describe(e: unknown, fallback: string): string {
    return e instanceof Error && e.message ? e.message : fallback
  }

  /** 402 means per-person needs Pro; flip the hint on and tell the caller. */
  function handleProError(e: unknown): boolean {
    if (e instanceof ApiError && e.status === 402) {
      proRequired = true
      return true
    }
    return false
  }

  function selectMode(next: SSOIdentityMode) {
    if (next === 'per_person' && proGated) return
    mode = next
    formError = ''
    verifyChecks = null
  }

  function addMapping() {
    mappings = [...mappings, { group: '', roles: '' }]
  }

  function removeMapping(index: number) {
    mappings = mappings.filter((_, i) => i !== index)
  }

  async function save() {
    if (busy || loading || loadError) return
    saving = true
    formError = ''
    try {
      await updateSSOIdentity(connection.id, buildConfig())
      toastSuccess('SSO identity settings saved')
      await load(true)
    } catch (e) {
      if (handleProError(e)) {
        formError = 'Per-person ClickHouse users need a Pro license.'
      } else if (e instanceof ApiError && e.status === 400) {
        formError = describe(e, 'Invalid settings')
      } else {
        toastError(describe(e, 'Could not save SSO identity settings'))
      }
    } finally {
      saving = false
    }
  }

  async function saveAccount() {
    if (busy || loading || loadError) return
    const username = accountUsername.trim()
    if (!username) {
      toastError('ClickHouse username is required')
      return
    }
    savingAccount = true
    try {
      await updateSSOAccount(connection.id, { username, password: accountPassword })
      accountPassword = ''
      toastSuccess(`${accountLabel} saved`)
      await load(true)
    } catch (e) {
      toastError(describe(e, `Could not save the ${accountLabel.toLowerCase()}`))
    } finally {
      savingAccount = false
    }
  }

  async function showSetupSQL() {
    if (busy) return
    loadingSQL = true
    try {
      const res = await fetchSSOSetupSQL(connection.id, {
        config: buildConfig(),
        provisioner_user: accountUsername.trim() || provisionerUser,
      })
      sqlText = res.sql ?? ''
      sqlOpen = true
      await tick()
      sqlPre?.scrollIntoView({ block: 'nearest' })
    } catch (e) {
      if (handleProError(e)) {
        toastError('Per-person ClickHouse users need a Pro license.')
      } else {
        toastError(describe(e, 'Could not build the setup SQL'))
      }
    } finally {
      loadingSQL = false
    }
  }

  async function verify() {
    if (busy || !perPerson) return
    verifying = true
    verifyChecks = null
    try {
      const username = accountUsername.trim()
      const res = await verifySSOIdentity(connection.id, {
        config: buildConfig(),
        provisioner: accountPassword && username ? { username, password: accountPassword } : undefined,
      })
      verifyChecks = res.checks ?? []
      verifyOk = !!res.ok
    } catch (e) {
      if (handleProError(e)) {
        toastError('Per-person ClickHouse users need a Pro license.')
      } else {
        toastError(describe(e, 'Verification failed'))
      }
    } finally {
      verifying = false
    }
  }

  function selectSQLText() {
    if (!sqlPre) return
    const selection = window.getSelection()
    if (!selection) return
    const range = document.createRange()
    range.selectNodeContents(sqlPre)
    selection.removeAllRanges()
    selection.addRange(range)
  }

  async function copySQL() {
    if (!sqlText) return
    try {
      await navigator.clipboard.writeText(sqlText)
      toastSuccess('Setup SQL copied')
    } catch {
      selectSQLText()
      toastError('Clipboard unavailable. The SQL is selected, copy it with the keyboard.')
    }
  }

  function close() {
    if (!busy) onclose()
  }
</script>

<Sheet
  open
  title={`SSO identity · ${connection.name}`}
  description="How people who sign in through SSO reach ClickHouse on this connection."
  size="lg"
  onclose={close}
>
  {#if loading}
    <div class="flex items-center justify-center py-6"><Spinner /></div>
  {:else if loadError}
    <p role="alert" class="text-[13px] text-danger">{loadError}</p>
  {:else}
    <form id="sso-identity-form" class="space-y-6" onsubmit={(e) => { e.preventDefault(); void save() }}>
      <div class="space-y-3">
        <h3 class="text-[13px] font-semibold text-fg">Mode</h3>
        <div class="space-y-2">
          <label class="flex cursor-pointer items-start gap-3 rounded-md border px-3 py-2.5 {mode === 'shared' ? 'border-accent' : 'border-edge-subtle'}">
            <input type="radio" class="ds-radio mt-0.5" name="sso-identity-mode" value="shared" checked={mode === 'shared'} disabled={busy} onchange={() => selectMode('shared')} />
            <span class="min-w-0">
              <span class="block text-[13px] font-medium text-fg">Shared account</span>
              <span class="block text-xs text-fg-3">ClickHouse sees one service account for every SSO person; CH-UI roles still apply.</span>
            </span>
          </label>
          <label class="flex items-start gap-3 rounded-md border px-3 py-2.5 {proGated ? 'cursor-not-allowed opacity-60' : 'cursor-pointer'} {mode === 'per_person' ? 'border-accent' : 'border-edge-subtle'}">
            <input type="radio" class="ds-radio mt-0.5" name="sso-identity-mode" value="per_person" checked={mode === 'per_person'} disabled={busy || proGated} onchange={() => selectMode('per_person')} />
            <span class="min-w-0">
              <span class="flex items-center gap-2 text-[13px] font-medium text-fg">Per-person ClickHouse users <Badge tone="brand">Pro</Badge></span>
              <span class="block text-xs text-fg-3">ClickHouse sees one user per person, created from their IdP groups, so grants and query logs name the individual.</span>
              {#if proGated}
                <span class="block pt-1 text-xs text-fg-4">Per-person ClickHouse users need a Pro license. Activate one under License to switch.</span>
              {/if}
            </span>
          </label>
        </div>
      </div>

      <div class="space-y-3">
        <div class="flex items-center gap-2">
          <h3 class="text-[13px] font-semibold text-fg">{accountLabel}</h3>
          {#if provisionerSet}
            <Badge tone="success">Set</Badge>
          {:else}
            <Badge tone="warning">Not set</Badge>
          {/if}
        </div>
        <p class="text-xs text-fg-3">
          {#if perPerson}
            The ClickHouse account CH-UI uses to create, alter and drop the per-person users. It needs ACCESS MANAGEMENT and must be able to grant the mapped roles; Show setup SQL writes the grants for you.
          {:else}
            The ClickHouse account every SSO person queries as on this connection. Grant it only what SSO people should be able to do.
          {/if}
        </p>
        <div class="flex items-center gap-4 rounded-md bg-surface-2 px-3 py-2 text-[13px]">
          <span class="shrink-0 text-fg-3">Current account</span>
          <code class="min-w-0 flex-1 truncate font-mono text-xs text-fg">{provisionerUser || 'not set'}</code>
        </div>
        <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
          <FormField label="ClickHouse username" for="sso-account-user" controlWidth="full">
            <Input id="sso-account-user" size="sm" autocomplete="off" bind:value={accountUsername} disabled={busy} maxlength={256} spellcheck={false} />
          </FormField>
          <FormField label="ClickHouse password" for="sso-account-password" controlWidth="full" hint="Never shown again. Typed here, it is also used by Verify.">
            <Input id="sso-account-password" size="sm" type="password" autocomplete="new-password" bind:value={accountPassword} disabled={busy} />
          </FormField>
        </div>
        <div class="flex justify-end">
          <Button size="sm" variant="outline" loading={savingAccount} disabled={busy || !accountUsername.trim()} onclick={() => void saveAccount()}>Save account</Button>
        </div>
      </div>

      {#if perPerson}
        <div class="space-y-3">
          <h3 class="text-[13px] font-semibold text-fg">Provisioned users</h3>
          <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
            <FormField label="User prefix" for="sso-prefix" controlWidth="full" hint="Prepended to the person's email to name their ClickHouse user.">
              <Input id="sso-prefix" size="sm" mono autocomplete="off" placeholder="sso:" bind:value={userPrefix} disabled={busy} spellcheck={false} />
            </FormField>
            <FormField label="Settings profile" for="sso-profile" controlWidth="full" hint="Optional ClickHouse settings profile applied to each user.">
              <Input id="sso-profile" size="sm" mono autocomplete="off" placeholder="default" bind:value={settingsProfile} disabled={busy} spellcheck={false} />
            </FormField>
          </div>
          <FormField label="Allowed host" for="sso-host" controlWidth="full" hint="Which addresses the provisioned users may connect from. auto uses the address this server reaches ClickHouse from.">
            <Select id="sso-host" size="sm" options={hostOptions} value={hostMode} disabled={busy} onchange={(v) => (hostMode = v as HostMode)} />
          </FormField>
          {#if hostMode === 'custom'}
            <FormField label="IP list" for="sso-host-ips" controlWidth="full" hint="Comma-separated IP addresses.">
              <Input id="sso-host-ips" size="sm" mono autocomplete="off" placeholder="10.0.0.5, 10.0.1.0/24" bind:value={hostCustom} disabled={busy} spellcheck={false} />
            </FormField>
          {/if}
        </div>

        <div class="space-y-3">
          <div class="flex items-center justify-between gap-3">
            <div>
              <h3 class="text-[13px] font-semibold text-fg">Group to roles</h3>
              <p class="mt-0.5 text-xs text-fg-3">Each IdP group grants the listed ClickHouse roles. A person gets the union of every group they are in.</p>
            </div>
            <Button size="xs" variant="outline" disabled={busy} onclick={addMapping}><Plus size={12} /> Add group</Button>
          </div>
          {#if mappings.length === 0}
            <EmptyState size="compact" title="No groups mapped" description="People will only get the fallback roles." />
          {:else}
            <div class="overflow-hidden rounded-lg border border-edge-subtle">
              <table class="w-full border-separate border-spacing-0 text-[13px]">
                <thead>
                  <tr>
                    <th class="h-8 border-b border-edge bg-surface px-3 text-left text-xs font-medium text-fg-3">IdP group</th>
                    <th class="h-8 border-b border-edge bg-surface px-3 text-left text-xs font-medium text-fg-3">ClickHouse roles</th>
                    <th class="h-8 w-9 border-b border-edge bg-surface"><span class="sr-only">Remove</span></th>
                  </tr>
                </thead>
                <tbody>
                  {#each mappings as row, i (i)}
                    <tr>
                      <td class="border-b border-edge-subtle px-2 py-1">
                        <label for={`sso-map-group-${i}`} class="sr-only">Group {i + 1}</label>
                        <Input size="sm" autocomplete="off" placeholder="data-analysts" id={`sso-map-group-${i}`} bind:value={row.group} disabled={busy} spellcheck={false} />
                      </td>
                      <td class="border-b border-edge-subtle px-2 py-1">
                        <label for={`sso-map-roles-${i}`} class="sr-only">Roles for group {i + 1}</label>
                        <Input size="sm" mono autocomplete="off" placeholder="analyst_ro, events_reader" id={`sso-map-roles-${i}`} bind:value={row.roles} disabled={busy} spellcheck={false} />
                      </td>
                      <td class="border-b border-edge-subtle px-1 py-1 text-right">
                        <Button icon variant="ghost" size="xs" aria-label="Remove group" title="Remove" disabled={busy} onclick={() => removeMapping(i)}><Trash2 size={13} /></Button>
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          {/if}
          <FormField label="Fallback roles" for="sso-fallback" controlWidth="full" hint="Comma-separated. Granted when no group matches; empty refuses sign-in when no group matches.">
            <Input id="sso-fallback" size="sm" mono autocomplete="off" placeholder="viewer_ro" bind:value={fallbackRoles} disabled={busy} spellcheck={false} />
          </FormField>
        </div>

        <div class="space-y-3">
          <h3 class="text-[13px] font-semibold text-fg">Lifecycle</h3>
          <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
            <FormField label="Lock after days" for="sso-lock-days" controlWidth="full" hint="Days without sign-in before the ClickHouse user is locked. 0 = never.">
              <Input id="sso-lock-days" size="sm" type="number" min={0} step={1} bind:value={lockAfterDays} disabled={busy} />
            </FormField>
            <FormField label="Drop after days" for="sso-drop-days" controlWidth="full" hint="Days without sign-in before the ClickHouse user is dropped. 0 = never.">
              <Input id="sso-drop-days" size="sm" type="number" min={0} step={1} bind:value={dropAfterDays} disabled={busy} />
            </FormField>
          </div>
        </div>
      {/if}

      {#if formError}
        <div role="alert" class="rounded-md bg-danger-soft px-3 py-2 text-[13px] text-danger">{formError}</div>
      {/if}

      {#if sqlOpen}
        <Panel variant="muted" padding="sm" title="Setup SQL" description="Run this as a ClickHouse admin. It creates what the current settings need.">
          {#snippet actions()}
            <Button size="xs" variant="outline" onclick={() => void copySQL()}><Copy size={12} /> Copy</Button>
            <Button size="xs" variant="ghost" onclick={() => (sqlOpen = false)}>Done</Button>
          {/snippet}
          <pre bind:this={sqlPre} class="max-h-80 overflow-auto whitespace-pre-wrap break-words rounded-md border border-edge-subtle bg-surface p-3 font-mono text-[11px] text-fg">{sqlText || '-- nothing to run'}</pre>
        </Panel>
      {/if}

      {#if verifyChecks}
        <div class="space-y-2">
          <div class="flex items-center gap-2">
            <h3 class="text-[13px] font-semibold text-fg">Verification</h3>
            {#if verifyOk}<Badge tone="success">All checks passed</Badge>{:else}<Badge tone="danger">Some checks failed</Badge>{/if}
          </div>
          {#if verifyChecks.length === 0}
            <p class="text-xs text-fg-4">The server returned no checks.</p>
          {:else}
            <ul class="divide-y divide-edge-subtle rounded-lg border border-edge-subtle">
              {#each verifyChecks as check (check.name)}
                <li class="flex items-start gap-2.5 px-3 py-2">
                  {#if check.ok}
                    <Check size={14} class="mt-0.5 shrink-0 text-success" aria-label="Passed" />
                  {:else}
                    <X size={14} class="mt-0.5 shrink-0 text-danger" aria-label="Failed" />
                  {/if}
                  <div class="min-w-0">
                    <div class="text-[13px] text-fg">{check.name}</div>
                    {#if check.detail}<div class="text-xs text-fg-3">{check.detail}</div>{/if}
                  </div>
                </li>
              {/each}
            </ul>
          {/if}
        </div>
      {/if}
    </form>
  {/if}
  {#snippet footer()}
    <Button variant="ghost" size="sm" disabled={busy} onclick={close}>Close</Button>
    <Button variant="outline" size="sm" loading={loadingSQL} disabled={busy || loading || !!loadError} onclick={() => void showSetupSQL()}>Show setup SQL</Button>
    <Button
      variant="outline"
      size="sm"
      loading={verifying}
      disabled={busy || loading || !!loadError || !perPerson}
      title={perPerson ? 'Check the provisioning account and settings against ClickHouse' : 'Verify applies to per-person mode'}
      onclick={() => void verify()}
    >
      Verify
    </Button>
    <Button size="sm" type="submit" form="sso-identity-form" loading={saving} disabled={busy || loading || !!loadError}>Save</Button>
  {/snippet}
</Sheet>
