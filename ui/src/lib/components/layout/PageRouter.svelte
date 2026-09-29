<script lang="ts">
  import { onMount } from 'svelte'
  import { getRouteType, getCurrentDashboardId, goTo } from '../../stores/router.svelte'
  import { loadLicense, hasProReadAccess, isLicenseInGrace, isLicenseLoaded, getLicense } from '../../stores/license.svelte'
  import { formatDate } from '../../utils/format'
  import { AlertTriangle } from 'lucide-svelte'
  import { PAGE_ROUTES, isPageRouteType, type PageRoute } from '../../routes'
  import ProRequired from '../common/ProRequired.svelte'
  import Button from '../common/Button.svelte'
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
  import Performance from '../../../pages/Performance.svelte'
  import Fleet from '../../../pages/Fleet.svelte'
  import SchemaCompare from '../../../pages/SchemaCompare.svelte'
  import OperationsReports from '../../../pages/OperationsReports.svelte'
  import IncidentTimeline from '../../../pages/IncidentTimeline.svelte'
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
    performance: Performance,
    fleet: Fleet,
    'schema-compare': SchemaCompare,
    'operations-reports': OperationsReports,
    'incident-timeline': IncidentTimeline,
    admin: Admin,
    settings: Settings,
  }

  // Gating reads the shared license store. Settings writes activation and
  // deactivation results into the same store, so Pro pages lock and unlock
  // without a refresh.
  onMount(() => {
    void loadLicense()
  })
  const licenseChecked = $derived(isLicenseLoaded())

  const type = $derived(getRouteType())
  const meta = $derived(isPageRouteType(type) ? PAGE_ROUTES[type] : undefined)
  const Page = $derived(isPageRouteType(type) ? COMPONENTS[type] : undefined)
  const gated = $derived(!!meta?.pro && !hasProReadAccess())
  // Expired Pro license in grace: Pro pages open, the backend refuses writes.
  const readOnlyGrace = $derived(!!meta?.pro && isLicenseInGrace())
</script>

{#if Page && meta}
  {#if gated}
    {#if !licenseChecked}
      <div class="flex h-full items-center justify-center text-[13px] text-fg-3">Checking license…</div>
    {:else}
      <ProRequired feature={meta.label} />
    {/if}
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
    {#if type === 'dashboards'}
      <Dashboards dashboardId={getCurrentDashboardId()} />
    {:else if Page}
      <Page />
    {/if}
  {/key}
{/snippet}
