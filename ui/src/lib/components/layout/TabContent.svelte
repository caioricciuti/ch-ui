<script lang="ts">
  import { onMount } from 'svelte'
  import { getGroupActiveTab } from '../../stores/tabs.svelte'
  import type { QueryTab, TableTab, DatabaseTab, DashboardTab } from '../../stores/tabs.svelte'
  import { loadLicense, isProActive, isLicenseLoading } from '../../stores/license.svelte'
  import QueryContent from './content/QueryContent.svelte'
  import TableContent from './content/TableContent.svelte'
  import DatabaseContent from './content/DatabaseContent.svelte'
  import ProRequired from '../common/ProRequired.svelte'
  import SavedQueries from '../../../pages/SavedQueries.svelte'
  import Settings from '../../../pages/Settings.svelte'
  import Dashboards from '../../../pages/Dashboards.svelte'
  import Schedules from '../../../pages/Schedules.svelte'
  import BrainPage from '../../../pages/Brain.svelte'
  import Admin from '../../../pages/Admin.svelte'
  import Governance from '../../../pages/Governance.svelte'
  import Home from '../../../pages/Home.svelte'

  interface Props {
    groupId: string
  }

  let { groupId }: Props = $props()

  const activeTab = $derived(getGroupActiveTab(groupId))
  const proActive = $derived(isProActive())
  const licenseLoading = $derived(isLicenseLoading())
  const requiresPro = $derived(!!activeTab && ['dashboards', 'dashboard', 'schedules', 'brain', 'admin', 'governance'].includes(activeTab.type))
  let licenseChecked = $state(false)

  onMount(() => {
    void loadLicense().finally(() => {
      licenseChecked = true
    })
  })

  $effect(() => {
    if (requiresPro && !licenseChecked) {
      void loadLicense().finally(() => {
        licenseChecked = true
      })
    }
  })

  function proFeatureLabel(): string {
    if (!activeTab) return 'this section'
    switch (activeTab.type) {
      case 'dashboards':
      case 'dashboard':
        return 'Dashboards'
      case 'schedules':
        return 'Scheduled Jobs'
      case 'brain':
        return 'Brain AI'
      case 'governance':
        return 'Governance'
      case 'admin':
        return 'Admin Panel'
      default:
        return 'this section'
    }
  }
</script>

<div class="flex-1 min-h-0 overflow-hidden">
  {#if !activeTab}
    <div class="flex items-center justify-center h-full text-gray-400 dark:text-gray-600 text-sm">
      Open a query or select a table to get started
    </div>
  {:else if requiresPro && !proActive}
    {#if licenseLoading || !licenseChecked}
      <div class="flex items-center justify-center h-full text-gray-500 dark:text-gray-400 text-sm">
        Checking license...
      </div>
    {:else}
      <ProRequired feature={proFeatureLabel()} />
    {/if}
  {:else if activeTab.type === 'query'}
    {#key activeTab.id}
      <QueryContent tab={activeTab as QueryTab} />
    {/key}
  {:else if activeTab.type === 'table'}
    {#key activeTab.id}
      <TableContent tab={activeTab as TableTab} />
    {/key}
  {:else if activeTab.type === 'database'}
    {#key activeTab.id}
      <DatabaseContent tab={activeTab as DatabaseTab} />
    {/key}
  {:else if activeTab.type === 'saved-queries'}
    <SavedQueries />
  {:else if activeTab.type === 'settings'}
    <Settings />
  {:else if activeTab.type === 'dashboards'}
    <Dashboards />
  {:else if activeTab.type === 'dashboard'}
    {#key activeTab.id}
      <Dashboards dashboardId={(activeTab as DashboardTab).dashboardId} />
    {/key}
  {:else if activeTab.type === 'schedules'}
    <Schedules />
  {:else if activeTab.type === 'brain'}
    <BrainPage />
  {:else if activeTab.type === 'admin'}
    <Admin />
  {:else if activeTab.type === 'governance'}
    <Governance />
  {:else if activeTab.type === 'home'}
    <Home />
  {/if}
</div>
