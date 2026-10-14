// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import type uPlot from 'uplot'
import { clickPlugin, pointAt, type ChartPoint } from './uplot-click'

// A stand-in chart: y values map to pixels as 100 - y, so y 80 is 20 px down.
function fakeChart(idx: number | null, top: number, data: (number | null)[][], show = data.map(() => true)) {
  const over = document.createElement('div')
  return {
    over,
    cursor: { idx, left: 10, top },
    data: [data[0].map((_, i) => i), ...data],
    series: [{ label: 'x' }, ...show.map((s, i) => ({ show: s, label: 's' + i }))],
    valToPos: (v: number) => 100 - v,
  } as unknown as uPlot
}

function mouse(type: string, x: number, y: number): MouseEvent {
  return new MouseEvent(type, { clientX: x, clientY: y, bubbles: true })
}

describe('uPlot click', () => {
  it('reports the cursor index and the nearest series on a click', () => {
    const chart = fakeChart(2, 20, [[1, 2, 10], [5, 6, 80]])
    const clicks: ChartPoint[] = []
    const plugin = clickPlugin(() => (point) => clicks.push(point))
    const init = plugin.hooks.init as (u: uPlot) => void
    init(chart)
    expect(chart.over.style.cursor).toBe('pointer')

    chart.over.dispatchEvent(mouse('mousedown', 50, 50))
    chart.over.dispatchEvent(mouse('click', 51, 50))
    expect(clicks).toEqual([{ idx: 2, series: 2 }])

    // A drag is a zoom, not a click.
    chart.over.dispatchEvent(mouse('mousedown', 50, 50))
    chart.over.dispatchEvent(mouse('click', 90, 50))
    expect(clicks).toHaveLength(1)
  })

  it('does nothing without a handler and leaves the cursor alone', () => {
    const chart = fakeChart(0, 20, [[1]])
    const plugin = clickPlugin(() => undefined)
    ;(plugin.hooks.init as (u: uPlot) => void)(chart)
    expect(chart.over.style.cursor).toBe('')
    chart.over.dispatchEvent(mouse('click', 0, 0))
  })

  it('skips hidden and empty series, and uses a custom index', () => {
    expect(pointAt(fakeChart(0, 20, [[80], [null], [10]], [false, true, true]))).toEqual({ idx: 0, series: 3 })
    expect(pointAt(fakeChart(null, 20, [[80]]))).toBeNull()
    expect(pointAt(fakeChart(0, 20, [[80, 1]]), { dataIndex: () => 1 })).toEqual({ idx: 1, series: 1 })
  })

  it('picks the stacked segment under the cursor', () => {
    // Segments 0..40 and 40..90: the cursor at y 60 is in the second.
    const chart = fakeChart(0, 40, [[40], [50]])
    expect(pointAt(chart, { stacked: true })).toEqual({ idx: 0, series: 2 })
    expect(pointAt(fakeChart(0, 85, [[40], [50]]), { stacked: true })).toEqual({ idx: 0, series: 1 })
  })
})
