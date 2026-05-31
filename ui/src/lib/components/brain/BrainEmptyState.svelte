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

<div class="h-full flex items-center justify-center px-6">
  <div class="max-w-2xl w-full text-center">
    <div class="inline-flex items-center justify-center w-14 h-14 rounded-2xl bg-ch-blue/10 mb-4">
      <Brain size={28} class="text-ch-blue" />
    </div>
    <h2 class="text-lg font-semibold text-foreground mb-1">Brain can act on your workspace</h2>
    <p class="text-sm text-muted-foreground mb-6">
      Ask anything. Brain reads your real ClickHouse and (with your approval) creates models, dashboards, panels, and queries for you.
    </p>
    {#if onPick}
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-2 text-left">
        {#each suggestions as s}
          <button
            type="button"
            onclick={() => onPick?.(s.prompt)}
            class="group p-3 rounded-lg border border-border bg-card/40 hover:bg-card hover:border-ch-blue/40 transition-colors"
          >
            <div class="flex items-center gap-2 mb-1">
              <s.icon size={14} class="text-ch-blue" />
              <span class="text-xs font-medium text-foreground">{s.title}</span>
            </div>
            <p class="text-[11px] text-muted-foreground line-clamp-2">{s.prompt}</p>
          </button>
        {/each}
      </div>
    {/if}
  </div>
</div>
