// SPDX-License-Identifier: BUSL-1.1
import { describe, expect, it } from 'vitest'
import { EVENT_TYPE_OPTIONS, eventTypeDescription, eventTypeLabel } from './alerting-silences'
import { CONNECTOR_FIELDS } from '../../types/pipelines'

describe('ingest alert event types', () => {
  it('lists every ingest and pipeline type with a description', () => {
    for (const t of ['ingest.stalled', 'ingest.spool_backlog', 'ingest.rejecting', 'ingest.auth_failures', 'pipeline.sink_failing']) {
      expect(EVENT_TYPE_OPTIONS.some((o) => o.value === t)).toBe(true)
      expect(eventTypeDescription(t)).not.toBe('')
    }
    expect(eventTypeLabel('ingest.*')).toBe('All ingest events')
    expect(eventTypeLabel('pipeline.*')).toBe('All pipeline events')
    expect(eventTypeDescription('*')).toBe('')
  })

  it('offers the stall threshold on push sources only', () => {
    for (const type of ['source_ingest', 'source_webhook'] as const) {
      const f = CONNECTOR_FIELDS[type].find((x) => x.key === 'stall_after_minutes')
      expect(f?.default).toBe(15)
      expect(f?.min).toBe(0)
    }
    expect(CONNECTOR_FIELDS.source_kafka.some((x) => x.key === 'stall_after_minutes')).toBe(false)
  })
})
