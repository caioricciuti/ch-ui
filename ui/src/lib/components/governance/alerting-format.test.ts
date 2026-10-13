import { describe, expect, it } from 'vitest'
import type { HealthAlertState } from '../../types/alerts'
import {
  bytesToGb, conditionLabel, formatHealthValue, formatSeconds, gbToBytes, parseOptionalNumber, summarizeStates,
} from './alerting-format'

describe('formatSeconds', () => {
  it.each([
    [45, '45s'], [60, '1m'], [300, '5m'], [90, '2m'], [1800, '30m'], [3600, '1h'], [7200, '2h'], [5400, '1.5h'], [86400, '24h'],
  ])('%d -> %s', (n, want) => {
    expect(formatSeconds(n)).toBe(want)
  })
})

describe('formatHealthValue', () => {
  it('formats by unit', () => {
    expect(formatHealthValue(80, 'percent')).toBe('80%')
    expect(formatHealthValue(300, 'seconds')).toBe('5m')
    expect(formatHealthValue(3, 'count')).toBe('3')
    expect(formatHealthValue(1, 'down')).toBe('Down')
    expect(formatHealthValue(null, 'percent')).toBe('-')
  })
})

describe('conditionLabel', () => {
  it('reads like the condition', () => {
    expect(conditionLabel('value', 'gt', 100)).toBe('value > 100')
    expect(conditionLabel('rows', 'gte', 1)).toBe('rows >= 1')
    expect(conditionLabel('value', 'ne', 0)).toBe('value != 0')
  })
})

describe('summarizeStates', () => {
  const base: Omit<HealthAlertState, 'check_kind' | 'node' | 'state'> = {
    connection_id: 'c', severity: null, breach_count: 0, ok_count: 0, last_value: null, since: null, last_event_id: null, updated_at: null,
  }
  it('counts firing and pending per check', () => {
    const got = summarizeStates([
      { ...base, check_kind: 'disk', node: 'a', state: 'firing' },
      { ...base, check_kind: 'disk', node: 'b', state: 'firing' },
      { ...base, check_kind: 'disk', node: 'c', state: 'pending' },
      { ...base, check_kind: 'parts', node: 'a', state: 'pending' },
      { ...base, check_kind: 'parts', node: 'b', state: 'ok' },
    ])
    expect(got.get('disk')).toEqual({ firing: 2, pending: 1 })
    expect(got.get('parts')).toEqual({ firing: 0, pending: 1 })
    expect(got.has('replication_lag')).toBe(false)
  })
})

describe('numbers from forms', () => {
  it('treats empty as null', () => {
    expect(parseOptionalNumber('')).toBeNull()
    expect(parseOptionalNumber(null)).toBeNull()
    expect(parseOptionalNumber('  ')).toBeNull()
    expect(parseOptionalNumber('12.5')).toBe(12.5)
    expect(parseOptionalNumber(0)).toBe(0)
    expect(parseOptionalNumber('abc')).toBeNull()
  })
  it('round-trips GB', () => {
    expect(gbToBytes(10)).toBe(10 * 1024 ** 3)
    expect(bytesToGb(10 * 1024 ** 3)).toBe('10')
    expect(bytesToGb(null)).toBe('')
    expect(gbToBytes(null)).toBeNull()
  })
})
