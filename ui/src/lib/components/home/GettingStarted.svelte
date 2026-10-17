<script lang="ts">
  /**
   * The first-ten-minutes checklist on Home. Each step opens the place
   * where it is done and ticks itself off from real state, so nothing is
   * stored except a per-person "hide" preference. Admins see every step;
   * everyone else sees the ones their role can finish. The block renders
   * nothing once every visible step is done.
   */
  import { untrack } from 'svelte'
  import { CircleCheck, Circle } from 'lucide-svelte'
  import Panel from '../common/Panel.svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import { getSession } from '../../stores/session.svelte'
  import { goTo } from '../../stores/router.svelte'
  import { setSection } from '../../stores/nav.svelte'
  import { getActiveTab, openQueryTab } from '../../stores/tabs.svelte'
  import { listConnections } from '../../api/auth'
  import { fetchQueryHistory } from '../../api/history'
  import { listWorkspaceDashboards, listWorkspaceSavedQueries } from '../../api/workspace'
  import { adminListBrainProviders } from '../../api/brain'
  import { listMCPKeys } from '../../api/mcp'
  import { apiGet } from '../../api/client'

  /** `created_by` of the dashboard the server seeds on first start. */
  const SYSTEM_CREATED_BY = 'system'

  type StepId = 'connect' | 'run' | 'save' | 'dashboard' | 'provider' | 'client' | 'teammate'

  interface Step {
    id: StepId
    title: string
    description: string
    action: string
    run: () => void
    adminOnly?: boolean
  }

  const session = $derived(getSession())
  const isAdmin = $derived(session?.role === 'admin')

  function openAdmin(section: string): void {
    goTo('admin')
    setSection(section)
  }

  const STEPS: Step[] = [
    {
      id: 'connect',
      title: 'Connect ClickHouse',
      description: 'Add the ClickHouse this instance should query, or check the embedded one is online.',
      action: 'Open connections',
      run: () => openAdmin('tunnels'),
      adminOnly: true,
    },
    {
      id: 'run',
      title: 'Run a query',
      description: 'Open a query tab and run any SELECT. It lands in your history.',
      action: 'New query',
      run: () => openQueryTab('SELECT version() AS version, uptime() AS uptime_seconds'),
    },
    {
      id: 'save',
      title: 'Save a query',
      description: 'Save one you will run again. Saved queries feed dashboards, schedules and alerts.',
      action: 'New query',
      run: () => openQueryTab(),
    },
    {
      id: 'dashboard',
      title: 'Make a dashboard',
      description: 'Put a saved query on a panel.',
      action: 'Open dashboards',
      run: () => goTo('dashboards'),
    },
    {
      id: 'provider',
      title: 'Add an AI provider',
      description: 'Brain needs a model: Anthropic, OpenAI, or a local Ollama.',
      action: 'Open Brain settings',
      run: () => openAdmin('brain'),
      adminOnly: true,
    },
    {
      id: 'client',
      title: 'Connect an AI client',
      description: 'Claude Code, Claude Desktop or Cursor can use this instance through MCP.',
      action: 'Open MCP Server',
      run: () => openAdmin('mcp'),
      adminOnly: true,
    },
    {
      id: 'teammate',
      title: 'Invite a teammate',
      description: 'Create a ClickHouse user for them, or turn on SSO.',
      action: 'Open users',
      run: () => openAdmin('users'),
      adminOnly: true,
    },
  ]

  const steps = $derived(STEPS.filter((s) => !s.adminOnly || isAdmin))

  // true done, false not yet, null unknown (the check failed): shown as not done, never ticked.
  let done = $state<Partial<Record<StepId, boolean | null>>>({})
  let loaded = $state(false)
  let hidden = $state(false)

  const storageKey = $derived(`ch-ui-getting-started-hidden:${session?.user ?? ''}`)

  const doneCount = $derived(steps.filter((s) => done[s.id] === true).length)
  const complete = $derived(loaded && steps.every((s) => done[s.id] === true))
  const visible = $derived(loaded && !hidden && !complete)

  async function check<T>(id: StepId, load: () => Promise<T>, test: (v: T) => boolean): Promise<void> {
    try {
      // Await before spreading: the checks run in parallel and each must
      // merge into the latest `done`, not the one it saw when it started.
      const value = test(await load())
      done = { ...done, [id]: value }
    } catch {
      done = { ...done, [id]: null }
    }
  }

  let refreshing = false

  /** Re-check every step. Cheap: one small request per step, all in parallel. */
  async function refresh(admin: boolean): Promise<void> {
    if (refreshing) return
    refreshing = true
    try {
      const checks: Promise<void>[] = [
        check('run', () => fetchQueryHistory({ limit: 1 }), (rows) => rows.length > 0),
        check('save', listWorkspaceSavedQueries, (rows) => rows.length > 0),
        // The built-in System Overview is created by the server, not by a person.
        check('dashboard', listWorkspaceDashboards, (rows) => rows.some((d) => d.created_by !== SYSTEM_CREATED_BY)),
      ]
      if (admin) {
        checks.push(
          check('connect', listConnections, (rows) => rows.some((c) => c.online)),
          check('provider', adminListBrainProviders, (rows) => rows.length > 0),
          check('client', listMCPKeys, (res) => res.keys.some((k) => !(k as { revoked_at?: string | null }).revoked_at)),
          check(
            'teammate',
            () => apiGet<{ users?: unknown[] } | unknown[]>('/api/admin/users'),
            (res) => (Array.isArray(res) ? res : (res.users ?? [])).length > 1,
          ),
        )
      }
      await Promise.all(checks)
      loaded = true
    } finally {
      refreshing = false
    }
  }

  // Home stays mounted while other tabs are active, and it can mount before
  // the session is known. Re-check whenever Home becomes the active tab or
  // the signed-in person changes, so a step done elsewhere ticks on return.
  $effect(() => {
    const user = session?.user
    const admin = isAdmin
    const key = storageKey
    const onHome = getActiveTab()?.type === 'home'
    if (!user || !onHome) return
    untrack(() => {
      try {
        hidden = localStorage.getItem(key) === '1'
      } catch {
        hidden = false
      }
      void refresh(admin)
    })
  })

  function hide(): void {
    hidden = true
    try {
      localStorage.setItem(storageKey, '1')
    } catch {
      /* private mode */
    }
  }
</script>

{#if visible}
  <Panel title="Getting started" description="{doneCount} of {steps.length} done. Each step opens where it is done and ticks itself off." padding="none">
    {#snippet actions()}
      <Button variant="ghost" size="xs" onclick={hide}>Hide</Button>
    {/snippet}
    <ol class="mt-3 divide-y divide-edge-subtle border-t border-edge-subtle">
      {#each steps as step (step.id)}
        {@const isDone = done[step.id] === true}
        <li class="flex items-center gap-3 px-4 py-2.5">
          {#if isDone}
            <CircleCheck size={16} class="shrink-0 text-success" aria-hidden="true" />
          {:else}
            <Circle size={16} class="shrink-0 text-fg-4" aria-hidden="true" />
          {/if}
          <div class="min-w-0 flex-1">
            <p class="text-[13px] font-medium {isDone ? 'text-fg-3' : 'text-fg'}">{step.title}</p>
            {#if !isDone}<p class="text-xs text-fg-3">{step.description}</p>{/if}
          </div>
          {#if isDone}
            <Badge tone="success">Done</Badge>
          {:else}
            <Button variant="outline" size="sm" onclick={step.run}>{step.action}</Button>
          {/if}
        </li>
      {/each}
    </ol>
  </Panel>
{/if}
