import { describe, expect, it } from 'vitest'
import type { AlertAck, FiringIncident } from '../../types/alerts'
import {
  ackTitle, durationPreset, hasMatcher, indexFiring, healthFiringKey, localInputToIso, silenceMatchSummary,
  silenceStatusLabel, sourceRefLabel, toLocalInput,
} from './alerting-silences'

const lookup = {
  connectionName: (id: string) => (id === 'c1' ? 'Prod' : undefined),
  sqlAlertName: (id: string) => (id === 's1' ? 'Queue depth' : undefined),
}

describe('silenceMatchSummary', () => {
  it('lists every matcher in order', () => {
    expect(silenceMatchSummary({ connection_id: 'c1', event_type: 'health.*', source_ref: 'health:disk', node: 'ch-1' }, lookup))
      .toBe('Prod, All health checks, Health: Disk usage, node ch-1')
  })
  it('names deleted connections, SQL alerts and the connection node', () => {
    expect(silenceMatchSummary({ connection_id: 'gone', event_type: null, source_ref: 's1', node: '*' }, lookup))
      .toBe('Deleted connection, Queue depth, node Connection')
  })
})

describe('sourceRefLabel', () => {
  it('falls back to the id for unknown SQL alerts', () => {
    expect(sourceRefLabel('x9', lookup.sqlAlertName)).toBe('SQL alert x9')
    expect(sourceRefLabel('health:connection_unreachable', lookup.sqlAlertName)).toBe('Health: Connection unreachable')
  })
})

describe('hasMatcher', () => {
  it('needs at least one', () => {
    expect(hasMatcher({ connection_id: null, event_type: null, source_ref: null, node: null })).toBe(false)
    expect(hasMatcher({ connection_id: null, event_type: null, source_ref: null, node: 'a' })).toBe(true)
  })
})

describe('local time conversion', () => {
  it('round-trips a datetime-local value through UTC', () => {
    const iso = localInputToIso('2026-10-13T09:30')
    expect(iso).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/)
    expect(toLocalInput(new Date(iso as string))).toBe('2026-10-13T09:30')
  })
  it('rejects empty and invalid input', () => {
    expect(localInputToIso('')).toBeNull()
    expect(localInputToIso('nope')).toBeNull()
  })
})

describe('durationPreset', () => {
  it('matches presets exactly', () => {
    expect(durationPreset('2026-10-13T09:00:00Z', '2026-10-13T13:00:00Z')).toBe('14400')
    expect(durationPreset('2026-10-13T09:00:00Z', '2026-10-13T09:45:00Z')).toBe('custom')
  })
})

describe('labels', () => {
  it('reads plainly', () => {
    expect(silenceStatusLabel('scheduled')).toBe('Scheduled')
    const ack: AlertAck = {
      id: 'a', alert_key: 'k', incident_id: null, event_id: 'e', connection_id: null, severity: null,
      comment: 'looking into it', acked_by: 'alice', created_at: '', expires_at: null,
    }
    expect(ackTitle(ack)).toBe('Acknowledged by alice: looking into it')
    expect(ackTitle({ ...ack, comment: null, acked_by: null })).toBe('Acknowledged')
  })
})

describe('indexFiring', () => {
  const base: FiringIncident = {
    source: 'health', alert_key: 'k', incident_id: 'i', connection_id: 'c1', check_kind: 'disk', node: 'ch-1',
    sql_alert_id: null, name: 'Disk', severity: 'critical', since: '', event_id: 'e', ack: null,
  }
  it('keys health by connection, check and node and SQL by alert id', () => {
    const idx = indexFiring([base, { ...base, source: 'sql', check_kind: null, node: null, sql_alert_id: 's1' }])
    expect(idx.health.get(healthFiringKey('c1', 'disk', 'ch-1'))?.incident_id).toBe('i')
    expect(idx.sql.has('s1')).toBe(true)
    expect(idx.health.size).toBe(1)
  })
})
