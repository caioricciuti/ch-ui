<script lang="ts">
  import { Brain, BarChart3, Search, Wand2, Sparkles } from 'lucide-svelte'

  interface Props {
    onPick?: (prompt: string) => void
  }
  let { onPick }: Props = $props()

  const suggestions = [
    {
      icon: Sparkles,
      title: 'Profile a table',
      prompt: 'Pick my most-active table and give me a deep profile — row count, null %, distinct counts, value ranges. Tell me what stands out.',
    },
    {
      icon: BarChart3,
      title: 'Build a dashboard',
      prompt: "Build me a nice overview dashboard for my main events table. Be creative — pick the right charts, name it well, and add 4-6 panels.",
    },
    {
      icon: Search,
      title: 'Find anomalies',
      prompt: 'Run a quick scan across my biggest tables this week — anything unusual? Spike in errors, drop in volume, weird outliers, anything you would flag to a data lead.',
    },
    {
      icon: Wand2,
      title: 'Create a model',
      prompt: 'Suggest a useful incremental model based on my existing tables (daily aggregates, rolling metrics, dim/fact). Pick the best candidate, draft it, and propose building it.',
    },
  ]
</script>

<div class="flex h-full items-center justify-center px-6">
  <div class="w-full max-w-2xl text-center">
    <div class="mb-4 inline-flex h-10 w-10 items-center justify-center rounded-md bg-surface-2 text-fg-3">
      <Brain size={18} strokeWidth={1.75} />
    </div>
    <h2 class="text-[14px] font-semibold text-fg">Brain can act on your workspace</h2>
    <p class="mx-auto mt-1 max-w-[46ch] text-[13px] leading-relaxed text-fg-3">
      Ask anything. Brain reads your real ClickHouse and, with your approval, creates models, dashboards, panels and queries for you.
    </p>
    {#if onPick}
      <div class="mt-6 grid grid-cols-1 gap-2 text-left sm:grid-cols-2">
        {#each suggestions as s}
          <button
            type="button"
            onclick={() => onPick?.(s.prompt)}
            class="group rounded-lg border border-edge-subtle bg-surface p-3 text-left transition-colors hover:border-edge-strong hover:bg-hover"
          >
            <div class="mb-1 flex items-center gap-2">
              <s.icon size={14} class="text-accent" />
              <span class="text-xs font-medium text-fg">{s.title}</span>
            </div>
            <p class="line-clamp-2 text-[11px] leading-relaxed text-fg-3">{s.prompt}</p>
          </button>
        {/each}
      </div>
    {/if}
  </div>
</div>
