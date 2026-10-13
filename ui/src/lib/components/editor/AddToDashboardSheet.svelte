<script lang="ts">
  import type { Dashboard, DashboardFolder, PanelConfig } from '../../types/api'
  import { listDashboards, listDashboardFolders, createDashboard, createPanel, folderPath } from '../../api/dashboards'
  import { pushDashboardDetail } from '../../stores/router.svelte'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import Sheet from '../common/Sheet.svelte'
  import Button from '../common/Button.svelte'
  import FormField from '../common/FormField.svelte'
  import Input from '../common/Input.svelte'
  import Combobox from '../common/Combobox.svelte'
  import type { ComboboxOption } from '../common/Combobox.svelte'
  import Spinner from '../common/Spinner.svelte'

  interface Props {
    open: boolean
    onclose: () => void
    /** The statement that produced the result; the panel runs it as is. */
    sql: string
    connectionId?: string
    config: PanelConfig
    /** Client-side filters hide rows in the editor; the panel will not. */
    filtered?: boolean
  }

  let { open, onclose, sql, connectionId = '', config, filtered = false }: Props = $props()

  const NEW = '__new__'

  let dashboards = $state<Dashboard[]>([])
  let folders = $state<DashboardFolder[]>([])
  let loading = $state(false)
  let saving = $state(false)

  let dashboardId = $state(NEW)
  let newName = $state('')
  let folderId = $state('')
  let panelName = $state('')
  let width = $state<'half' | 'full'>('half')
  let formError = $state<string | null>(null)
  // A dashboard created by this sheet whose panel could not be added.
  let orphan = $state<Dashboard | null>(null)

  const dashboardOptions = $derived<ComboboxOption[]>([
    { value: NEW, label: 'New dashboard' },
    ...dashboards.map((d) => {
      const path = folderPath(folders, d.folder_id).join(' / ')
      return { value: d.id, label: d.name, hint: path || undefined, keywords: `${d.name} ${path}` }
    }),
  ])
  const folderOptions = $derived<ComboboxOption[]>([
    { value: '', label: 'No folder' },
    ...folders.map((f) => ({ value: f.id, label: folderPath(folders, f.id).join(' / ') })),
  ])
  const isNew = $derived(dashboardId === NEW)

  function defaultPanelName(): string {
    const label: Record<PanelConfig['chartType'], string> = {
      table: 'Table', stat: 'Stat', timeseries: 'Time series', bar: 'Bar chart', text: 'Text', gauge: 'Gauge', pie: 'Pie chart',
    }
    const y = config.chartType === 'stat' ? config.statField : config.yColumns?.[0]
    return y ? `${y} ${config.chartType === 'stat' ? '' : 'by ' + (config.xColumn ?? '')}`.trim() : label[config.chartType]
  }

  async function load() {
    loading = true
    try {
      const [d, f] = await Promise.all([listDashboards(), listDashboardFolders()])
      dashboards = d
      folders = f
    } catch (e: unknown) {
      formError = e instanceof Error ? e.message : String(e)
    } finally {
      loading = false
    }
  }

  // Each opening starts from a fresh form and a fresh dashboard list.
  let wasOpen = false
  $effect(() => {
    if (open && !wasOpen) {
      dashboardId = NEW
      newName = ''
      folderId = ''
      panelName = defaultPanelName()
      width = config.chartType === 'stat' ? 'half' : 'full'
      formError = null
      orphan = null
      void load()
    }
    wasOpen = open
  })

  function openDashboard(id: string) {
    onclose()
    pushDashboardDetail(id)
  }

  /** The SQL a panel runs: one statement, without the trailing semicolon. */
  function panelSql(): string {
    return sql.trim().replace(/;+\s*$/, '').trim()
  }

  async function handleAdd() {
    formError = null
    if (isNew && !newName.trim()) {
      formError = 'Name the new dashboard'
      return
    }
    if (!panelName.trim()) {
      formError = 'Name the panel'
      return
    }
    saving = true
    let target: Dashboard | null = null
    try {
      target = isNew
        ? await createDashboard({ name: newName.trim(), folder_id: folderId || null })
        : (dashboards.find((d) => d.id === dashboardId) ?? null)
      if (!target) throw new Error('Dashboard not found')
    } catch (e: unknown) {
      formError = e instanceof Error ? e.message : String(e)
      saving = false
      return
    }

    try {
      await createPanel(target.id, {
        name: panelName.trim(),
        panel_type: config.chartType,
        query: panelSql(),
        config: JSON.stringify(config),
        ...(connectionId ? { connection_id: connectionId } : {}),
        layout_x: 0,
        layout_w: width === 'full' ? 12 : 6,
        layout_h: config.chartType === 'stat' ? 3 : 5,
      })
      const id = target.id
      toastSuccess(`Added to ${target.name}`, { action: { label: 'Open dashboard', onClick: () => pushDashboardDetail(id) } })
      onclose()
    } catch (e: unknown) {
      const message = e instanceof Error ? e.message : String(e)
      if (isNew) {
        // Keep the new dashboard: list it, select it, and offer a link, so a
        // retry adds the panel there instead of creating a second one.
        orphan = target
        dashboards = [target, ...dashboards]
        dashboardId = target.id
        formError = `The dashboard was created, but the panel could not be added: ${message}`
      } else {
        formError = message
      }
      toastError('Could not add the panel')
    } finally {
      saving = false
    }
  }
