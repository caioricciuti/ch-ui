import { describe, expect, it } from 'vitest'
import { CONNECTOR_FIELDS, generateToken, newNodeConfig } from './pipelines'

describe('newNodeConfig', () => {
  it('turns webhook auth on with a fresh token', () => {
    const a = newNodeConfig('source_webhook', 'p1')
    const b = newNodeConfig('source_webhook', 'p1')
    expect(a.auth_enabled).toBe(true)
    expect(String(a.auth_token)).toMatch(/^[0-9a-f]{48}$/)
    expect(a.auth_token).not.toBe(b.auth_token)
  })

  it('gives Kafka a per-pipeline consumer group', () => {
    expect(newNodeConfig('source_kafka', 'abc')).toEqual({ consumer_group: 'ch-ui-abc' })
  })

  it('leaves other nodes empty', () => {
    expect(newNodeConfig('sink_clickhouse', 'abc')).toEqual({})
  })
})

describe('connector fields', () => {
  it('defaults the database type to postgres', () => {
    expect(CONNECTOR_FIELDS.source_database.find((f) => f.key === 'db_type')?.default).toBe('postgres')
  })

  it('sets a minimum on every interval and batch field', () => {
    for (const fields of Object.values(CONNECTOR_FIELDS)) {
      for (const f of fields) {
        if (['batch_size', 'batch_timeout_ms', 'poll_interval'].includes(f.key)) {
          expect(f.min, f.key).toBeGreaterThan(0)
        }
      }
    }
  })

  it('generates 24-byte hex tokens', () => {
    expect(generateToken()).toHaveLength(48)
  })
})
