<script lang="ts">
  import { onMount } from 'svelte'
  import { getSection, setSection } from '../lib/stores/nav.svelte'
  import {
    getLicense,
    isLicenseLoaded,
    isProActive,
    isLicenseInGrace,
    loadLicense,
    activateLicense,
    deactivateLicense,
  } from '../lib/stores/license.svelte'
  import { success, error as toastError } from '../lib/stores/toast.svelte'
  import { getSession } from '../lib/stores/session.svelte'
  import { formatDate } from '../lib/utils/format'
  import {
    startTrial,
    createCheckout,
    resendLicense,
    requestBillingPortal,
    LicenseServerError,
  } from '../lib/license-server'
  import { Shield, ShieldCheck, ShieldAlert, FileText, ExternalLink, KeyRound } from 'lucide-svelte'
  import Badge from '../lib/components/common/Badge.svelte'
  import Button from '../lib/components/common/Button.svelte'
  import FormField from '../lib/components/common/FormField.svelte'
  import Input from '../lib/components/common/Input.svelte'
  import PageBody from '../lib/components/common/PageBody.svelte'
  import PageHeader from '../lib/components/common/PageHeader.svelte'
  import { docsURL } from '../lib/routes'
  import Panel from '../lib/components/common/Panel.svelte'
  import SectionHeader from '../lib/components/common/SectionHeader.svelte'
  import Sheet from '../lib/components/common/Sheet.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import Stat from '../lib/components/common/Stat.svelte'
  import Textarea from '../lib/components/common/Textarea.svelte'

  // License state lives in the shared store so the router, sidebar and
  // palette see activation and deactivation without a page refresh.
  const license = $derived(getLicense())
  const loading = $derived(!isLicenseLoaded())
  let activating = $state(false)
  let deactivating = $state(false)
  let showConfirmDeactivate = $state(false)
  let licenseInput = $state('')
  let activateOpen = $state(false)
  let fileInput = $state<HTMLInputElement | null>(null)

  // Self-serve licensing (license.ch-ui.com)
  let trialOpen = $state(false)
  let trialEmail = $state('')
  let trialName = $state('')
  let trialLoading = $state(false)
  let trialHint = $state('')
  // Email the trial license was sent to; shown where the form used to be.
  let trialSentTo = $state('')
  let checkoutLoading = $state(false)
  let manageOpen = $state(false)
  let manageEmail = $state('')
  let manageStatus = $state('')
  let resendLoading = $state(false)
  let portalLoading = $state(false)
  type SettingsTab = 'license' | 'instance' | 'legal'
  const settingsTabItems: Array<{ id: SettingsTab; label: string }> = [
    { id: 'license', label: 'License' },
    { id: 'instance', label: 'Instance' },
    { id: 'legal', label: 'Legal' },
  ]
  let activeTab = $state<SettingsTab>('license')

  const session = $derived(getSession())
  // The server only lets admins activate, replace or remove the license.
  const canManageLicense = $derived(session?.role === 'admin')

  const legalScopes = [
    {
      title: 'CH-UI Core',
      badge: 'Apache-2.0',
      description:
        'Community features and open-source code are licensed under Apache License 2.0.',
    },
    {
      title: 'CH-UI Pro features',
      badge: 'BSL 1.1',
      description:
        'Pro features are source-available under the Business Source License 1.1, not Apache-2.0. Production use requires a valid Pro license.',
    },
    {
      title: 'License Boundary',
      badge: 'Scope-separated',
      description:
        'Apache-2.0 applies to Core. The Business Source License 1.1 applies to Pro features.',
    },
  ]

  const proActive = $derived(isProActive())
  const inGrace = $derived(isLicenseInGrace())
  // Customer and license id reach admins only; expires_at is set for any
  // license that verified, so it marks an expired one for every role.
  const expiredLicense = $derived(!!(license && !license.valid && !!license.expires_at))

  const licenseState = $derived.by(() => {
    if (loading) return 'loading'
    if (proActive) return 'pro'
    if (inGrace) return 'grace'
    if (expiredLicense) return 'expired'
    return 'community'
  })

  function normalizeSettingsTab(value: string | null | undefined): SettingsTab {
    const raw = (value ?? '').trim().toLowerCase()
    if (raw === 'access') return 'license'
    if (raw === 'overview') return 'license'
    if (raw === 'licensing') return 'license'
    if (raw === 'brand') return 'instance'
    if ((settingsTabItems.map((item) => item.id) as string[]).includes(raw)) return raw as SettingsTab
    return 'license'
  }

  function syncSettingsTabParam(tab: SettingsTab) {
    if (typeof window === 'undefined') return
    if (!window.location.pathname.endsWith('/license')) return
    setSection(tab)
  }

  function switchTab(tab: SettingsTab, syncUrl = true) {
    activeTab = tab
    if (syncUrl) syncSettingsTabParam(tab)
  }

  onMount(() => {
    const initialTab = normalizeSettingsTab(
      typeof window === 'undefined' ? null : new URLSearchParams(window.location.search).get('section'),
    )
    switchTab(initialTab, true)
    // Always re-read on open: the status can change server-side (expiry,
    // CHUI_LICENSE env) while the shared store holds an older answer.
    void loadLicense(true)
  })

  async function activate() {
    const text = licenseInput.trim()
    if (!text) return

    activating = true
    try {
      await activateLicense(text)
      licenseInput = ''
      activateOpen = false
      success('License activated successfully')
    } catch (e) {
      toastError(e instanceof Error && e.message ? e.message : 'Failed to activate license')
    } finally {
      activating = false
    }
  }

  function licenseServerMessage(e: unknown, fallback: string): string {
    if (e instanceof Error && e.message) return e.message
    return fallback
  }

  const trialEmailValid = $derived(/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(trialEmail.trim()))

  async function startFreeTrial() {
    if (!trialEmailValid || trialLoading) return
    trialLoading = true
    trialHint = ''
    const email = trialEmail.trim()
    try {
      const res = await startTrial(email, trialName.trim() || undefined)
      trialOpen = false
      if (res.license) {
        try {
          // Older servers return the license inline: activate immediately.
          await activateLicense(JSON.stringify(res.license))
          trialEmail = ''
          trialName = ''
          success('Trial activated — 30 days of Pro')
        } catch {
          // Trial was created but local activation failed — the license was emailed.
          trialSentTo = email
          success('Trial created. The license was sent to your email; activate it under Activate Pro.')
        }
      } else {
        // Email-only delivery: prove inbox ownership, then paste to activate.
        trialEmail = ''
        trialName = ''
        trialSentTo = email
        success('Trial created. The license was sent to your email; activate it under Activate Pro.')
      }
    } catch (e) {
      if (e instanceof LicenseServerError && e.status === 409) {
        toastError(e.message)
        trialHint = 'Already used a trial with this email? Use "Resend license email" under Manage license & billing.'
        // Hand the same address to the manage sheet so it can be re-sent.
        if (!manageEmail.trim()) manageEmail = email
        trialOpen = false
        openManage()
      } else {
        toastError(licenseServerMessage(e, 'Failed to start trial'))
      }
    } finally {
      trialLoading = false
    }
  }

  async function buyProLicense() {
    if (checkoutLoading) return
    checkoutLoading = true
    // Open the tab synchronously so popup blockers don't eat the redirect.
    const popup = window.open('about:blank', '_blank')
    try {
      const { url } = await createCheckout()
      if (popup) popup.location.href = url
      else window.open(url, '_blank', 'noopener')
    } catch (e) {
      popup?.close()
      toastError(licenseServerMessage(e, 'Could not start checkout — use the pricing page instead'))
    } finally {
      checkoutLoading = false
    }
  }

  function openManage() {
    manageStatus = ''
    manageOpen = true
  }

  async function submitResend() {
    const email = manageEmail.trim()
    if (!email || resendLoading || portalLoading) return
    resendLoading = true
    manageStatus = ''
    try {
      const res = await resendLicense(email)
      manageStatus = res.message || 'If a license exists for this email, it has been re-sent.'
    } catch (e) {
      toastError(licenseServerMessage(e, 'Failed to resend license email'))
    } finally {
      resendLoading = false
    }
  }

  async function submitPortal() {
    const email = manageEmail.trim()
    if (!email || portalLoading || resendLoading) return
    portalLoading = true
    manageStatus = ''
    try {
      const res = await requestBillingPortal(email)
      manageStatus = res.message || 'Billing portal link sent — check your email.'
    } catch (e) {
      toastError(licenseServerMessage(e, 'Failed to request billing portal link'))
    } finally {
      portalLoading = false
    }
  }

  async function deactivate() {
    deactivating = true
    try {
      await deactivateLicense()
      showConfirmDeactivate = false
      success('License deactivated')
    } catch (e) {
      toastError(e instanceof Error && e.message ? e.message : 'Failed to deactivate license')
    } finally {
      deactivating = false
    }
  }

  function handleFileUpload(e: Event) {
    const target = e.target as HTMLInputElement
    const file = target.files?.[0]
    if (!file) return
    const reader = new FileReader()
    reader.onload = () => {
      licenseInput = reader.result as string
      activateOpen = true
    }
    reader.readAsText(file)
    target.value = ''
  }

  function openFilePicker() {
    fileInput?.click()
  }

  // The context panel changes ?section=; follow it.
  $effect(() => {
    const next = getSection()
    if (next === null) return
    const tab = normalizeSettingsTab(next)
    if (tab !== activeTab) switchTab(tab, false)
  })
