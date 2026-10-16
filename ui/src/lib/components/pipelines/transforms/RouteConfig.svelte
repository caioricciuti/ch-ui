<script lang="ts">
  import { ArrowDown, ArrowUp, Plus, X } from 'lucide-svelte'
  import { nextRouteId, readRoute, ROUTE_MAX, ROUTE_NAME_MAX, UNMATCHED_HANDLE, type RouteConfig, type RouteDef } from '../../../types/transforms'
  import Tabs from '../../common/Tabs.svelte'
  import ConfirmDialog from '../../common/ConfirmDialog.svelte'
  import RulesEditor from './RulesEditor.svelte'
  import { hintClass, iconButtonClass, inputClass, labelClass, smallButtonClass } from './ui'

  interface Props {
    config: Record<string, unknown>
    signal?: string | null
    /** Label of the node each connected output feeds, by handle. */
    connected?: Record<string, string>
    onchange: (config: Record<string, unknown>) => void
  }

  let { config, signal = null, connected = {}, onchange }: Props = $props()

  const c = $derived(readRoute(config))
  const fieldHint = $derived(signal === 'logs' ? 'SeverityText or LogAttributes.k' : 'field')

  /** A change that disconnects an output, waiting for a yes. */
  let pending = $state<{ title: string; description: string; next: RouteConfig } | null>(null)

  function emit(next: RouteConfig) {
    onchange({ ...config, ...next })
  }

  function setRoute(i: number, patch: Partial<RouteDef>) {
    emit({ ...c, routes: c.routes.map((r, j) => (j === i ? { ...r, ...patch } : r)) })
  }

  function move(i: number, by: number) {
    const j = i + by
    if (j < 0 || j >= c.routes.length) return
    const routes = [...c.routes]
    ;[routes[i], routes[j]] = [routes[j], routes[i]]
    emit({ ...c, routes })
  }

  function addRoute() {
    if (c.routes.length >= ROUTE_MAX) return
    const id = nextRouteId(c.routes)
    emit({ ...c, routes: [...c.routes, { id, name: `route ${c.routes.length + 1}`, match: 'all', rules: [] }] })
  }

  function removeRoute(i: number) {
    const r = c.routes[i]
    const next = { ...c, routes: c.routes.filter((_, j) => j !== i) }
    const target = connected[r.id]
    if (target) {
      const name = r.name.trim() || r.id
      pending = { title: `Remove route ${name}?`, description: `Its output feeds ${target}. Removing the route also removes that connection.`, next }
      return
    }
    emit(next)
  }

  function setUnmatched(v: 'drop' | 'output') {
    const next = { ...c, unmatched: v }
    const target = connected[UNMATCHED_HANDLE]
    if (v === 'drop' && target) {
      pending = { title: 'Drop unmatched records?', description: `The unmatched output feeds ${target}. Dropping unmatched records removes that connection.`, next }
      return
    }
    emit(next)
  }
</script>

<div class="space-y-3">
  <div>
    <p class={labelClass}>A record goes to</p>
    <Tabs
      variant="segmented"
      items={[{ id: 'first', label: 'First matching route' }, { id: 'all', label: 'Every matching route' }]}
      value={c.mode}
      onchange={(id) => emit({ ...c, mode: id === 'all' ? 'all' : 'first' })}
    />
    <p class={hintClass}>{c.mode === 'first' ? 'Routes are checked top to bottom; the first match wins.' : 'A record that matches several routes is copied to each of their sinks.'}</p>
  </div>

  <div>
    <p class={labelClass}>Routes</p>
    <div class="space-y-2">
      {#each c.routes as r, i (r.id)}
        <div class="space-y-2 rounded-md border border-edge-subtle p-2">
          <div class="flex items-center gap-1">
            <input
              class={inputClass}
              value={r.name}
              placeholder="route name"
              maxlength={ROUTE_NAME_MAX}
              aria-label="Route {i + 1} name"
              oninput={(e) => setRoute(i, { name: e.currentTarget.value })}
            />
            <button class={iconButtonClass} aria-label="Move route {i + 1} up" title="Move up" disabled={i === 0} onclick={() => move(i, -1)}>
              <ArrowUp size={13} />
            </button>
            <button class={iconButtonClass} aria-label="Move route {i + 1} down" title="Move down" disabled={i === c.routes.length - 1} onclick={() => move(i, 1)}>
              <ArrowDown size={13} />
            </button>
            <button class={iconButtonClass} aria-label="Remove route {i + 1}" title="Remove route" onclick={() => removeRoute(i)}>
              <X size={13} />
            </button>
          </div>
          <p class="text-[11px] text-fg-4">
            {connected[r.id] ? `Feeds ${connected[r.id]}` : 'Not connected; connect this output to a branch'}
          </p>
          <RulesEditor
            rules={r.rules}
            match={r.match}
            {fieldHint}
            onchange={(rules, match) => setRoute(i, { rules, match })}
          />
          {#if r.rules.length === 0}
            <p class={hintClass}>No rules: every record matches this route.</p>
          {/if}
        </div>
      {/each}
    </div>
    <div class="mt-2 flex items-center gap-2">
      <button class={smallButtonClass} onclick={addRoute} disabled={c.routes.length >= ROUTE_MAX}>
        <Plus size={12} /> Add route
      </button>
      {#if c.routes.length >= ROUTE_MAX}
        <span class="text-[11px] text-fg-4">At most {ROUTE_MAX} routes</span>
      {/if}
    </div>
  </div>

  <div>
    <p class={labelClass}>Records matching no route</p>
    <Tabs
      variant="segmented"
      items={[{ id: 'drop', label: 'Dropped' }, { id: 'output', label: 'Unmatched output' }]}
      value={c.unmatched}
      onchange={(id) => setUnmatched(id === 'output' ? 'output' : 'drop')}
    />
    <p class={hintClass}>
      {c.unmatched === 'drop'
        ? 'They are dropped and counted.'
        : connected[UNMATCHED_HANDLE]
          ? `They go to ${connected[UNMATCHED_HANDLE]}.`
          : 'They leave through the unmatched output; with nothing connected there they are dropped and counted.'}
    </p>
  </div>
</div>

<ConfirmDialog
  open={pending !== null}
  title={pending?.title ?? ''}
  description={pending?.description ?? ''}
  confirmLabel="Remove"
  destructive
  onconfirm={() => { if (pending) emit(pending.next); pending = null }}
  oncancel={() => { pending = null }}
/>
