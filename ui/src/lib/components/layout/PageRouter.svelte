<script lang="ts">
  import { errorText } from '../../utils/errors'
  import { onMount } from 'svelte'
  import { getRouteType, getCurrentDashboardId, goTo } from '../../stores/router.svelte'
  import { loadLicense, hasProReadAccess, isLicenseInGrace, isLicenseLoaded, getLicense } from '../../stores/license.svelte'
  import { formatDate } from '../../utils/format'
  import { AlertTriangle } from 'lucide-svelte'
  import { PAGE_ROUTES, isPageRouteType } from '../../routes'
  import ProRequired from '../common/ProRequired.svelte'
  import Button from '../common/Button.svelte'
  import Spinner from '../common/Spinner.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import { loadPage, loadedPage, type LoadedPage } from './pageLoaders'

  // Gating reads the shared license store. Settings writes activation and
  // deactivation results into the same store, so Pro pages lock and unlock
  // without a refresh.
  onMount(() => {
    void loadLicense()
  })
  const licenseChecked = $derived(isLicenseLoaded())

  const type = $derived(getRouteType())
  const meta = $derived(isPageRouteType(type) ? PAGE_ROUTES[type] : undefined)

  const gated = $derived(!!meta?.pro && !hasProReadAccess())
  // Expired Pro license in grace: Pro pages open, the backend refuses writes.
  const readOnlyGrace = $derived(!!meta?.pro && isLicenseInGrace())

  // Pages load on first visit (see pageLoaders.ts) and stay cached. Page is
  // derived, not set from the effect, so a route change never renders the
  // previous page under the new route for a frame.
  let loadedVersion = $state(0)
  let loadFailed = $state<string | null>(null)
  let attempt = $state(0)

  const Page = $derived.by<LoadedPage | undefined>(() => {
    void loadedVersion
    return isPageRouteType(type) && !gated ? loadedPage(type) : undefined
  })

  $effect(() => {
    void attempt
    const route = type
    loadFailed = null
    // A locked Pro page renders ProRequired; no need to download it.
    if (!isPageRouteType(route) || gated || loadedPage(route)) return
    let cancelled = false
    loadPage(route).then(
      () => {
        if (!cancelled) loadedVersion++
      },
      (e: unknown) => {
        if (!cancelled) loadFailed = errorText(e)
      },
    )
    return () => {
      cancelled = true
    }
  })
</script>

{#if meta}
  {#if gated}
    {#if !licenseChecked}
      <div class="flex h-full items-center justify-center text-[13px] text-fg-3">Checking license…</div>
    {:else}
      <ProRequired feature={meta.label} />
    {/if}
  {:else if loadFailed}
    <EmptyState
      icon={AlertTriangle}
      title="Could not load {meta.label}"
      description="{loadFailed}. A new version may have been deployed; reload to get it."
      primary={{ label: 'Retry', onclick: () => attempt++ }}
      secondary={{ label: 'Reload', onclick: () => window.location.reload() }}
    />
  {:else if !Page}
    <div class="flex h-full items-center justify-center text-fg-3" aria-busy="true" aria-label="Loading {meta.label}">
      <Spinner size="md" />
    </div>
  {:else if readOnlyGrace}
    <div class="flex h-full flex-col">
      <div class="flex shrink-0 items-center gap-2 border-b border-warning/40 bg-warning-soft px-4 py-2 text-[13px] text-fg">
        <AlertTriangle size={14} class="shrink-0 text-warning" />
        <span class="min-w-0 flex-1">
          Pro license expired. Read-only until {formatDate(getLicense()?.grace_until)}. Changes are blocked until a
          renewed license is activated.
        </span>
        <Button variant="outline" size="xs" onclick={() => goTo('settings', 'License')}>Manage license</Button>
      </div>
      <div class="min-h-0 flex-1">
        {@render page()}
      </div>
    </div>
  {:else}
    {@render page()}
  {/if}
{/if}

{#snippet page()}
  {#key type}
    {#if Page?.kind === 'dashboards'}
      <Page.component dashboardId={getCurrentDashboardId()} />
    {:else if Page}
      <Page.component />
    {/if}
  {/key}
{/snippet}
