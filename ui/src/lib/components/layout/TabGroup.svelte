<script lang="ts">
  import TabBar from './TabBar.svelte'
  import TabContent from './TabContent.svelte'
  import { getFocusedGroupId, isSplit } from '../../stores/tabs.svelte'

  interface Props {
    groupId: string
  }

  let { groupId }: Props = $props()

  const isFocused = $derived(isSplit() && getFocusedGroupId() === groupId)
</script>

<div class="flex min-h-0 min-w-0 flex-1 flex-col {isSplit() ? (isFocused ? 'ring-1 ring-inset ring-accent/40' : 'ring-1 ring-inset ring-transparent') : ''}">
  <TabBar {groupId} />
  <TabContent {groupId} />
</div>
