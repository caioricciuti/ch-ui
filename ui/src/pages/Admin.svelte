<script lang="ts">
  import MCPKeysPanel from '../lib/components/admin/MCPKeysPanel.svelte'
  import OverviewSection from '../lib/components/admin/OverviewSection.svelte'
  import ConnectionsSection from '../lib/components/admin/ConnectionsSection.svelte'
  import UsersSection from '../lib/components/admin/UsersSection.svelte'
  import BrainSection from '../lib/components/admin/BrainSection.svelte'
  import BrainBudgetsSection from '../lib/components/admin/BrainBudgetsSection.svelte'
  import BrainUsagePanel from '../lib/components/admin/BrainUsagePanel.svelte'
  import GitHubSection from '../lib/components/admin/GitHubSection.svelte'
  import SettingsSection from '../lib/components/admin/SettingsSection.svelte'
  import PageHeader from '../lib/components/common/PageHeader.svelte'
  import PageBody from '../lib/components/common/PageBody.svelte'
  import { getSection, setSection } from '../lib/stores/nav.svelte'
  import { getSession } from '../lib/stores/session.svelte'
  import EmptyState from '../lib/components/common/EmptyState.svelte'
  import { PAGE_SECTIONS, docsURL } from '../lib/routes'
  import { Shield } from 'lucide-svelte'

  // Sections come from the context panel via ?section=; each section is a
  // self-contained component that loads its own data on mount.
  type AdminTab = 'overview' | 'tunnels' | 'users' | 'brain' | 'brain-budgets' | 'brain-usage' | 'github' | 'mcp' | 'settings'
  const adminTabs = PAGE_SECTIONS.admin ?? []
  const adminTabIds = adminTabs.map((t) => t.id) as AdminTab[]
  let activeTab = $state<AdminTab>('overview')

  const isAdminRole = $derived(getSession()?.role === 'admin')

  // The panel label says Connections; the section id stayed `tunnels`.
  // Links written from the label still land on the right section.
  const sectionAliases: Record<string, AdminTab> = { connections: 'tunnels' }

  function normalizeAdminTab(value: string | null | undefined): AdminTab {
    const raw = (value ?? '').trim().toLowerCase()
    if ((adminTabIds as string[]).includes(raw)) return raw as AdminTab
    if (raw in sectionAliases) return sectionAliases[raw]
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
  <div class="flex h-full items-center justify-center overflow-auto">
    <EmptyState
      size="page"
      icon={Shield}
      title="Admin access required"
      description={`You are signed in as ${getSession()?.role ?? 'viewer'}. Ask a CH-UI admin to grant you the admin role under Admin, Users, then log in again.`}
    />
  </div>
{:else}
  <div class="flex h-full min-h-0 flex-col">
    <PageHeader title="Admin" subtitle={adminTabs.find((t) => t.id === activeTab)?.label} docs={docsURL('admin', activeTab)} />

    {#key activeTab}
      {#if activeTab === 'overview'}
        <OverviewSection />
      {:else if activeTab === 'tunnels'}
        <ConnectionsSection />
      {:else if activeTab === 'users'}
        <UsersSection />
      {:else if activeTab === 'brain'}
        <BrainSection />
      {:else if activeTab === 'brain-budgets'}
        <BrainBudgetsSection />
      {:else if activeTab === 'brain-usage'}
        <PageBody width="lg">
          <BrainUsagePanel />
        </PageBody>
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
