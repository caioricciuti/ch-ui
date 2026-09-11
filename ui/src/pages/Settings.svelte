<script lang="ts">
  import { onMount } from 'svelte'
  import { getSection, setSection } from '../lib/stores/nav.svelte'
  import { apiGet, apiPost } from '../lib/api/client'
  import type { LicenseInfo } from '../lib/types/api'
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
  import { Shield, ShieldCheck, ShieldAlert, Upload, X, FileText, ExternalLink, KeyRound, ChevronRight } from 'lucide-svelte'
  import Badge from '../lib/components/common/Badge.svelte'
  import Button from '../lib/components/common/Button.svelte'
  import FormField from '../lib/components/common/FormField.svelte'
  import Input from '../lib/components/common/Input.svelte'
  import PageBody from '../lib/components/common/PageBody.svelte'
  import PageHeader from '../lib/components/common/PageHeader.svelte'
  import Panel from '../lib/components/common/Panel.svelte'
  import SectionHeader from '../lib/components/common/SectionHeader.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import Stat from '../lib/components/common/Stat.svelte'
  import Textarea from '../lib/components/common/Textarea.svelte'

  let license = $state<LicenseInfo | null>(null)
  let loading = $state(true)
  let activating = $state(false)
  let deactivating = $state(false)
  let showConfirmDeactivate = $state(false)
  let licenseInput = $state('')
  let inputMode = $state<'paste' | 'idle'>('idle')
  let fileInput = $state<HTMLInputElement | null>(null)

  // Self-serve licensing (license.ch-ui.com)
  let trialEmail = $state('')
  let trialName = $state('')
  let trialLoading = $state(false)
  let trialHint = $state('')
  let checkoutLoading = $state(false)
  let manageOpen = $state(false)
  let resendEmail = $state('')
  let resendLoading = $state(false)
  let portalEmail = $state('')
  let portalLoading = $state(false)
  type SettingsTab = 'license' | 'instance' | 'legal'
  const settingsTabItems: Array<{ id: SettingsTab; label: string }> = [
    { id: 'license', label: 'License' },
    { id: 'instance', label: 'Instance' },
    { id: 'legal', label: 'Legal' },
  ]
  let activeTab = $state<SettingsTab>('license')

  const session = $derived(getSession())

  const legalScopes = [
    {
      title: 'CH-UI Core',
      badge: 'Apache-2.0',
      description:
        'Community features and open-source code are licensed under Apache License 2.0.',
    },
    {
      title: 'CH-UI Pro Modules',
      badge: 'Proprietary',
      description:
        'Commercial Pro capabilities are not Apache-2.0 and require a signed commercial agreement + valid license.',
    },
    {
      title: 'License Boundary',
      badge: 'Scope-separated',
      description:
        'Apache terms apply to Core. Commercial terms apply to Pro-only modules and entitlements.',
    },
  ]

  const proActive = $derived(!!(license?.valid && license?.edition?.toLowerCase() === 'pro'))
  const expiredLicense = $derived(!!(license && !license.valid && !!license.license_id))

  const licenseState = $derived.by(() => {
    if (loading) return 'loading'
    if (proActive) return 'pro'
    if (expiredLicense) return 'expired'
    return 'community'
  })

  async function loadLicense() {
    try {
      const res = await apiGet<LicenseInfo>('/api/license')
      license = res
    } catch {
      license = null
    } finally {
      loading = false
    }
  }

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
    void loadLicense()
  })

  async function activateLicenseText(text: string): Promise<void> {
    const res = await apiPost<LicenseInfo>('/api/license/activate', { license: text })
    license = res
  }

  async function activate() {
    const text = licenseInput.trim()
    if (!text) return

    activating = true
    try {
      await activateLicenseText(text)
      licenseInput = ''
      inputMode = 'idle'
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
    try {
      const res = await startTrial(trialEmail.trim(), trialName.trim() || undefined)
      if (res.license) {
        try {
          // Older servers return the license inline: activate immediately.
          await activateLicenseText(JSON.stringify(res.license))
          trialEmail = ''
          trialName = ''
          success('Trial activated — 30 days of Pro')
        } catch {
          // Trial was created but local activation failed — the license was emailed.
          success('Trial created — the license was sent to your email. Paste it below to activate.')
        }
      } else {
        // Email-only delivery: prove inbox ownership, then paste to activate.
        trialEmail = ''
        trialName = ''
        success('Trial created — the license was sent to your email. Paste it below to activate.')
      }
    } catch (e) {
      if (e instanceof LicenseServerError && e.status === 409) {
        toastError(e.message)
        trialHint = 'Already used a trial with this email? Use "Resend license email" under Manage below.'
        manageOpen = true
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

  async function submitResend() {
    const email = resendEmail.trim()
    if (!email || resendLoading) return
    resendLoading = true
    try {
      const res = await resendLicense(email)
      success(res.message || 'If a license exists for this email, it has been re-sent.')
    } catch (e) {
      toastError(licenseServerMessage(e, 'Failed to resend license email'))
    } finally {
      resendLoading = false
    }
  }

  async function submitPortal() {
    const email = portalEmail.trim()
    if (!email || portalLoading) return
    portalLoading = true
    try {
      const res = await requestBillingPortal(email)
      success(res.message || 'Billing portal link sent — check your email.')
    } catch (e) {
      toastError(licenseServerMessage(e, 'Failed to request billing portal link'))
    } finally {
      portalLoading = false
    }
  }

  async function deactivate() {
    deactivating = true
    try {
      const res = await apiPost<LicenseInfo>('/api/license/deactivate')
      license = res
      showConfirmDeactivate = false
      success('License deactivated')
    } catch (e: any) {
      toastError(e.message || 'Failed to deactivate license')
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
      inputMode = 'paste'
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
  <PageHeader title="License" subtitle={settingsTabItems.find((t) => t.id === activeTab)?.label}>
    {#snippet meta()}
      {#if licenseState === 'loading'}
        <Badge tone="neutral">Checking license…</Badge>
      {:else if licenseState === 'pro'}
        <Badge tone="success">Pro Active</Badge>
      {:else if licenseState === 'expired'}
        <Badge tone="danger">Pro Expired</Badge>
      {:else}
        <Badge tone="neutral">Community</Badge>
      {/if}
      <Badge tone="brand" class="uppercase">{license?.edition || 'community'}</Badge>
    {/snippet}
  </PageHeader>

  <PageBody width="md">
    {#if activeTab === 'license'}
      <div class="space-y-8">
        <div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
          <Stat size="md" label="Edition" value={license?.edition || 'community'} />
          <Stat size="md" label="Customer" value={license?.customer || 'Open Source Deployment'} />
          <Stat size="md" label="License ID" value={license?.license_id || '—'} />
          <Stat size="md" label="Expiration" value={formatDate(license?.expires_at)} />
        </div>

        <div>
          <SectionHeader title="License status" description="Core capabilities are enabled under Apache-2.0. A Pro license unlocks the proprietary modules." />
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
                {#if showConfirmDeactivate}
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
          {:else if expiredLicense}
            <Panel>
              <div class="flex flex-wrap items-center gap-3">
                <ShieldAlert size={16} class="text-danger" />
                <Badge tone="danger">License Expired</Badge>
                <span class="text-xs text-fg-3">Customer: {license?.customer || '—'}</span>
              </div>
              <p class="mt-3 text-[13px] text-danger">Expired on {formatDate(license?.expires_at)}</p>
              <p class="mt-1 text-[13px] text-fg-3">Activate a new Pro license to restore proprietary features.</p>
            </Panel>
          {:else}
            <Panel padding="none">
              <div class="flex flex-wrap items-center gap-3 px-4 py-3">
                <Shield size={16} class="text-fg-4" />
                <Badge tone="neutral">Community Edition</Badge>
                <span class="text-xs text-fg-3">Try every Pro feature for 30 days, no card required. One trial per email; the license is also sent to your inbox.</span>
              </div>
              <div class="divide-y divide-edge-subtle border-t border-edge-subtle px-4">
                <FormField layout="row" label="Email" for="trial-email" required hint="Where the trial license is sent.">
                  <Input id="trial-email" type="email" bind:value={trialEmail} placeholder="you@company.com" autocomplete="email" />
                </FormField>
                <FormField layout="row" label="Name" for="trial-name" hint="Optional.">
                  <Input id="trial-name" type="text" bind:value={trialName} placeholder="Your name" />
                </FormField>
              </div>
              <div class="flex flex-wrap items-center justify-between gap-3 border-t border-edge-subtle px-4 py-3">
                <span class="text-xs {trialHint ? 'text-warning' : 'text-fg-3'}">{trialHint || 'Free 30-day trial of Pro.'}</span>
                <Button size="sm" loading={trialLoading} disabled={!trialEmailValid} onclick={startFreeTrial}>
                  {trialLoading ? 'Starting trial…' : 'Start free trial'}
                </Button>
              </div>
            </Panel>
          {/if}
        </div>

        {#if !loading && !proActive}
          <div>
            <SectionHeader title="Activate Pro" description="Paste the signed license JSON or upload the license file you received by email.">
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
              {#if inputMode === 'idle'}
                <div class="flex flex-wrap items-center gap-2">
                  <Button size="sm" variant="outline" onclick={() => inputMode = 'paste'}>Paste License JSON</Button>
                  <Button size="sm" variant="outline" onclick={openFilePicker}><Upload size={14} /> Upload License File</Button>
                </div>
              {:else}
                <div class="space-y-3">
                  <div class="relative">
                    <Textarea bind:value={licenseInput} placeholder="Paste signed license JSON here…" rows={8} mono class="resize-y pr-8" />
                    <Button icon size="xs" variant="ghost" class="absolute top-1.5 right-1.5" aria-label="Cancel" onclick={() => { licenseInput = ''; inputMode = 'idle' }}>
                      <X size={13} />
                    </Button>
                  </div>
                  <div class="flex flex-wrap items-center gap-2">
                    <Button size="sm" loading={activating} disabled={!licenseInput.trim()} onclick={activate}>
                      {activating ? 'Activating…' : 'Activate License'}
                    </Button>
                    <Button size="sm" variant="outline" onclick={openFilePicker}><Upload size={14} /> Replace from File</Button>
                  </div>
                </div>
              {/if}
            </Panel>
          </div>
        {/if}

        <div>
          <SectionHeader title="Manage license & billing" description="Re-send a license file or get a Stripe billing-portal link by email.">
            {#snippet actions()}
              <Button size="sm" variant="ghost" aria-pressed={manageOpen} onclick={() => manageOpen = !manageOpen}>
                <ChevronRight size={12} class="transition-transform {manageOpen ? 'rotate-90' : ''}" />
                {manageOpen ? 'Hide' : 'Show'}
              </Button>
            {/snippet}
          </SectionHeader>
          {#if manageOpen}
            <Panel padding="none">
              <div class="divide-y divide-edge-subtle px-4">
                <FormField layout="row" label="Resend license email" for="resend-email" hint="Get your license file re-sent to the email used at purchase or trial.">
                  <div class="flex items-center gap-2">
                    <Input id="resend-email" type="email" bind:value={resendEmail} placeholder="you@company.com" autocomplete="email" />
                    <Button size="md" variant="outline" loading={resendLoading} disabled={!resendEmail.trim()} onclick={submitResend}>
                      {resendLoading ? 'Sending…' : 'Resend'}
                    </Button>
                  </div>
                </FormField>
                <FormField layout="row" label="Manage billing" for="portal-email" hint="Receive a secure Stripe billing-portal link by email.">
                  <div class="flex items-center gap-2">
                    <Input id="portal-email" type="email" bind:value={portalEmail} placeholder="you@company.com" autocomplete="email" />
                    <Button size="md" variant="outline" loading={portalLoading} disabled={!portalEmail.trim()} onclick={submitPortal}>
                      {portalLoading ? 'Sending…' : 'Email link'}
                    </Button>
                  </div>
                </FormField>
              </div>
            </Panel>
          {/if}
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
          <SectionHeader title="Legal scope" description="Apache terms apply to Core. Commercial terms apply to Pro-only modules and entitlements.">
            {#snippet actions()}
              <a
                href="https://www.apache.org/licenses/LICENSE-2.0"
                target="_blank"
                rel="noreferrer"
                class="inline-flex h-7 items-center gap-1.5 rounded-md border border-edge px-2.5 text-xs font-medium text-fg-2 transition-colors hover:border-edge-strong hover:bg-hover hover:text-fg"
              ><FileText size={12} /> Apache-2.0 text <ExternalLink size={12} /></a>
              <a
                href="https://github.com/caioricciuti/ch-ui/blob/main/docs/license.md"
                target="_blank"
                rel="noreferrer"
                class="inline-flex h-7 items-center gap-1.5 rounded-md border border-edge px-2.5 text-xs font-medium text-fg-2 transition-colors hover:border-edge-strong hover:bg-hover hover:text-fg"
              ><KeyRound size={12} /> License policy <ExternalLink size={12} /></a>
              <a
                href="https://github.com/caioricciuti/ch-ui/blob/main/docs/legal/terms-of-service.md"
                target="_blank"
                rel="noreferrer"
                class="inline-flex h-7 items-center gap-1.5 rounded-md border border-edge px-2.5 text-xs font-medium text-fg-2 transition-colors hover:border-edge-strong hover:bg-hover hover:text-fg"
              >Terms <ExternalLink size={12} /></a>
              <a
                href="https://github.com/caioricciuti/ch-ui/blob/main/docs/legal/privacy-policy.md"
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
