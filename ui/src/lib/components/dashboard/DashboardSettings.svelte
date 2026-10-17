<script lang="ts">
  import { untrack } from 'svelte'
  import type { Dashboard, DashboardVariable, Panel } from '../../types/api'
  import { apiPost, apiPut } from '../../api/client'
  import { duplicateDashboard, type DashboardDetail } from '../../api/dashboards'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import Sheet from '../common/Sheet.svelte'
  import Button from '../common/Button.svelte'
  import Tabs from '../common/Tabs.svelte'
  import FormField from '../common/FormField.svelte'
  import Input from '../common/Input.svelte'
  import Textarea from '../common/Textarea.svelte'
  import VariablesEditor from './VariablesEditor.svelte'
  import VersionHistory from './VersionHistory.svelte'
  import { Download, Upload, Save, Copy, AlertTriangle, Trash2 } from 'lucide-svelte'

  interface Props {
    open: boolean
    dashboard: Dashboard
    panels: Panel[]
    variables: DashboardVariable[]
    onclose: () => void
    onimported: (dashboard: Dashboard, panels: Panel[], variables?: DashboardVariable[]) => void
    onvariableschange: (variables: DashboardVariable[]) => void
    onduplicated: (detail: DashboardDetail) => void
    ondelete: () => void
  }

  let { open, dashboard, panels, variables, onclose, onimported, onvariableschange, onduplicated, ondelete }: Props = $props()

  type Tab = 'general' | 'variables' | 'json' | 'export' | 'history'
  const tabs: { id: Tab; label: string }[] = [
    { id: 'general', label: 'General' },
    { id: 'variables', label: 'Variables' },
    { id: 'json', label: 'JSON Editor' },
    { id: 'export', label: 'Export / Import' },
    { id: 'history', label: 'History' },
  ]

  let tab = $state<Tab>('general')
  let jsonText = $state('')
  /** The editor holds user edits or a loaded file; do not overwrite them. */
  let jsonDirty = $state(false)
  let jsonError = $state<string | null>(null)
  let saving = $state(false)
  let savingGeneral = $state(false)
  let duplicating = $state(false)
  let importing = $state(false)
  let fileInput = $state<HTMLInputElement>(undefined!)
  let generalName = $state('')
  let generalDescription = $state('')

  function buildExportPayload(): Record<string, unknown> {
    return {
      version: 1,
      dashboard: {
        name: dashboard.name,
        description: dashboard.description,
      },
      variables,
      panels: panels.map(p => ({
        name: p.name,
        description: p.description,
        panel_type: p.panel_type,
        query: p.query,
        config: safeParseJson(p.config),
        layout_x: p.layout_x,
        layout_y: p.layout_y,
        layout_w: p.layout_w,
        layout_h: p.layout_h,
      })),
    }
  }

  function safeParseJson(s: string): unknown {
    try { return JSON.parse(s) } catch { return {} }
  }

  function resetJson() {
    jsonText = JSON.stringify(buildExportPayload(), null, 2)
    jsonDirty = false
    jsonError = null
  }

  // Reset only when the sheet opens. Saving variables changes the props
  // while the sheet is open, which must not bounce the user to General.
  $effect(() => {
    if (open) {
      untrack(() => {
        resetJson()
        generalName = dashboard.name
        generalDescription = dashboard.description ?? ''
        tab = 'general'
      })
    }
  })

  function selectTab(id: string) {
    tab = id as Tab
    if (tab === 'json' && !jsonDirty) resetJson()
  }

  function validateJson(): boolean {
    try {
      const parsed = JSON.parse(jsonText)
      if (!parsed.dashboard || !Array.isArray(parsed.panels)) {
        jsonError = 'JSON must have "dashboard" and "panels" fields'
        return false
      }
      if (parsed.variables !== undefined && !Array.isArray(parsed.variables)) {
        jsonError = '"variables" must be an array when present'
        return false
      }
      jsonError = null
      return true
    } catch (e: unknown) {
      jsonError = 'Invalid JSON: ' + (e instanceof Error ? e.message : String(e))
      return false
    }
  }

  /**
   * The server validates every panel first and swaps them in one
   * transaction, so a bad file leaves the dashboard exactly as it was.
   * A file without "variables" keeps the current ones.
   */
  async function applyJson() {
    if (!validateJson()) return
    saving = true
    try {
      const parsed: unknown = JSON.parse(jsonText)
      const res = await apiPost<Partial<DashboardDetail>>(
        `/api/dashboards/${dashboard.id}/import`,
        parsed,
      )
      toastSuccess('Dashboard config applied')
      jsonDirty = false
      onimported({ ...dashboard, ...res.dashboard }, res.panels ?? [], res.variables)
    } catch (e: unknown) {
      jsonError = e instanceof Error ? e.message : String(e)
      toastError('Failed to apply: ' + jsonError)
    } finally {
      saving = false
    }
  }

  function handleExport() {
    const blob = new Blob([JSON.stringify(buildExportPayload(), null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${dashboard.name.replace(/[^a-zA-Z0-9_-]/g, '_')}.json`
    a.click()
    URL.revokeObjectURL(url)
    toastSuccess('Dashboard exported')
  }

  function handleCopyJson() {
    navigator.clipboard.writeText(JSON.stringify(buildExportPayload(), null, 2))
    toastSuccess('Copied to clipboard')
  }

  function handleCopyEditor() {
    navigator.clipboard.writeText(jsonText)
    toastSuccess('Copied to clipboard')
  }

  function triggerImport() {
    fileInput?.click()
  }

  async function handleFileImport(e: Event) {
    const file = (e.target as HTMLInputElement).files?.[0]
    if (!file) return
    importing = true
    try {
      const text = await file.text()
      const parsed = JSON.parse(text)
      if (!parsed.dashboard || !Array.isArray(parsed.panels)) {
        toastError('Invalid dashboard file: missing "dashboard" or "panels"')
        return
      }
      jsonText = JSON.stringify(parsed, null, 2)
      jsonDirty = true
      jsonError = null
      tab = 'json'
      toastSuccess('File loaded. Review it and click Apply to import.')
    } catch (e: unknown) {
      toastError('Failed to read file: ' + (e instanceof Error ? e.message : String(e)))
    } finally {
      importing = false
      if (fileInput) fileInput.value = ''
    }
  }

  async function saveGeneral() {
    if (!generalName.trim()) {
      toastError('Name is required')
      return
    }
    savingGeneral = true
    try {
      await apiPut(`/api/dashboards/${dashboard.id}`, {
        name: generalName.trim(),
        description: generalDescription.trim() || null,
      })
      const updated = {
        ...dashboard,
        name: generalName.trim(),
        description: generalDescription.trim() || null,
      }
      toastSuccess('Dashboard updated')
      onimported(updated, panels)
    } catch (e: unknown) {
      toastError('Failed to save: ' + (e instanceof Error ? e.message : String(e)))
    } finally {
      savingGeneral = false
    }
  }

  async function handleDuplicate() {
    duplicating = true
    try {
      const detail = await duplicateDashboard(dashboard.id)
      toastSuccess(`Duplicated as "${detail.dashboard.name}"`)
      onduplicated(detail)
    } catch (e: unknown) {
      toastError('Failed to duplicate: ' + (e instanceof Error ? e.message : String(e)))
    } finally {
      duplicating = false
    }
  }
</script>

<Sheet {open} title="Dashboard Settings" size="lg" {onclose}>
  <div class="flex flex-col gap-4 h-full">
    <Tabs items={tabs} value={tab} onchange={selectTab} size="sm" />

    {#if tab === 'general'}
      <div class="flex flex-col gap-4">
        <FormField label="Name" for="dashboard-name" required controlWidth="full">
          <Input id="dashboard-name" placeholder="Dashboard name" bind:value={generalName} />
        </FormField>
        <FormField label="Description" for="dashboard-description" controlWidth="full">
          <Textarea id="dashboard-description" rows={3} placeholder="Optional description" bind:value={generalDescription} />
        </FormField>
        <div class="mt-4 rounded-lg border border-edge-subtle p-4">
          <h3 class="text-[13px] font-medium text-fg mb-1">Duplicate Dashboard</h3>
          <p class="text-xs text-fg-3 mb-3">Copy this dashboard with its panels and variables into the same folder, with the same tags.</p>
          <Button size="sm" variant="secondary" loading={duplicating} onclick={handleDuplicate}>
            <Copy size={13} /> Duplicate dashboard
          </Button>
        </div>

        <!-- Danger zone -->
        <div class="mt-4 rounded-lg border border-danger/40 p-4">
          <h3 class="text-[13px] font-medium text-danger mb-1">Danger Zone</h3>
          <p class="text-xs text-fg-3 mb-3">Permanently delete this dashboard and all its panels. This action cannot be undone.</p>
          <Button size="sm" variant="danger" onclick={ondelete}>
            <Trash2 size={13} /> Delete Dashboard
          </Button>
        </div>
      </div>

    {:else if tab === 'variables'}
      <VariablesEditor dashboardId={dashboard.id} {variables} onsaved={(v: DashboardVariable[]) => onvariableschange(v)} />

    {:else if tab === 'json'}
      <div class="flex flex-col gap-3 flex-1 min-h-0">
        <p class="text-xs text-fg-3">Edit the full dashboard configuration as JSON, variables included. Changes are applied when you click Apply.</p>

        {#if jsonError}
          <div class="flex items-center gap-2 px-3 py-2 text-xs text-danger bg-danger-soft rounded-md">
            <AlertTriangle size={14} />
            {jsonError}
          </div>
        {/if}

        <textarea
          class="flex-1 min-h-[300px] w-full font-mono text-xs bg-canvas border border-edge rounded-lg p-3 text-fg resize-none focus:outline-none focus:border-accent"
          bind:value={jsonText}
          oninput={() => { jsonError = null; jsonDirty = true }}
          spellcheck="false"
        ></textarea>
      </div>

    {:else if tab === 'export'}
      <div class="flex flex-col gap-6">
        <!-- Export -->
        <div class="rounded-lg border border-edge-subtle p-4">
          <h3 class="text-[13px] font-medium text-fg mb-1">Export Dashboard</h3>
          <p class="text-xs text-fg-3">Download the full dashboard configuration, including all panels and variables, as a JSON file, or copy it to the clipboard. Both actions are below.</p>
        </div>

        <!-- Import -->
        <div class="rounded-lg border border-edge-subtle p-4">
          <h3 class="text-[13px] font-medium text-fg mb-1">Import Dashboard</h3>
          <p class="text-xs text-fg-3 mb-3">Upload a dashboard JSON file. This replaces all current panels, and the variables when the file has a "variables" field.</p>
          <div class="flex items-center gap-2">
            <Button size="sm" variant="secondary" loading={importing} onclick={triggerImport}>
              <Upload size={13} /> Upload JSON File
            </Button>
            <span class="text-[11px] text-fg-4">Loads into the JSON editor for review before applying</span>
          </div>
          <input
            bind:this={fileInput}
            type="file"
            accept=".json,application/json"
            class="hidden"
            onchange={handleFileImport}
          />
        </div>

        <!-- Info -->
        <div class="rounded-lg bg-surface border border-edge-subtle p-4">
          <h3 class="text-[13px] font-medium text-fg mb-2">JSON Format</h3>
          <pre class="text-[11px] text-fg-3 font-mono leading-relaxed">{"{"}
  "version": 1,
  "dashboard": {"{"} "name": "...", "description": "..." {"}"},
  "variables": [
    {"{"} "name": "db", "label": "Database", "type": "query|custom|text|constant|interval",
      "query": "SELECT name FROM system.databases", "options": "a,b,c",
      "multi": false, "include_all": false, "default": ["..."] {"}"}
  ],
  "panels": [
    {"{"} "name": "...", "panel_type": "table|stat|gauge|timeseries|bar|pie|text",
      "query": "SELECT ... WHERE database = $db", "config": {"{"} ... {"}"},
      "layout_x": 0, "layout_y": 0, "layout_w": 6, "layout_h": 4 {"}"}
  ]
{"}"}</pre>
          <p class="mt-2 text-[11px] text-fg-4">"variables" is optional on import; leave it out to keep the current variables.</p>
        </div>
      </div>

    {:else}
      <VersionHistory
        dashboardId={dashboard.id}
        onrestored={(d) => onimported({ ...dashboard, ...d.dashboard }, d.panels, d.variables)}
      />
    {/if}
  </div>
  {#snippet footer()}
    {#if tab === 'general'}
      <Button variant="ghost" size="sm" onclick={onclose} disabled={savingGeneral}>Cancel</Button>
      <Button size="sm" loading={savingGeneral} onclick={saveGeneral}>
        <Save size={13} /> Save
      </Button>
    {:else if tab === 'json'}
      <Button variant="ghost" size="sm" onclick={onclose} disabled={saving}>Cancel</Button>
      {#if jsonDirty}
        <Button variant="ghost" size="sm" onclick={resetJson} disabled={saving}>Discard edits</Button>
      {/if}
      <Button variant="secondary" size="sm" onclick={handleCopyEditor}>
        <Copy size={13} /> Copy
      </Button>
      <Button size="sm" loading={saving} onclick={applyJson}>
        <Save size={13} /> Apply Changes
      </Button>
    {:else if tab === 'export'}
      <Button variant="ghost" size="sm" onclick={onclose}>Cancel</Button>
      <Button variant="secondary" size="sm" onclick={handleCopyJson}>
        <Copy size={13} /> Copy to Clipboard
      </Button>
      <Button size="sm" onclick={handleExport}>
        <Download size={13} /> Download JSON
      </Button>
    {:else}
      <Button variant="ghost" size="sm" onclick={onclose}>Close</Button>
    {/if}
  {/snippet}
</Sheet>
