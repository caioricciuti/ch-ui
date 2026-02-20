<script lang="ts">
  import { onMount } from 'svelte'
  import { apiGet, apiPost } from '../lib/api/client'
  import type { LicenseInfo } from '../lib/types/api'
  import { success, error as toastError } from '../lib/stores/toast.svelte'
  import { getSession } from '../lib/stores/session.svelte'
  import {
    Settings as SettingsIcon,
    Shield,
    ShieldCheck,
    ShieldAlert,
    Upload,
    X,
    Sparkles,
    FileText,
    ExternalLink,
    Building2,
    Scale,
    KeyRound,
  } from 'lucide-svelte'
  import logo from '../assets/logo.png'

  let license = $state<LicenseInfo | null>(null)
  let loading = $state(true)
  let activating = $state(false)
  let deactivating = $state(false)
  let showConfirmDeactivate = $state(false)
  let licenseInput = $state('')
  let inputMode = $state<'paste' | 'idle'>('idle')
  let fileInput = $state<HTMLInputElement | null>(null)
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
    const url = new URL(window.location.href)
    if (url.searchParams.get('tab') === tab) return
    url.searchParams.set('tab', tab)
    history.replaceState(null, '', `${url.pathname}?${url.searchParams.toString()}`)
  }

  function switchTab(tab: SettingsTab, syncUrl = true) {
    activeTab = tab
    if (syncUrl) syncSettingsTabParam(tab)
  }

  onMount(() => {
    const initialTab = normalizeSettingsTab(
      typeof window === 'undefined' ? null : new URLSearchParams(window.location.search).get('tab'),
    )
    switchTab(initialTab, true)
    void loadLicense()
  })

  async function activate() {
    const text = licenseInput.trim()
    if (!text) return

    activating = true
    try {
      const res = await apiPost<LicenseInfo>('/api/license/activate', { license: text })
      license = res
      licenseInput = ''
      inputMode = 'idle'
      success('License activated successfully')
    } catch (e: any) {
      toastError(e.message || 'Failed to activate license')
    } finally {
      activating = false
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

  function formatDate(date: string | undefined) {
    if (!date) return '—'
    return new Date(date).toLocaleDateString()
  }

  function openFilePicker() {
    fileInput?.click()
  }
</script>

<div class="h-full overflow-auto">
  <div class="max-w-7xl mx-auto p-6 space-y-4">
    <section class="ds-panel overflow-hidden">
      <div class="px-5 py-4 border-b border-orange-300/25 dark:border-orange-700/35 bg-gradient-to-r from-orange-100/60 via-transparent to-transparent dark:from-orange-500/10 dark:via-transparent">
        <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
          <div class="flex items-center gap-4 min-w-0">
            <div class="h-16 w-16 rounded-2xl border border-orange-300/40 dark:border-orange-700/40 bg-gray-100 dark:bg-gray-900 grid place-items-center overflow-hidden shrink-0">
              <img src={logo} alt="CH-UI logo" class="h-12 w-12 object-contain" />
            </div>
            <div class="min-w-0">
              <div class="flex items-center gap-2 text-gray-900 dark:text-gray-100">
                <SettingsIcon size={18} class="text-ch-orange" />
                <h1 class="text-2xl font-semibold leading-tight">CH-UI License</h1>
              </div>
              <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">Identity, licensing, entitlements, and legal scope controls</p>
            </div>
          </div>

          <div class="flex items-center gap-2 flex-wrap">
            {#if licenseState === 'loading'}
              <span class="ds-badge ds-badge-neutral">Checking license...</span>
            {:else if licenseState === 'pro'}
              <span class="ds-badge ds-badge-success">Pro Active</span>
            {:else if licenseState === 'expired'}
              <span class="ds-badge ds-badge-danger">Pro Expired</span>
            {:else}
              <span class="ds-badge ds-badge-neutral">Community Edition</span>
            {/if}
            <span class="ds-badge ds-badge-brand uppercase">{license?.edition || 'community'}</span>
          </div>
        </div>
      </div>

      <div class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-4 gap-3 p-5">
        <div class="ds-panel-muted px-3 py-2">
          <p class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Edition</p>
          <p class="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100">{license?.edition || 'community'}</p>
        </div>
        <div class="ds-panel-muted px-3 py-2">
          <p class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Customer</p>
          <p class="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100">{license?.customer || 'Open Source Deployment'}</p>
        </div>
        <div class="ds-panel-muted px-3 py-2">
          <p class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">License ID</p>
          <p class="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100 truncate">{license?.license_id || '—'}</p>
        </div>
        <div class="ds-panel-muted px-3 py-2">
          <p class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Expiration</p>
          <p class="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100">{formatDate(license?.expires_at)}</p>
        </div>
      </div>
    </section>

    <div class="ds-panel p-2">
      <nav class="ds-tabs border-0 px-1 pt-0 gap-1 overflow-x-auto whitespace-nowrap" aria-label="License Tabs">
        {#each settingsTabItems as item}
          <button
            type="button"
            class="ds-tab {activeTab === item.id ? 'ds-tab-active' : ''}"
            onclick={() => switchTab(item.id)}
          >
            {item.label}
          </button>
        {/each}
      </nav>
    </div>

    <div class="grid grid-cols-1 xl:grid-cols-[1.55fr_1fr] gap-4">
      {#if activeTab === 'license'}
      <section class="ds-panel p-5 space-y-4">
        <div class="flex items-center gap-2">
          <Sparkles size={16} class="text-ch-orange" />
          <h2 class="text-sm font-semibold text-gray-800 dark:text-gray-200 uppercase tracking-wide">License Control</h2>
        </div>

        {#if loading}
          <div class="flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400">
            <div class="w-4 h-4 border-2 border-gray-300 border-t-transparent rounded-full animate-spin"></div>
            Loading license status...
          </div>
        {:else if proActive}
          <div class="space-y-4">
            <div class="flex items-center gap-2">
              <ShieldCheck size={18} class="text-emerald-500" />
              <span class="ds-badge ds-badge-success">Pro License Active</span>
              <span class="text-xs text-gray-500 dark:text-gray-400">ID: {license?.license_id || '—'}</span>
            </div>

            <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <div class="ds-panel-muted p-3">
                <p class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Customer</p>
                <p class="text-sm font-medium text-gray-900 dark:text-gray-100 mt-1">{license?.customer || '—'}</p>
              </div>
              <div class="ds-panel-muted p-3">
                <p class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Expires</p>
                <p class="text-sm font-medium text-gray-900 dark:text-gray-100 mt-1">{formatDate(license?.expires_at)}</p>
              </div>
            </div>

            <div class="pt-2 border-t border-gray-200 dark:border-gray-800">
              {#if showConfirmDeactivate}
                <div class="ds-panel-muted p-3 border-red-400/35">
                  <p class="text-sm text-red-500">Deactivate this Pro license and downgrade to Community Edition?</p>
                  <div class="flex items-center gap-2 flex-wrap mt-2">
                    <button
                      onclick={deactivate}
                      disabled={deactivating}
                      class="ds-btn-primary px-3 py-1 border-red-500 bg-red-600 hover:bg-red-700 disabled:opacity-50"
                    >
                      {deactivating ? 'Deactivating...' : 'Confirm Deactivate'}
                    </button>
                    <button
                      onclick={() => showConfirmDeactivate = false}
                      class="ds-btn-outline px-3 py-1"
                    >
                      Cancel
                    </button>
                  </div>
                </div>
              {:else}
                <button
                  onclick={() => showConfirmDeactivate = true}
                  class="text-xs text-red-500 hover:text-red-400"
                >
                  Deactivate License
                </button>
              {/if}
            </div>
          </div>
        {:else if expiredLicense}
          <div class="space-y-3">
            <div class="flex items-center gap-2">
              <ShieldAlert size={18} class="text-red-500" />
              <span class="ds-badge ds-badge-danger">License Expired</span>
            </div>
            <div class="text-sm text-gray-500 dark:text-gray-400">Customer: {license?.customer || '—'}</div>
            <div class="text-sm text-red-500">Expired on {formatDate(license?.expires_at)}</div>
            <p class="text-sm text-gray-500 dark:text-gray-400">Activate a new Pro license to restore proprietary features.</p>
          </div>
        {:else}
          <div class="space-y-3">
            <div class="flex items-center gap-2">
              <Shield size={18} class="text-gray-400" />
              <span class="ds-badge ds-badge-neutral">Community Edition</span>
            </div>
            <p class="text-sm text-gray-500 dark:text-gray-400">
              Core capabilities are enabled under Apache-2.0. Activate Pro to unlock proprietary modules.
            </p>
          </div>
        {/if}

        {#if !proActive}
          <div class="pt-3 border-t border-gray-200 dark:border-gray-800 space-y-3">
            <h3 class="text-sm font-semibold text-gray-800 dark:text-gray-200">Activate Pro License</h3>

            {#if inputMode === 'idle'}
              <div class="flex items-center gap-2 flex-wrap">
                <button
                  onclick={() => inputMode = 'paste'}
                  class="ds-btn-primary px-4 py-2"
                >
                  Paste License JSON
                </button>
                <button
                  onclick={openFilePicker}
                  class="ds-btn-outline px-4 py-2"
                >
                  <Upload size={14} /> Upload License File
                </button>
              </div>
            {:else}
              <div class="space-y-3">
                <div class="relative">
                  <textarea
                    bind:value={licenseInput}
                    placeholder="Paste signed license JSON here..."
                    rows={8}
                    class="ds-textarea text-xs font-mono resize-y"
                  ></textarea>
                  <button
                    onclick={() => { licenseInput = ''; inputMode = 'idle' }}
                    class="absolute top-2 right-2 text-gray-400 hover:text-gray-200"
                    title="Cancel"
                  >
                    <X size={14} />
                  </button>
                </div>
                <div class="flex items-center gap-2 flex-wrap">
                  <button
                    onclick={activate}
                    disabled={activating || !licenseInput.trim()}
                    class="ds-btn-primary px-4 py-2 disabled:opacity-50 disabled:cursor-not-allowed"
                  >
                    {activating ? 'Activating...' : 'Activate License'}
                  </button>
                  <button
                    onclick={openFilePicker}
                    class="ds-btn-outline px-4 py-2"
                  >
                    <Upload size={14} /> Replace from File
                  </button>
                </div>
              </div>
            {/if}
          </div>
        {/if}

        <input
          bind:this={fileInput}
          type="file"
          accept=".json,application/json"
          onchange={handleFileUpload}
          class="hidden"
        />
      </section>
      {/if}

      {#if activeTab === 'license' || activeTab === 'instance' || activeTab === 'legal'}
      <aside class="space-y-4">
        {#if activeTab === 'license' || activeTab === 'instance'}
        <section class="ds-panel p-5">
          <div class="flex items-center gap-2 mb-3">
            <Building2 size={16} class="text-ch-orange" />
            <h2 class="text-sm font-semibold text-gray-800 dark:text-gray-200 uppercase tracking-wide">Brand & Instance</h2>
          </div>

          <div class="flex items-center gap-3">
            <img src={logo} alt="CH-UI mark" class="h-12 w-12 rounded-xl border border-orange-300/35 dark:border-orange-700/35 bg-gray-100 dark:bg-gray-900 p-1.5" />
            <div>
              <p class="text-sm font-semibold text-gray-900 dark:text-gray-100">CH-UI</p>
              <p class="text-xs text-gray-500 dark:text-gray-400">ClickHouse operations and analytics control surface</p>
            </div>
          </div>

          <div class="grid grid-cols-1 sm:grid-cols-2 gap-2 mt-3">
            <div class="ds-panel-muted p-2.5">
              <p class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Connected As</p>
              <p class="text-xs font-medium text-gray-800 dark:text-gray-200 mt-1">{session?.user || '—'}</p>
            </div>
            <div class="ds-panel-muted p-2.5">
              <p class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Role</p>
              <p class="text-xs font-medium text-gray-800 dark:text-gray-200 mt-1 uppercase">{session?.role || '—'}</p>
            </div>
            <div class="ds-panel-muted p-2.5">
              <p class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Connection</p>
              <p class="text-xs font-medium text-gray-800 dark:text-gray-200 mt-1">{session?.connectionName || '—'}</p>
            </div>
            <div class="ds-panel-muted p-2.5">
              <p class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Version</p>
              <p class="text-xs font-medium text-gray-800 dark:text-gray-200 mt-1">{session?.version || '—'}</p>
            </div>
          </div>
        </section>
        {/if}

        {#if activeTab === 'license' || activeTab === 'legal'}
        <section class="ds-panel p-5">
          <div class="flex items-center gap-2 mb-3">
            <Scale size={16} class="text-ch-orange" />
            <h2 class="text-sm font-semibold text-gray-800 dark:text-gray-200 uppercase tracking-wide">Legal Scope</h2>
          </div>

          <div class="space-y-2">
            {#each legalScopes as item}
              <div class="ds-panel-muted p-3">
                <div class="flex items-center justify-between gap-2 mb-1.5">
                  <p class="text-sm font-semibold text-gray-900 dark:text-gray-100">{item.title}</p>
                  <span class="ds-badge {item.badge === 'Apache-2.0' ? 'ds-badge-success' : item.badge === 'Proprietary' ? 'ds-badge-danger' : 'ds-badge-brand'}">{item.badge}</span>
                </div>
                <p class="text-xs text-gray-500 dark:text-gray-400 leading-relaxed">{item.description}</p>
              </div>
            {/each}
          </div>

          <div class="mt-3 flex items-center gap-2 flex-wrap">
            <a
              href="https://www.apache.org/licenses/LICENSE-2.0"
              target="_blank"
              rel="noreferrer"
              class="ds-btn-outline px-3 py-1"
            >
              <FileText size={12} /> Apache-2.0 Text <ExternalLink size={12} />
            </a>
            <a
              href="https://github.com/caioricciuti/ch-ui-cloud/blob/main/docs/license.md"
              target="_blank"
              rel="noreferrer"
              class="ds-btn-outline px-3 py-1"
            >
              <KeyRound size={12} /> CH-UI License Policy <ExternalLink size={12} />
            </a>
            <a
              href="https://github.com/caioricciuti/ch-ui-cloud/blob/main/docs/legal/terms-of-service.md"
              target="_blank"
              rel="noreferrer"
              class="ds-btn-outline px-3 py-1"
            >
              Terms <ExternalLink size={12} />
            </a>
            <a
              href="https://github.com/caioricciuti/ch-ui-cloud/blob/main/docs/legal/privacy-policy.md"
              target="_blank"
              rel="noreferrer"
              class="ds-btn-outline px-3 py-1"
            >
              Privacy <ExternalLink size={12} />
            </a>
          </div>
        </section>
        {/if}
      </aside>
      {/if}
    </div>

  </div>
</div>
