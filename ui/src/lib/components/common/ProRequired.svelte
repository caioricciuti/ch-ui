<script lang="ts">
  import { openSingletonTab } from '../../stores/tabs.svelte'
  import {
    ShieldAlert,
    Settings,
    ExternalLink,
    CheckCircle2,
    Lock,
  } from 'lucide-svelte'
  import logo from '../../../assets/logo.png'

  interface Props {
    feature: string
  }

  let { feature }: Props = $props()

  function openSettings() {
    openSingletonTab('settings', 'License')
  }

  const proFeatures = [
    'Dashboards and panel builder',
    'Scheduled query jobs',
    'Brain AI workflows',
    'Governance, Access and Policy controls',
    'Admin and multi-connection operations',
  ]
</script>

<div class="h-full overflow-auto p-6">
  <div class="mx-auto max-w-2xl">
    <div class="ds-panel overflow-hidden">
      <!-- Header -->
      <div class="px-6 py-5 border-b border-orange-300/25 dark:border-orange-700/35 bg-gradient-to-r from-orange-100/60 via-transparent to-transparent dark:from-orange-500/10">
        <div class="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
          <div class="flex items-center gap-4 min-w-0">
            <div class="h-14 w-14 rounded-2xl border border-orange-300/40 dark:border-orange-700/40 bg-gray-100 dark:bg-gray-900 grid place-items-center overflow-hidden shrink-0">
              <img src={logo} alt="CH-UI logo" class="h-11 w-11 object-contain" />
            </div>
            <div>
              <div class="flex items-center gap-2">
                <ShieldAlert size={18} class="text-ch-orange" />
                <h1 class="text-xl font-semibold text-gray-900 dark:text-gray-100">Pro license required</h1>
              </div>
              <p class="text-sm text-gray-600 dark:text-gray-400 mt-1">
                <span class="text-ch-orange font-medium">{feature}</span> is part of CH-UI Pro.
              </p>
            </div>
          </div>
        </div>
      </div>

      <!-- Content -->
      <div class="p-6 space-y-6">
        <!-- Locked feature callout -->
        <div class="flex items-center gap-3 rounded-lg border border-orange-300/30 dark:border-orange-700/30 bg-orange-50/50 dark:bg-orange-900/10 px-4 py-3">
          <Lock size={16} class="text-ch-orange shrink-0" />
          <p class="text-sm text-gray-700 dark:text-gray-300">
            Activate a Pro license in <strong>License</strong> to unlock this feature, or get a license if you don't have one yet.
          </p>
        </div>

        <!-- What you unlock -->
        <div>
          <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-3">What you unlock with Pro</h3>
          <ul class="space-y-2">
            {#each proFeatures as feat}
              <li class="flex items-center gap-2 text-sm text-gray-600 dark:text-gray-400">
                <CheckCircle2 size={14} class="text-ch-orange shrink-0" />
                {feat}
              </li>
            {/each}
          </ul>
        </div>

        <!-- Actions -->
        <div class="flex flex-col sm:flex-row items-start sm:items-center gap-3 pt-2 border-t border-gray-200 dark:border-gray-800">
          <button class="ds-btn-primary px-4 py-2" onclick={openSettings}>
            <Settings size={14} />
            Manage License
          </button>

          <a
            class="inline-flex items-center gap-1.5 text-sm text-ch-orange hover:underline"
            href="https://ch-ui.com/pricing"
            target="_blank"
            rel="noreferrer"
          >
            Get a License <ExternalLink size={12} />
          </a>
        </div>
      </div>
    </div>
  </div>
</div>
