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
  formatValue?: (value: number, seriesIdx: number) => string
}

export function tooltipPlugin(options: TooltipOptions): uPlot.Plugin {
  const formatValue = options.formatValue ?? ((v) => v.toLocaleString(undefined, { maximumFractionDigits: 2 }))
  let tip: HTMLDivElement

  function init(u: uPlot) {
    tip = document.createElement('div')
    tip.className = 'chart-tip'
    Object.assign(tip.style, {
      position: 'absolute',
      display: 'none',
      pointerEvents: 'none',
      zIndex: '100',
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
    u.over.appendChild(tip)
    u.over.addEventListener('mouseleave', () => (tip.style.display = 'none'))
  }

  function setCursor(u: uPlot) {
    const { idx, left, top } = u.cursor
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
      value.textContent = raw == null ? '—' : formatValue(Number(raw), i)
      row.append(dot, label, value)
      tip.appendChild(row)
    }

    const pad = 12
    const ow = u.over.clientWidth
    const tw = tip.offsetWidth
    const th = tip.offsetHeight
    let x = left + pad
    let y = top - th - pad
    if (x + tw > ow) x = left - tw - pad
    if (y < 0) y = top + pad
    tip.style.left = `${x}px`
    tip.style.top = `${y}px`
    tip.style.display = 'block'
  }

  return { hooks: { init, setCursor } }
}

/** Axis and grid colors read from the theme, so charts follow light/dark. */
export function chartTheme(el: HTMLElement): { axis: string; grid: string } {
  const css = getComputedStyle(el)
  return {
    axis: css.getPropertyValue('--fg-4').trim() || '#8a8a8a',
    grid: css.getPropertyValue('--edge-subtle').trim() || 'rgba(128,128,128,0.15)',
  }
}
