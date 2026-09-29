<script lang="ts">
  import { goTo } from '../../stores/router.svelte'
  import { Lock, ExternalLink, Check } from 'lucide-svelte'
  import Button from './Button.svelte'

  interface Props {
    feature: string
  }

  let { feature }: Props = $props()

  function openSettings() {
    goTo('settings', 'License')
  }

  // A few representative Pro areas; the full list lives on the pricing page.
  const proAreas = [
    'Governance, policies and guardrails',
    'Cluster Health, Query Insights and Cost Center',
    'Scheduled queries and alerts',
    'SSO (OIDC) and SIEM audit forwarding',
  ]
</script>

<div class="flex h-full items-center justify-center overflow-auto p-6">
  <div class="w-full max-w-md text-center">
    <div class="mx-auto mb-4 flex h-10 w-10 items-center justify-center rounded-md bg-accent-soft text-accent">
      <Lock size={18} strokeWidth={1.75} />
    </div>
    <h1 class="text-[15px] font-semibold tracking-[-0.01em] text-fg">Pro license required</h1>
    <p class="mt-1 text-[13px] leading-relaxed text-fg-3">
      <span class="font-medium text-fg">{feature}</span> is part of CH-UI Pro. Activate a license under
      Settings, or get one if you don't have it yet.
    </p>
    <p class="mt-4 text-[12px] text-fg-3">Pro also includes</p>

    <ul class="mx-auto mt-2 inline-flex flex-col items-start gap-1.5 text-left">
      {#each proAreas as feat}
        <li class="flex items-center gap-2 text-[13px] text-fg-2">
          <Check size={14} class="shrink-0 text-accent" />
          {feat}
        </li>
      {/each}
    </ul>
    <div class="mt-2">
      <a
        class="inline-flex items-center gap-1 text-xs font-medium text-accent hover:underline"
        href="https://ch-ui.com/pricing"
        target="_blank"
        rel="noreferrer"
      >
        See what's in Pro <ExternalLink size={12} />
      </a>
    </div>

    <div class="mt-6 flex items-center justify-center gap-2">
      <Button size="sm" onclick={openSettings}>Manage license</Button>
      <a
        class="inline-flex h-7 items-center gap-1.5 rounded-md px-2.5 text-xs font-medium text-fg-2 transition-colors hover:bg-hover hover:text-fg"
        href="https://ch-ui.com/pricing"
        target="_blank"
        rel="noreferrer"
      >
        Get a license <ExternalLink size={12} />
      </a>
    </div>
  </div>
</div>