</script>

<div class="flex h-full flex-col">
  <PageHeader title="Settings" subtitle={settingsTabItems.find((t) => t.id === activeTab)?.label} docs={docsURL('settings', activeTab)}>
    {#snippet meta()}
      {#if licenseState === 'loading'}
        <Badge tone="neutral">Checking license…</Badge>
      {:else if licenseState === 'pro'}
        <Badge tone="success">Pro Active</Badge>
      {:else if licenseState === 'grace'}
        <Badge tone="warning">Pro Grace Period</Badge>
      {:else if licenseState === 'expired'}
        <Badge tone="danger">Pro Expired</Badge>
      {:else}
        <Badge tone="neutral">Community</Badge>
      {/if}
    {/snippet}
  </PageHeader>

  <PageBody width="md">
    {#if activeTab === 'license'}
      <div class="space-y-8">
        <div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
          <Stat size="md" label="Edition" value={license?.edition || 'community'} />
          <Stat size="md" label="Customer" value={license?.customer || (license?.expires_at ? '—' : 'Open Source Deployment')} />
          <Stat size="md" label="License ID" value={license?.license_id || '—'} />
          <Stat size="md" label="Expiration" value={formatDate(license?.expires_at)} />
        </div>

        <div>
          <SectionHeader title="License status" description="Core capabilities are enabled under Apache-2.0. A Pro license unlocks the Pro features (Business Source License 1.1, source-available)." />
          {#if loading}
            <div class="flex items-center justify-center py-6"><Spinner /></div>
          {:else if proActive}
            <Panel padding="none">
              <div class="flex flex-wrap items-center gap-3 px-4 py-3">
                <ShieldCheck size={16} class="text-success" />
                <Badge tone="success">Pro License Active</Badge>
                <span class="text-xs text-fg-3">ID: {license?.license_id || '—'}</span>
              </div>
              <div class="divide-y divide-edge-subtle border-t border-edge-subtle px-4">
                <FormField layout="row" label="Customer">
                  <p class="text-[13px] text-fg">{license?.customer || '—'}</p>
                </FormField>
                <FormField layout="row" label="Expires">
                  <p class="text-[13px] text-fg">{formatDate(license?.expires_at)}</p>
                </FormField>
              </div>
              <div class="flex flex-wrap items-center justify-between gap-3 border-t border-edge-subtle px-4 py-3">
                {#if !canManageLicense}
                  <span class="text-xs text-fg-3">Only admins can change or remove the license.</span>
                {:else if showConfirmDeactivate}
                  <span class="text-[13px] text-danger">Deactivate this Pro license and downgrade to Community Edition?</span>
                  <div class="flex items-center gap-2">
                    <Button size="sm" variant="outline" onclick={() => showConfirmDeactivate = false}>Cancel</Button>
                    <Button size="sm" variant="danger" loading={deactivating} onclick={deactivate}>
                      {deactivating ? 'Deactivating…' : 'Confirm deactivate'}
                    </Button>
                  </div>
                {:else}
                  <span class="text-xs text-fg-3">Deactivating removes Pro features from this instance until a license is activated again.</span>
                  <Button size="sm" variant="outline" class="text-danger hover:text-danger" onclick={() => showConfirmDeactivate = true}>Deactivate license</Button>
                {/if}
              </div>
            </Panel>
          {:else if inGrace}
            <Panel>
              <div class="flex flex-wrap items-center gap-3">
                <ShieldAlert size={16} class="text-warning" />
                <Badge tone="warning">Grace Period</Badge>
                <span class="text-xs text-fg-3">Customer: {license?.customer || '—'}</span>
              </div>
              <p class="mt-3 text-[13px] text-warning">Expired on {formatDate(license?.expires_at)}. Grace period ends on {formatDate(license?.grace_until)}.</p>
              <p class="mt-1 text-[13px] text-fg-3">Activate a renewed Pro license before the grace period ends.</p>
            </Panel>
          {:else if expiredLicense}
            <Panel>
              <div class="flex flex-wrap items-center gap-3">
                <ShieldAlert size={16} class="text-danger" />
                <Badge tone="danger">License Expired</Badge>
                <span class="text-xs text-fg-3">Customer: {license?.customer || '—'}</span>
              </div>
              <p class="mt-3 text-[13px] text-danger">Expired on {formatDate(license?.expires_at)}</p>
              <p class="mt-1 text-[13px] text-fg-3">Activate a new Pro license to restore Pro features.</p>
            </Panel>
          {:else}
            <Panel padding="none">
              <div class="flex flex-wrap items-center gap-3 px-4 py-3">
                <Shield size={16} class="text-fg-4" />
                <Badge tone="neutral">Community Edition</Badge>
                <span class="text-xs text-fg-3">Try every Pro feature for 30 days, no card required. One trial per email; the license is also sent to your inbox.</span>
              </div>
              <div class="flex flex-wrap items-center justify-between gap-3 border-t border-edge-subtle px-4 py-3">
                {#if trialSentTo}
                  <span class="text-xs text-success">Trial license sent to {trialSentTo}. Activate it under Activate Pro once it arrives.</span>
                {:else}
                  <span class="text-xs {trialHint ? 'text-warning' : 'text-fg-3'}">{trialHint || (canManageLicense ? 'No trial requested on this instance.' : 'Ask an admin to start the trial on this instance.')}</span>
                {/if}
                <Button size="sm" disabled={!canManageLicense} onclick={() => trialOpen = true}>Start free trial</Button>
              </div>
            </Panel>
          {/if}
        </div>

        {#if !loading && canManageLicense}
          <div>
            <SectionHeader
              title={proActive ? 'Replace license' : 'Activate Pro'}
              description={proActive
                ? 'Activate a paid, renewed or upgraded license. It replaces the current one, no need to deactivate first.'
                : 'Paste the signed license JSON or upload the license file you received by email.'}
            >
              {#snippet actions()}
                <a
                  href="https://ch-ui.com/pricing?utm_source=app&utm_medium=license_page"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="text-xs text-fg-3 underline underline-offset-2 hover:text-fg"
                >Pricing</a>
                <a
                  href="https://ch-ui.com/license"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="text-xs text-fg-3 underline underline-offset-2 hover:text-fg"
                >Lost your license? Enterprise?</a>
                <Button size="sm" loading={checkoutLoading} onclick={buyProLicense}>
                  {checkoutLoading ? 'Opening checkout…' : 'Buy Pro License'} <ExternalLink size={13} />
                </Button>
              {/snippet}
            </SectionHeader>
            <Panel>
              <div class="flex flex-wrap items-center justify-between gap-3">
                <span class="text-xs text-fg-3">
                  {proActive
                    ? 'The new license takes effect as soon as it is activated.'
                    : 'The license is the signed JSON from the email, pasted or uploaded as a file.'}
                </span>
                <Button size="sm" variant="outline" onclick={() => activateOpen = true}>
                  {proActive ? 'Replace license' : 'Activate license'}
                </Button>
              </div>
            </Panel>
          </div>
        {/if}

        <div>
          <SectionHeader title="Manage license & billing" description="Re-send a license file or get a Stripe billing-portal link by email.">
            {#snippet actions()}
              <Button size="sm" variant="outline" onclick={openManage}>Manage</Button>
            {/snippet}
          </SectionHeader>
          <Panel>
            <span class="text-xs text-fg-3">Both go to the email used at purchase or trial. Nothing on this instance changes.</span>
          </Panel>
        </div>

        <input
          bind:this={fileInput}
          type="file"
          accept=".json,application/json"
          onchange={handleFileUpload}
          class="hidden"
        />
      </div>

    {:else if activeTab === 'instance'}
      <div class="space-y-8">
        <div>
          <SectionHeader title="Instance" description="Who this session is and what it is connected to." />
          <Panel padding="none">
            <div class="divide-y divide-edge-subtle px-4">
              <FormField layout="row" label="Connected as">
                <p class="text-[13px] text-fg">{session?.user || '—'}</p>
              </FormField>
              <FormField layout="row" label="Role">
                <p class="text-[13px] text-fg">{session?.role || '—'}</p>
              </FormField>
              <FormField layout="row" label="Connection">
                <p class="text-[13px] text-fg">{session?.connectionName || '—'}</p>
              </FormField>
              <FormField layout="row" label="Version">
                <p class="py-1.5 font-mono text-[13px] text-fg">{session?.version || '—'}</p>
              </FormField>
            </div>
          </Panel>
        </div>
      </div>

    {:else if activeTab === 'legal'}
      <div class="space-y-8">
        <div>
          <SectionHeader title="Legal scope" description="Apache-2.0 applies to Core. The Business Source License 1.1 (source-available) applies to Pro features.">
            {#snippet actions()}
              <a
                href="https://www.apache.org/licenses/LICENSE-2.0"
                target="_blank"
                rel="noreferrer"
                class="inline-flex h-7 items-center gap-1.5 rounded-md border border-edge px-2.5 text-xs font-medium text-fg-2 transition-colors hover:border-edge-strong hover:bg-hover hover:text-fg"
              ><FileText size={12} /> Apache-2.0 text <ExternalLink size={12} /></a>
              <a
                href="https://ch-ui.com/docs/license/"
                target="_blank"
                rel="noreferrer"
                class="inline-flex h-7 items-center gap-1.5 rounded-md border border-edge px-2.5 text-xs font-medium text-fg-2 transition-colors hover:border-edge-strong hover:bg-hover hover:text-fg"
              ><KeyRound size={12} /> License policy <ExternalLink size={12} /></a>
              <a
                href="https://ch-ui.com/terms"
                target="_blank"
                rel="noreferrer"
                class="inline-flex h-7 items-center gap-1.5 rounded-md border border-edge px-2.5 text-xs font-medium text-fg-2 transition-colors hover:border-edge-strong hover:bg-hover hover:text-fg"
              >Terms <ExternalLink size={12} /></a>
              <a
                href="https://ch-ui.com/privacy"
                target="_blank"
                rel="noreferrer"
                class="inline-flex h-7 items-center gap-1.5 rounded-md border border-edge px-2.5 text-xs font-medium text-fg-2 transition-colors hover:border-edge-strong hover:bg-hover hover:text-fg"
              >Privacy <ExternalLink size={12} /></a>
            {/snippet}
          </SectionHeader>
          <Panel padding="none">
            <div class="divide-y divide-edge-subtle px-4">
              {#each legalScopes as item}
                <div class="flex items-start justify-between gap-4 py-3">
                  <div class="min-w-0">
                    <p class="text-[13px] font-medium text-fg">{item.title}</p>
                    <p class="mt-0.5 text-xs leading-relaxed text-fg-3">{item.description}</p>
                  </div>
                  <Badge tone={item.badge === 'Apache-2.0' ? 'success' : item.badge === 'Proprietary' ? 'danger' : 'brand'}>{item.badge}</Badge>
                </div>
              {/each}
            </div>
          </Panel>
        </div>
      </div>
    {/if}
  </PageBody>
</div>

<Sheet open={trialOpen} title="Start a Pro trial" description="30 days of every Pro feature, no card required. One trial per email." size="sm" onclose={() => trialOpen = false}>
  {#snippet footer()}
    <Button variant="outline" size="sm" onclick={() => trialOpen = false}>Cancel</Button>
    <Button size="sm" loading={trialLoading} disabled={!trialEmailValid || !canManageLicense} onclick={startFreeTrial}>
      {trialLoading ? 'Requesting…' : 'Request trial'}
    </Button>
  {/snippet}
  <div class="flex flex-col gap-4">
    <FormField label="Email" for="trial-email" required hint="Where the trial license is sent." controlWidth="full">
      <Input id="trial-email" type="email" bind:value={trialEmail} placeholder="you@company.com" autocomplete="email" />
    </FormField>
    <FormField label="Name" for="trial-name" hint="Optional." controlWidth="full">
      <Input id="trial-name" type="text" bind:value={trialName} placeholder="Your name" />
    </FormField>
  </div>
</Sheet>

<Sheet
  open={activateOpen}
  title="Activate a license"
  description={proActive ? 'Replaces the current license. No need to deactivate first.' : 'Paste the signed license JSON you received by email, or upload the file.'}
  size="md"
  onclose={() => activateOpen = false}
>
  {#snippet footer()}
    <Button variant="outline" size="sm" onclick={() => activateOpen = false}>Cancel</Button>
    <Button size="sm" loading={activating} disabled={!licenseInput.trim()} onclick={activate}>
      {activating ? 'Activating…' : 'Activate'}
    </Button>
  {/snippet}
  <div class="flex flex-col gap-4">
    <FormField label="License JSON" for="license-json" controlWidth="full">
      <Textarea id="license-json" bind:value={licenseInput} placeholder="Paste signed license JSON here…" rows={12} mono class="resize-y" />
    </FormField>
    <div class="flex flex-wrap items-center justify-between gap-3">
      <span class="text-xs text-fg-3">Or load the .json file from the email. Its contents fill the field above.</span>
      <Button size="sm" variant="outline" onclick={openFilePicker}>Upload a file</Button>
    </div>
  </div>
</Sheet>

<Sheet open={manageOpen} title="License and billing" description="Re-send a license file or get a Stripe billing-portal link by email." size="sm" onclose={() => manageOpen = false}>
  {#snippet footer()}
    <Button variant="outline" size="sm" onclick={() => manageOpen = false}>Cancel</Button>
  {/snippet}
  <div class="flex flex-col gap-4">
    <FormField label="Email" for="manage-email" required hint="The email used at purchase or trial." controlWidth="full">
      <Input id="manage-email" type="email" bind:value={manageEmail} placeholder="you@company.com" autocomplete="email" />
    </FormField>
    <div class="divide-y divide-edge-subtle rounded-lg border border-edge-subtle px-3">
      <div class="flex flex-wrap items-center justify-between gap-3 py-3">
        <div class="min-w-0">
          <p class="text-[13px] font-medium text-fg">Resend license email</p>
          <p class="mt-0.5 text-xs text-fg-3">Your license file, re-sent to this address.</p>
        </div>
        <Button size="sm" variant="outline" loading={resendLoading} disabled={!manageEmail.trim() || portalLoading} onclick={submitResend}>
          {resendLoading ? 'Sending…' : 'Resend'}
        </Button>
      </div>
      <div class="flex flex-wrap items-center justify-between gap-3 py-3">
        <div class="min-w-0">
          <p class="text-[13px] font-medium text-fg">Manage billing</p>
          <p class="mt-0.5 text-xs text-fg-3">A secure Stripe billing-portal link, sent by email.</p>
        </div>
        <Button size="sm" variant="outline" loading={portalLoading} disabled={!manageEmail.trim() || resendLoading} onclick={submitPortal}>
          {portalLoading ? 'Sending…' : 'Email link'}
        </Button>
      </div>
    </div>
    {#if manageStatus}
      <p class="text-[13px] text-success" role="status">{manageStatus}</p>
    {/if}
  </div>
</Sheet>
