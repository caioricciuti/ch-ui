import type uPlot from 'uplot'

/** The point a click landed on: data index and 1-based series index. */
export interface ChartPoint {
  idx: number
  series: number
}

export interface ClickOptions {
  /** Index under the cursor; the cursor's nearest x when not given (scatter passes its nearest point). */
  dataIndex?: (chart: uPlot) => number | null
  /** Bars stacked on draw: a series' segment sits on the series before it. */
  stacked?: boolean
}

/** A press that moved further than this is a drag (a zoom), not a click. */
const DRAG_PX = 4

/**
 * The visible series at index idx closest to the cursor's height. For a
 * stack, the segment whose middle is closest.
 */
export function pointAt(u: uPlot, options: ClickOptions = {}): ChartPoint | null {
  const { top } = u.cursor
  const idx = options.dataIndex ? options.dataIndex(u) : u.cursor.idx
  if (idx == null || top == null || top < 0) return null
  let best: ChartPoint | null = null
  let bestDist = Infinity
  let base = 0
  for (let s = 1; s < u.series.length; s++) {
    const raw = u.data[s]?.[idx]
    if (raw == null || !u.series[s].show) continue
    const v = Number(raw)
    const mid = options.stacked ? base + v / 2 : v
    if (options.stacked) base += v
    const dist = Math.abs(u.valToPos(mid, 'y') - top)
    if (dist < bestDist) {
      bestDist = dist
      best = { idx, series: s }
    }
  }
  return best
}

/**
 * uPlot has no click event: listen on the plot area and report the point
 * under the cursor. A drag (uPlot's zoom) is not a click. onclick is read on
 * every click, so the plugin can be built once per chart.
 */
export function clickPlugin(
  onclick: () => ((point: ChartPoint, chart: uPlot, e: MouseEvent) => void) | undefined,
  options: ClickOptions = {},
): uPlot.Plugin {
  function init(u: uPlot) {
    let downX = 0
    let downY = 0
    if (onclick()) u.over.style.cursor = 'pointer'
    u.over.addEventListener('mousedown', (e) => {
      downX = e.clientX
      downY = e.clientY
    })
    u.over.addEventListener('click', (e) => {
      const handler = onclick()
      if (!handler) return
      if (Math.abs(e.clientX - downX) > DRAG_PX || Math.abs(e.clientY - downY) > DRAG_PX) return
      const point = pointAt(u, options)
      if (point) handler(point, u, e)
    })
  }
  return { hooks: { init } }
}
