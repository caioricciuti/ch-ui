<script lang="ts">
  import { CircleHelp } from 'lucide-svelte'
  import Button from '../common/Button.svelte'

  let open = $state(false)
  let el = $state<HTMLDivElement | null>(null)

  $effect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => {
      if (el && !el.contains(e.target as Node)) open = false
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') open = false
    }
    document.addEventListener('mousedown', onDoc)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDoc)
      document.removeEventListener('keydown', onKey)
    }
  })

  const rows: [string, string][] = [
    ['timeout', 'word anywhere in the body, case-insensitive'],
    ['"rate limit"', 'exact phrase'],
    ['service:api-gateway', 'field equals value'],
    ['level:error', 'severity (also: severity:)'],
    ['http.status_code:500', 'attribute key equals value'],
    ['http.status_code:>=500', 'numeric comparison on a field'],
    ['user.id:*', 'attribute exists'],
    ['trace:*', 'has a trace id'],
    ['-level:debug  or  NOT level:debug', 'negate'],
    ['level:error OR level:fatal', 'either'],
    ['(a OR b) c', 'group; adjacent terms are AND'],
    ['api*', 'wildcard'],
  ]
</script>

<div class="relative" bind:this={el}>
  <Button icon variant="ghost" size="sm" aria-label="Search syntax" title="Search syntax" aria-pressed={open} onclick={() => (open = !open)}>
    <CircleHelp size={14} />
  </Button>
  {#if open}
    <div class="surface-card absolute right-0 top-9 z-40 w-[26rem] rounded-lg p-3">
      <div class="mb-2 text-[13px] font-semibold text-fg">Search syntax</div>
      <table class="w-full text-xs">
        <tbody>
          {#each rows as [ex, what]}
            <tr>
              <td class="whitespace-nowrap py-0.5 pr-3 font-mono text-fg">{ex}</td>
              <td class="py-0.5 text-fg-3">{what}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>
