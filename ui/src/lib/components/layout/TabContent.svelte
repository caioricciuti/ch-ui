<script lang="ts">
  import { getGroupActiveTab } from '../../stores/tabs.svelte'
  import type { QueryTab, TableTab, DatabaseTab, ModelTab } from '../../stores/tabs.svelte'
  import QueryContent from './content/QueryContent.svelte'
  import TableContent from './content/TableContent.svelte'
  import DatabaseContent from './content/DatabaseContent.svelte'
  import ModelContent from './content/ModelContent.svelte'
  import Home from '../../../pages/Home.svelte'

  interface Props {
    groupId: string
  }

  let { groupId }: Props = $props()

  // Pro gating lives in PageRouter now: every workspace tab type is free.
  const activeTab = $derived(getGroupActiveTab(groupId))
</script>

<div class="flex-1 min-h-0 overflow-hidden">
  {#if !activeTab}
    <div class="flex h-full items-center justify-center text-[13px] text-fg-4">
      Open a query or select a table to get started
    </div>
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
  {:else if activeTab.type === 'model'}
    {#key activeTab.id}
      <ModelContent tab={activeTab as ModelTab} />
    {/key}
  {:else if activeTab.type === 'home'}
    <Home />
  {/if}
</div>
