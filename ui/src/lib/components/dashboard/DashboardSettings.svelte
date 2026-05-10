<script lang="ts">
  import type { Dashboard, Panel } from '../../types/api'
  import { apiPost, apiPut, apiDel } from '../../api/client'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import Sheet from '../common/Sheet.svelte'
  import Button from '../common/Button.svelte'
  import { Download, Upload, Save, Copy, AlertTriangle } from 'lucide-svelte'

  interface Props {
    open: boolean
    dashboard: Dashboard
    panels: Panel[]
    onclose: () => void
    onimported: (dashboard: Dashboard, panels: Panel[]) => void
  }

  let { open, dashboard, panels, onclose, onimported }: Props = $props()

  let tab = $state<'json' | 'export'>('json')
  let jsonText = $state('')
  let jsonError = $state<string | null>(null)
  let saving = $state(false)
  let importing = $state(false)
  let fileInput = $state<HTMLInputElement>(undefined!)

  function buildExportPayload(): Record<string, unknown> {
    return {
      version: 1,
      dashboard: {
        name: dashboard.name,
        description: dashboard.description,
      },
      panels: panels.map(p => ({
        name: p.name,
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

  $effect(() => {
    if (open) {
      jsonText = JSON.stringify(buildExportPayload(), null, 2)
      jsonError = null
      tab = 'json'
    }
  })

  function validateJson(): boolean {
    try {
      const parsed = JSON.parse(jsonText)
      if (!parsed.dashboard || !Array.isArray(parsed.panels)) {
        jsonError = 'JSON must have "dashboard" and "panels" fields'
        return false
      }
      jsonError = null
      return true
    } catch (e: any) {
      jsonError = 'Invalid JSON: ' + e.message
      return false
    }
  }

  async function applyJson() {
    if (!validateJson()) return
    saving = true
    try {
      const parsed = JSON.parse(jsonText)

      await apiPut(`/api/dashboards/${dashboard.id}`, {
        name: parsed.dashboard.name ?? dashboard.name,
        description: parsed.dashboard.description ?? dashboard.description,
      })

      for (const p of panels) {
        await apiDel(`/api/dashboards/${dashboard.id}/panels/${p.id}`)
      }

      const newPanels: Panel[] = []
      for (const p of parsed.panels) {
        const res = await apiPost<{ panel: Panel }>(`/api/dashboards/${dashboard.id}/panels`, {
          name: p.name,
          panel_type: p.panel_type,
          query: p.query,
          config: typeof p.config === 'string' ? p.config : JSON.stringify(p.config ?? {}),
          layout_x: p.layout_x ?? 0,
          layout_y: p.layout_y ?? 0,
          layout_w: p.layout_w ?? 6,
          layout_h: p.layout_h ?? 4,
        })
        if (res.panel) newPanels.push(res.panel)
      }

      const updatedDashboard = {
        ...dashboard,
        name: parsed.dashboard.name ?? dashboard.name,
        description: parsed.dashboard.description ?? dashboard.description,
      }

      toastSuccess('Dashboard config applied')
      onimported(updatedDashboard, newPanels)
    } catch (e: any) {
      toastError('Failed to apply: ' + e.message)
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
      jsonError = null
      tab = 'json'
      toastSuccess('File loaded — review and click Apply to import')
    } catch (e: any) {
      toastError('Failed to read file: ' + e.message)
    } finally {
      importing = false
      if (fileInput) fileInput.value = ''
    }
  }
</script>

<Sheet {open} title="Dashboard Settings" size="lg" {onclose}>
  <div class="flex flex-col gap-4 h-full">
    <!-- Tabs -->
    <div class="flex border-b border-gray-200 dark:border-gray-700">
      <button
        class="px-4 py-2 text-xs font-medium transition-colors
          {tab === 'json'
            ? 'text-ch-blue border-b-2 border-ch-blue'
            : 'text-gray-400 hover:text-gray-600 dark:hover:text-gray-300'}"
        onclick={() => tab = 'json'}
      >JSON Editor</button>
      <button
        class="px-4 py-2 text-xs font-medium transition-colors
          {tab === 'export'
            ? 'text-ch-blue border-b-2 border-ch-blue'
            : 'text-gray-400 hover:text-gray-600 dark:hover:text-gray-300'}"
        onclick={() => tab = 'export'}
      >Export / Import</button>
    </div>

    {#if tab === 'json'}
      <div class="flex flex-col gap-3 flex-1 min-h-0">
        <div class="flex items-center justify-between">
          <p class="text-xs text-gray-500">Edit the full dashboard configuration as JSON. Changes are applied when you click Apply.</p>
        </div>

        {#if jsonError}
          <div class="flex items-center gap-2 px-3 py-2 text-xs text-red-600 bg-red-50 dark:bg-red-900/20 rounded-lg">
            <AlertTriangle size={14} />
            {jsonError}
          </div>
        {/if}

        <textarea
          class="flex-1 min-h-[300px] w-full font-mono text-xs bg-gray-50 dark:bg-gray-950 border border-gray-300 dark:border-gray-700 rounded-lg p-3 text-gray-800 dark:text-gray-200 resize-none focus:outline-none focus:ring-2 focus:ring-ch-blue/40 focus:border-ch-blue"
          bind:value={jsonText}
          oninput={() => jsonError = null}
          spellcheck="false"
        ></textarea>

        <div class="flex items-center justify-end gap-2">
          <Button variant="secondary" size="sm" onclick={handleCopyJson}>
            <Copy size={13} /> Copy
          </Button>
          <Button size="sm" loading={saving} onclick={applyJson}>
            <Save size={13} /> Apply Changes
          </Button>
        </div>
      </div>

    {:else}
      <div class="flex flex-col gap-6">
        <!-- Export -->
        <div class="rounded-lg border border-gray-200 dark:border-gray-800 p-4">
          <h3 class="text-sm font-medium text-gray-800 dark:text-gray-200 mb-1">Export Dashboard</h3>
          <p class="text-xs text-gray-500 mb-3">Download the full dashboard configuration including all panels as a JSON file.</p>
          <div class="flex gap-2">
            <Button size="sm" onclick={handleExport}>
              <Download size={13} /> Download JSON
            </Button>
            <Button variant="secondary" size="sm" onclick={handleCopyJson}>
              <Copy size={13} /> Copy to Clipboard
            </Button>
          </div>
        </div>

        <!-- Import -->
        <div class="rounded-lg border border-gray-200 dark:border-gray-800 p-4">
          <h3 class="text-sm font-medium text-gray-800 dark:text-gray-200 mb-1">Import Dashboard</h3>
          <p class="text-xs text-gray-500 mb-3">Upload a dashboard JSON file. This will replace all current panels.</p>
          <div class="flex items-center gap-2">
            <Button size="sm" variant="secondary" loading={importing} onclick={triggerImport}>
              <Upload size={13} /> Upload JSON File
            </Button>
            <span class="text-[11px] text-gray-400">Loads into the JSON editor for review before applying</span>
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
        <div class="rounded-lg bg-gray-50 dark:bg-gray-900/50 border border-gray-200 dark:border-gray-800 p-4">
          <h3 class="text-sm font-medium text-gray-800 dark:text-gray-200 mb-2">JSON Format</h3>
          <pre class="text-[11px] text-gray-500 font-mono leading-relaxed">{"{"}
  "version": 1,
  "dashboard": {"{"} "name": "...", "description": "..." {"}"},
  "panels": [
    {"{"} "name": "...", "panel_type": "stat|table|timeseries|bar",
      "query": "SELECT ...", "config": {"{"} ... {"}"},
      "layout_x": 0, "layout_y": 0, "layout_w": 6, "layout_h": 4 {"}"}
  ]
{"}"}</pre>
        </div>
      </div>
    {/if}
  </div>
</Sheet>
