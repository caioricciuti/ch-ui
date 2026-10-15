<script lang="ts">
  import { onDestroy } from 'svelte'
  import {
    SvelteFlow,
    Controls,
    Background,
    MiniMap,
    Panel,
    MarkerType,
    type Edge,
    type Node,
    type NodeTypes,
  } from '@xyflow/svelte'
  import '@xyflow/svelte/dist/style.css'
  import { getTheme } from '../../stores/theme.svelte'
  import { openQueryTab, openTableTab } from '../../stores/tabs.svelte'
  import { goWorkspace } from '../../stores/router.svelte'
  import { formatBytes } from '../../utils/format'
  import { KIND_LABELS, compactCount, NODE_HEIGHT, NODE_WIDTH, type LayoutEdge, type LayoutNode } from '../../utils/lineage-layout'
  import type { LineageNode } from '../../types/lineage'
  import LineageNodeCard, { type LineageCardData } from './LineageNodeCard.svelte'
  import LineageNodeSheet from './LineageNodeSheet.svelte'
  import LineageViewport from './LineageViewport.svelte'

  interface Props {
    nodes: LayoutNode[]
    edges: LayoutEdge[]
    /** Pan to this node; bump seq to pan again to the same one. */
    center?: { id: string; seq: number } | null
    onfocus: (id: string) => void
    /** Clicked a collapsed database: show its nodes. */
    onexpand: (database: string) => void
  }

  let { nodes, edges, center = null, onfocus, onexpand }: Props = $props()

  // Edge labels are DOM nodes; past this many edges the legend explains the styles instead.
  const LABEL_LIMIT = 200

  const nodeTypes: NodeTypes = {
    lineage: LineageNodeCard as unknown as NodeTypes[string],
  }

  let selectedId = $state<string | null>(null)
  const byId = $derived(new Map(nodes.map((n) => [n.id, n])))
  const selected = $derived<LineageNode | null>((selectedId && byId.get(selectedId)?.node) || null)
  const selectedIsFocus = $derived(!!selectedId && !!byId.get(selectedId)?.focus)

  function stats(n: LineageNode): string {
    const parts: string[] = []
    if (n.rows !== null) parts.push(`${compactCount(n.rows)} rows`)
    if (n.bytes !== null) parts.push(formatBytes(n.bytes))
    return parts.join(' · ')
  }

  function cardData(n: LayoutNode): LineageCardData {
    if (n.group) {
      return {
        name: n.group.database,
        sub: 'Click to expand',
        kindLabel: `${n.group.count} ${n.group.count === 1 ? 'object' : 'objects'}`,
        kind: null,
        stats: '',
        focus: false,
        selected: false,
      }
    }
    const node = n.node!
    return {
      name: node.name,
      // External ids carry the descriptor as their name; engine tables keep their database.
      sub: node.database === '' ? 'Outside ClickHouse' : node.external ? `${node.database} · ${node.external}` : node.database,
      kindLabel: KIND_LABELS[node.kind],
      kind: node.kind,
      stats: stats(node),
      focus: n.focus,
      selected: n.id === selectedId,
    }
  }

  const flowNodes = $derived<Node[]>(
    nodes.map((n) => ({
      id: n.id,
      type: 'lineage',
      position: { x: n.x, y: n.y },
      width: NODE_WIDTH,
      height: NODE_HEIGHT,
      draggable: false,
      connectable: false,
      data: cardData(n),
    })),
  )

  const STROKE = 'var(--fg-4)'
  const BACK_STROKE = 'var(--warning)'

  const flowEdges = $derived.by<Edge[]>(() => {
    const labels = edges.length <= LABEL_LIMIT
    return edges.map((e) => {
      const color = e.back ? BACK_STROKE : STROKE
      const dash = e.confidence === 'parsed' ? ' stroke-dasharray: 5 4;' : ''
      const parts: string[] = []
      if (e.confidence === 'parsed') parts.push('parsed')
      if (e.back) parts.push('cycle')
      if (e.count > 1) parts.push(`${e.count} links`)
      return {
        id: e.id,
        source: e.from,
        target: e.to,
        selectable: false,
        focusable: false,
        style: `stroke: ${color}; stroke-width: ${e.back ? 1.5 : 1.25};${dash}`,
        markerEnd: { type: MarkerType.ArrowClosed, color, width: 16, height: 16 },
        label: labels && parts.length > 0 ? parts.join(' · ') : undefined,
        labelStyle: 'font-size: 10px; padding: 0 4px; border-radius: 3px;',
      }
    })
  })

  // A first click waits a moment in case it is the start of a double click;
  // the sheet's scrim would otherwise swallow the second click.
  const DOUBLE_CLICK_MS = 240
  let clickTimer: ReturnType<typeof setTimeout> | undefined

  function handleClick(id: string, event: MouseEvent | TouchEvent) {
    const n = byId.get(id)
    if (!n) return
    if (n.group) {
      onexpand(n.group.database)
      return
    }
    if (!n.node || n.node.kind === 'hidden') return
    const clicks = event instanceof MouseEvent ? event.detail : 1
    if (clickTimer) clearTimeout(clickTimer)
    if (clicks >= 2) {
      clickTimer = undefined
      openTable(n.node)
      return
    }
    clickTimer = setTimeout(() => {
      clickTimer = undefined
      selectedId = id
    }, DOUBLE_CLICK_MS)
  }

  function canOpen(n: LineageNode): boolean {
    return n.database !== '' && n.kind !== 'missing' && n.kind !== 'hidden'
  }

  function openTable(n: LineageNode) {
    if (!canOpen(n)) return
    selectedId = null
    openTableTab(n.database, n.name)
    goWorkspace()
  }

  function quoteIdent(s: string): string {
    return '`' + s.replace(/\\/g, '\\\\').replace(/`/g, '\\`') + '`'
  }

  function openEditor(n: LineageNode) {
    if (!canOpen(n)) return
    selectedId = null
    openQueryTab(`SELECT *\nFROM ${quoteIdent(n.database)}.${quoteIdent(n.name)}\nLIMIT 100`)
    goWorkspace()
  }

  function focusHere(id: string) {
    selectedId = null
    onfocus(id)
  }

  onDestroy(() => {
    if (clickTimer) clearTimeout(clickTimer)
  })

  // xyflow theming through its own variables, pointed at our tokens.
  const flowStyle = [
    'background-color: var(--canvas)',
    '--xy-background-color: var(--canvas)',
    '--xy-edge-label-background-color: var(--elevated)',
    '--xy-edge-label-color: var(--fg-3)',
    '--xy-minimap-background-color: var(--surface)',
    '--xy-minimap-mask-background-color: var(--hover)',
    '--xy-controls-button-background-color: var(--elevated)',
    '--xy-controls-button-background-color-hover: var(--hover)',
    '--xy-controls-button-color: var(--fg-2)',
    '--xy-controls-button-color-hover: var(--fg)',
    '--xy-controls-button-border-color: var(--edge)',
    '--xy-controls-box-shadow: none',
  ].join('; ')
</script>

<div class="h-full w-full">
  <SvelteFlow
    nodes={flowNodes}
    edges={flowEdges}
    {nodeTypes}
    fitView
    fitViewOptions={{ padding: 0.15, maxZoom: 1.25 }}
    minZoom={0.1}
    maxZoom={2}
    nodesDraggable={false}
    nodesConnectable={false}
    elementsSelectable={false}
    zoomOnDoubleClick={false}
    onlyRenderVisibleElements
    colorMode={getTheme() === 'dark' ? 'dark' : 'light'}
    proOptions={{ hideAttribution: true }}
    style={flowStyle}
    onnodeclick={({ node, event }) => handleClick(node.id, event)}
  >
    <Background gap={18} patternColor="var(--edge-strong)" />
    <Controls showLock={false} />
    <MiniMap
      pannable
      zoomable
      nodeColor={(n) => (n.id === selectedId ? 'var(--fg-3)' : 'var(--edge-strong)')}
      bgColor="var(--surface)"
      maskColor="var(--hover)"
      class="!rounded-md !border !border-edge"
    />
    <Panel position="top-left">
      <div class="flex items-center gap-3 rounded-md border border-edge-subtle bg-elevated px-2.5 py-1.5 text-[11px] text-fg-3">
        <span class="flex items-center gap-1.5"><svg width="22" height="6" aria-hidden="true"><line x1="0" y1="3" x2="22" y2="3" style="stroke: var(--fg-4); stroke-width: 1.5" /></svg>From metadata</span>
        <span class="flex items-center gap-1.5"><svg width="22" height="6" aria-hidden="true"><line x1="0" y1="3" x2="22" y2="3" style="stroke: var(--fg-4); stroke-width: 1.5; stroke-dasharray: 5 4" /></svg>Parsed from SQL</span>
        <span class="flex items-center gap-1.5"><svg width="22" height="6" aria-hidden="true"><line x1="0" y1="3" x2="22" y2="3" style="stroke: var(--warning); stroke-width: 1.5" /></svg>Closes a cycle</span>
      </div>
    </Panel>
    <LineageViewport {nodes} {center} />
  </SvelteFlow>
</div>

<LineageNodeSheet
  node={selected}
  isFocus={selectedIsFocus}
  onclose={() => (selectedId = null)}
  onfocus={focusHere}
  onopentable={openTable}
  onopeneditor={openEditor}
/>
