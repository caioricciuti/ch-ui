<script lang="ts">
  /**
   * Who ran a set of query_log rows: the person when CH-UI's query tag
   * named one (SSO email, job owner), the ClickHouse account otherwise.
   * When the rows ran as accounts other than the name itself, a muted
   * "via" line lists them: the one account, or a count with the full
   * list on hover.
   */
  interface Props {
    name: string
    /** ClickHouse accounts behind the rows; non-array values are ignored. */
    accounts?: unknown
  }

  let { name, accounts }: Props = $props()

  const via = $derived(
    Array.isArray(accounts)
      ? accounts.map(String).filter((a) => a && a !== name).sort()
      : [],
  )
</script>

<span class="flex min-w-0 flex-col leading-tight">
  <span class="truncate">{name}</span>
  {#if via.length === 1}
    <span class="truncate text-[11px] text-fg-3" title={`Ran as ClickHouse user ${via[0]}`}>via {via[0]}</span>
  {:else if via.length > 1}
    <span class="text-[11px] text-fg-3" title={`Ran as ClickHouse users: ${via.join(', ')}`}>via {via.length} accounts</span>
  {/if}
</span>
