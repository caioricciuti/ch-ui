import { describe, it, expect } from 'vitest'
import { RESULT_VIEWS, DEFAULT_RESULT_VIEW } from './result-views'
import footer from '../components/editor/ResultFooter.svelte?raw'

describe('result views', () => {
  it('offers Chart as the fifth view, right after Insights', () => {
    expect(RESULT_VIEWS.map((v) => v.id)).toEqual(['data', 'stats', 'schema', 'insights', 'chart'])
    expect(RESULT_VIEWS[4].label).toBe('Chart')
  })

  it('keeps Data as the default view', () => {
    expect(DEFAULT_RESULT_VIEW).toBe('data')
  })

  it('renders its tabs in ResultFooter from the shared list', () => {
    expect(footer).toContain('RESULT_VIEWS.map')
    expect(footer).toMatch(/chart:\s*ChartLine/)
  })
})
