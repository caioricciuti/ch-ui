<script lang="ts">
  import TabBar from './TabBar.svelte'
  import TabContent from './TabContent.svelte'
  import { getFocusedGroupId } from '../../stores/tabs.svelte'

  interface Props {
    groupId: string
  }

  let { groupId }: Props = $props()

  const isFocused = $derived(getFocusedGroupId() === groupId)
</script>

<div class="flex flex-col flex-1 min-w-0 min-h-0 {isFocused ? 'border-t-2 border-t-ch-blue' : 'border-t-2 border-t-transparent'}">
  <TabBar {groupId} />
  <TabContent {groupId} />
</div>
