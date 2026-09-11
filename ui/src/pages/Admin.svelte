<script lang="ts">
  import MCPKeysPanel from '../lib/components/admin/MCPKeysPanel.svelte'
  import OverviewSection from '../lib/components/admin/OverviewSection.svelte'
  import ConnectionsSection from '../lib/components/admin/ConnectionsSection.svelte'
  import UsersSection from '../lib/components/admin/UsersSection.svelte'
  import BrainSection from '../lib/components/admin/BrainSection.svelte'
  import GitHubSection from '../lib/components/admin/GitHubSection.svelte'
  import SettingsSection from '../lib/components/admin/SettingsSection.svelte'
  import PageHeader from '../lib/components/common/PageHeader.svelte'
  import PageBody from '../lib/components/common/PageBody.svelte'
  import { getSection, setSection } from '../lib/stores/nav.svelte'
  import { getSession } from '../lib/stores/session.svelte'
  import { PAGE_SECTIONS } from '../lib/routes'
  import { Shield } from 'lucide-svelte'

  // Sections come from the context panel via ?section=; each section is a
  // self-contained component that loads its own data on mount.
  type AdminTab = 'overview' | 'tunnels' | 'users' | 'brain' | 'github' | 'mcp' | 'settings'
  const adminTabs = PAGE_SECTIONS.admin ?? []
  const adminTabIds = adminTabs.map((t) => t.id) as AdminTab[]
  let activeTab = $state<AdminTab>('overview')

  const isAdminRole = $derived(getSession()?.role === 'admin')

  function normalizeAdminTab(value: string | null | undefined): AdminTab {
    const raw = (value ?? '').trim().toLowerCase()
    if ((adminTabIds as string[]).includes(raw)) return raw as AdminTab
    return 'overview'
  }

  $effect(() => {
    const next = getSection()
    if (next === null) {
      // Seed the URL so the panel highlights the section and reloads land here.
      if (typeof window !== 'undefined' && window.location.pathname.endsWith('/admin')) setSection(activeTab)
      return
    }
    const tab = normalizeAdminTab(next)
    if (tab !== activeTab) activeTab = tab
  })
</script>

{#if !isAdminRole}
  <div class="flex h-full flex-col items-center justify-center gap-3 px-6 text-center">
    <Shield size={28} class="text-fg-4" />
    <h1 class="text-[15px] font-semibold tracking-[-0.01em] text-fg">Admin access required</h1>
    <p class="max-w-md text-[13px] text-fg-3">
      You are signed in as <span class="font-semibold">{getSession()?.role ?? 'viewer'}</span>.
      Ask a CH-UI admin to grant you the admin role (Admin &rarr; Users), then log in again.
    </p>
  </div>
{:else}
  <div class="flex h-full min-h-0 flex-col">
    <PageHeader title="Admin" subtitle={adminTabs.find((t) => t.id === activeTab)?.label} />

    {#key activeTab}
      {#if activeTab === 'overview'}
        <OverviewSection />
      {:else if activeTab === 'tunnels'}
        <ConnectionsSection />
      {:else if activeTab === 'users'}
        <UsersSection />
      {:else if activeTab === 'brain'}
        <BrainSection />
      {:else if activeTab === 'github'}
        <GitHubSection />
      {:else if activeTab === 'mcp'}
        <PageBody width="md">
          <MCPKeysPanel />
        </PageBody>
      {:else if activeTab === 'settings'}
        <SettingsSection />
      {/if}
    {/key}
  </div>
{/if}