</script>

<Sheet {open} title="Add to dashboard" description="Saves this query and chart as a dashboard panel." size="sm" {onclose}>
  {#if loading && dashboards.length === 0}
    <div class="flex justify-center py-8"><Spinner size="sm" /></div>
  {:else}
    <div class="flex flex-col gap-4">
      <FormField label="Dashboard" for="atd-dashboard">
        <Combobox id="atd-dashboard" options={dashboardOptions} value={dashboardId} onChange={(v) => (dashboardId = v)} placeholder="Choose a dashboard" />
      </FormField>

      {#if isNew}
        <FormField label="Name" for="atd-name" required>
          <Input id="atd-name" bind:value={newName} placeholder="My dashboard" />
        </FormField>
        <FormField label="Folder" for="atd-folder">
          <Combobox id="atd-folder" options={folderOptions} value={folderId} onChange={(v) => (folderId = v)} placeholder="No folder" />
        </FormField>
      {/if}

      <FormField label="Panel name" for="atd-panel" required>
        <Input id="atd-panel" bind:value={panelName} />
      </FormField>

      <div>
        <span id="atd-width" class="mb-1 block text-xs font-medium text-fg-2">Width</span>
        <div class="grid grid-cols-2 gap-1" role="group" aria-labelledby="atd-width">
          {#each [{ value: 'half', label: 'Half' }, { value: 'full', label: 'Full' }] as const as opt (opt.value)}
            <button
              type="button"
              class="rounded border py-1.5 text-xs font-medium transition-colors {width === opt.value ? 'border-accent bg-accent-soft text-accent' : 'border-edge-subtle text-fg-3 hover:border-edge hover:text-fg'}"
              aria-pressed={width === opt.value}
              onclick={() => (width = opt.value)}
            >{opt.label}</button>
          {/each}
        </div>
      </div>

      {#if filtered}
        <p class="rounded-md bg-surface-2 px-3 py-2 text-[13px] text-fg-2">
          The chart shows filtered rows. The panel runs the unfiltered SQL.
        </p>
      {/if}

      {#if formError}
        <div class="rounded-md bg-danger-soft px-3 py-2 text-[13px] text-danger" role="alert">
          <p>{formError}</p>
          {#if orphan}
            {@const o = orphan}
            <button type="button" class="mt-1 text-[13px] text-fg underline" onclick={() => openDashboard(o.id)}>Open {o.name}</button>
          {/if}
        </div>
      {/if}
    </div>
  {/if}

  {#snippet footer()}
    <Button variant="secondary" size="sm" onclick={onclose}>Cancel</Button>
    <Button size="sm" loading={saving} disabled={loading} onclick={() => void handleAdd()}>Add</Button>
  {/snippet}
</Sheet>
