// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import type uPlot from 'uplot'
import { tipPosition, tooltipPlugin } from './uplot-tooltip'
import { logsHistogramData } from '../components/telemetry/histogram'

describe('histogram tooltip', () => {
  it('shows the hovered bucket raw counts under their matching severity labels and colors', () => {
    const plot = logsHistogramData([
      { t: '2026-09-21T07:02:30Z', counts: { INFO: 37 } },
      { t: '2026-09-21T07:02:40Z', counts: { WARN: 1 } },
    ], 10)
    const over = document.createElement('div')
    const chart = {
      over,
      cursor: { idx: 0, left: 10, top: 20 },
      data: [plot.x, ...plot.series.map((s) => s.stacked)],
      series: [{ label: 'Time' }, ...plot.series.map((s) => ({ show: true, label: s.label, stroke: s.label === 'WARN' ? '#eab308' : '#f97316' }))],
    } as unknown as uPlot
    let dataIndex: number | null = 0
    const plugin = tooltipPlugin({
      dataIndex: () => dataIndex,
      formatX: (_value, idx) => String(idx),
      formatValue: (_value, seriesIdx, dataIdx) => String(plot.series[seriesIdx - 1].counts[dataIdx]),
    })
    const call = (hook: 'init' | 'setCursor') => {
      const hooks = plugin.hooks![hook]! as ((chart: uPlot) => void) | Array<(chart: uPlot) => void>
      for (const handler of Array.isArray(hooks) ? hooks : [hooks]) handler(chart)
    }
    call('init')
    call('setCursor')
    const values = () => [...document.querySelector('.chart-tip')!.children].slice(1).map((row) => row.textContent)
    expect(values()).toEqual(['WARN0', 'INFO37'])
    expect((document.querySelector('.chart-tip')!.children[1].firstChild as HTMLElement).style.background).toBe('rgb(234, 179, 8)')
    // The nearest-point cursor still points at the first bucket. The
    // containing-interval hook must determine both heading and values.
    dataIndex = 1
    call('setCursor')
    expect(values()).toEqual(['WARN1', 'INFO0'])
    dataIndex = null
    call('setCursor')
    expect((document.querySelector('.chart-tip') as HTMLElement).style.display).toBe('none')
  })
})

describe('tipPosition', () => {
  it('sits above-right of the pointer and stays inside the window at every edge', () => {
    Object.assign(window, { innerWidth: 800, innerHeight: 600 })
    expect(tipPosition(100, 300, 120, 50)).toEqual({ left: 112, top: 238 })
    // Right edge: flips to the left of the pointer.
    expect(tipPosition(780, 300, 120, 50).left).toBe(648)
    // Top edge: flips below the pointer.
    expect(tipPosition(100, 20, 120, 50).top).toBe(32)
    // Bottom edge of a short panel near the window bottom: still fully visible.
    const p = tipPosition(100, 598, 120, 80)
    expect(p.top + 80).toBeLessThanOrEqual(596)
  })
})
