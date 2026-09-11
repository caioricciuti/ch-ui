<script lang="ts">
  import { onMount } from 'svelte'
  import { getRouteType, getCurrentDashboardId } from '../../stores/router.svelte'
  import { loadLicense, isProActive } from '../../stores/license.svelte'
  import { PAGE_ROUTES, isPageRouteType, type PageRoute } from '../../routes'
  import ProRequired from '../common/ProRequired.svelte'
  import Admin from '../../../pages/Admin.svelte'
  import SavedQueries from '../../../pages/SavedQueries.svelte'
  import Dashboards from '../../../pages/Dashboards.svelte'
  import Telemetry from '../../../pages/Telemetry.svelte'
  import Pipelines from '../../../pages/Pipelines.svelte'
  import Models from '../../../pages/Models.svelte'
  import Schedules from '../../../pages/Schedules.svelte'
  import Brain from '../../../pages/Brain.svelte'
  import Governance from '../../../pages/Governance.svelte'
  import ClusterHealth from '../../../pages/ClusterHealth.svelte'
  import QueryInsights from '../../../pages/QueryInsights.svelte'
  import CostCenter from '../../../pages/CostCenter.svelte'
  import Settings from '../../../pages/Settings.svelte'

  // Route type -> page component.
  const COMPONENTS: Record<PageRoute, typeof Admin> = {
    'saved-queries': SavedQueries,
    dashboards: Dashboards,
    telemetry: Telemetry,
    pipelines: Pipelines,
    models: Models,
    schedules: Schedules,
    brain: Brain,
    governance: Governance,
    'cluster-health': ClusterHealth,
    'query-insights': QueryInsights,
    'cost-center': CostCenter,
    admin: Admin,
    settings: Settings,
  }

  let licenseChecked = $state(false)
  onMount(() => {
    void Promise.resolve(loadLicense()).finally(() => (licenseChecked = true))
  })

  const type = $derived(getRouteType())
  const meta = $derived(isPageRouteType(type) ? PAGE_ROUTES[type] : undefined)
  const Page = $derived(isPageRouteType(type) ? COMPONENTS[type] : undefined)
  const gated = $derived(!!meta?.pro && !isProActive())
</script>

{#if Page && meta}
  {#if gated}
    {#if !licenseChecked}
      <div class="flex h-full items-center justify-center text-[13px] text-fg-3">Checking license…</div>
    {:else}
      <ProRequired feature={meta.label} />
    {/if}
  {:else}
    {#key type}
      {#if type === 'dashboards'}
        <Dashboards dashboardId={getCurrentDashboardId()} />
      {:else}
        <Page />
      {/if}
    {/key}
  {/if}
{/if}
