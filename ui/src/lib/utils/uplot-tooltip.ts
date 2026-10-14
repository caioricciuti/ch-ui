import type uPlot from 'uplot'

/**
 * Floating tooltip for every uPlot chart in the app: one look, driven by
 * the theme tokens. Shows the x value as a header and one row per visible
 * series with its color dot, label and formatted value.
 */
export interface TooltipOptions {
  /** Header text for the hovered x index. */
  formatX: (xVal: number, idx: number) => string
  /** Value text for series `seriesIdx` (1-based, as in uPlot). */
  formatValue?: (value: number, seriesIdx: number, dataIdx: number) => string
  /** Histograms use interval containment instead of the nearest sample. */
  dataIndex?: (chart: uPlot) => number | null
  /** Leave out series with no value at the index (scatter groups). */
  skipNull?: boolean
}

/** Above every panel and the fullscreen panel overlay (z 9999). */
export const TOOLTIP_Z = 10000

/**
 * Where a floating tooltip goes for a pointer at (cx, cy), in viewport
 * coordinates: right of and above the pointer, flipped when that would leave
 * the window, then clamped inside it. Tooltips live on document.body with
 * position fixed, so no panel with overflow hidden can cut them off.
 */
export function tipPosition(cx: number, cy: number, tw: number, th: number, pad = 12): { left: number; top: number } {
  const margin = 4
  const vw = window.innerWidth
  const vh = window.innerHeight
  let left = cx + pad
  let top = cy - th - pad
  if (left + tw > vw - margin) left = cx - tw - pad
  if (top < margin) top = cy + pad
  left = Math.max(margin, Math.min(left, vw - tw - margin))
  top = Math.max(margin, Math.min(top, vh - th - margin))
  return { left, top }
}

/** Svelte action: moves the element to document.body for its lifetime. */
export function portal(node: HTMLElement): { destroy: () => void } {
  document.body.appendChild(node)
  return { destroy: () => node.remove() }
}

export function tooltipPlugin(options: TooltipOptions): uPlot.Plugin {
  const formatValue = options.formatValue ?? ((v) => v.toLocaleString(undefined, { maximumFractionDigits: 2 }))
  let tip: HTMLDivElement
  const hide = () => {
    if (tip) tip.style.display = 'none'
  }

  function init(u: uPlot) {
    tip = document.createElement('div')
    tip.className = 'chart-tip'
    Object.assign(tip.style, {
      position: 'fixed',
      display: 'none',
      pointerEvents: 'none',
      zIndex: String(TOOLTIP_Z),
      whiteSpace: 'nowrap',
      background: 'var(--elevated)',
      color: 'var(--fg)',
      border: '1px solid var(--edge)',
      borderRadius: '6px',
      boxShadow: 'var(--shadow-popover)',
      padding: '6px 9px',
      font: '11px var(--font-sans)',
      lineHeight: '1.5',
    })
    document.body.appendChild(tip)
    u.over.addEventListener('mouseleave', hide)
    window.addEventListener('scroll', hide, true)
  }

  function destroy() {
    window.removeEventListener('scroll', hide, true)
    tip?.remove()
  }

  function setCursor(u: uPlot) {
    const { left, top } = u.cursor
    const idx = options.dataIndex ? options.dataIndex(u) : u.cursor.idx
    if (idx == null || left == null || top == null || left < 0 || top < 0) {
      tip.style.display = 'none'
      return
    }

    tip.textContent = ''
    const head = document.createElement('div')
    Object.assign(head.style, { fontWeight: '600', marginBottom: '2px', color: 'var(--fg-2)' })
    head.textContent = options.formatX(u.data[0][idx] as number, idx)
    tip.appendChild(head)

    for (let i = 1; i < u.series.length; i++) {
      const s = u.series[i]
      if (!s.show) continue
      const raw = u.data[i][idx]
      if (raw == null && options.skipNull) continue
      const stroke = typeof s.stroke === 'function' ? (s.stroke as (u: uPlot, i: number) => string)(u, i) : s.stroke

      const row = document.createElement('div')
      Object.assign(row.style, { display: 'flex', alignItems: 'center', gap: '6px' })
      const dot = document.createElement('span')
      Object.assign(dot.style, { width: '7px', height: '7px', borderRadius: '2px', background: String(stroke ?? ''), flexShrink: '0' })
      const label = document.createElement('span')
      Object.assign(label.style, { color: 'var(--fg-3)', flex: '1' })
      label.textContent = String(s.label ?? '')
      const value = document.createElement('span')
      Object.assign(value.style, { fontWeight: '600', marginLeft: '12px', fontVariantNumeric: 'tabular-nums' })
      value.textContent = raw == null ? '—' : formatValue(Number(raw), i, idx)
      row.append(dot, label, value)
      tip.appendChild(row)
    }

    // Measure while visible but before placing, then place in the viewport.
    tip.style.visibility = 'hidden'
    tip.style.display = 'block'
    const rect = u.over.getBoundingClientRect()
    const pos = tipPosition(rect.left + left, rect.top + top, tip.offsetWidth, tip.offsetHeight)
    tip.style.left = `${pos.left}px`
    tip.style.top = `${pos.top}px`
    tip.style.visibility = ''
  }

  return { hooks: { init, setCursor, destroy } }
}

/** Axis and grid colors read from the theme, so charts follow light/dark. */
export function chartTheme(el: HTMLElement): { axis: string; grid: string } {
  const css = getComputedStyle(el)
  return {
    axis: css.getPropertyValue('--fg-4').trim() || '#8a8a8a',
    grid: css.getPropertyValue('--edge-subtle').trim() || 'rgba(128,128,128,0.15)',
  }
}
