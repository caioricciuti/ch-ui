import { describe, expect, it } from 'vitest'
import { CONNECTOR_FIELDS, createTableDDL, generateToken, newNodeConfig, quoteIdent, schemaColumnsOf } from './pipelines'

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

  it('starts an ingest source on logs with the contract defaults', () => {
    expect(newNodeConfig('source_ingest', 'abc')).toEqual({ signal: 'logs', ttl_days: 30, rate_limit_rps: 0, daily_quota_mb: 0 })
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

describe('createTableDDL', () => {
  it('quotes identifiers like the server', () => {
    expect(quoteIdent('a`b\\c')).toBe('`a\\`b\\\\c`')
  })

  it('builds the statement the sink runs', () => {
    const ddl = createTableDDL({
      database: 'db',
      table: 'events',
      columns: [{ name: 'ts', type: 'DateTime64(3)' }, { name: 'id', type: 'Int64' }],
      engine: 'MergeTree',
      orderBy: '(`ts`, `id`)',
      partitionBy: 'toYYYYMM(`ts`)',
    })
    expect(ddl).toBe('CREATE TABLE IF NOT EXISTS `db`.`events` (\n  `ts` DateTime64(3),\n  `id` Int64\n) ENGINE = MergeTree\nPARTITION BY toYYYYMM(`ts`)\nORDER BY (`ts`, `id`)\nSETTINGS non_replicated_deduplication_window = 1000')
  })

  it('defaults ORDER BY to tuple() and skips an empty PARTITION BY', () => {
    const ddl = createTableDDL({ database: 'd', table: 't', columns: [{ name: 'a', type: 'String' }] })
    expect(ddl.endsWith(') ENGINE = MergeTree\nORDER BY tuple()\nSETTINGS non_replicated_deduplication_window = 1000')).toBe(true)
  })

  it('leaves the dedup window to Replicated and Shared engines', () => {
    for (const engine of ['ReplicatedMergeTree', 'SharedMergeTree']) {
      expect(createTableDDL({ database: 'd', table: 't', columns: [{ name: 'a', type: 'String' }], engine })).not.toContain('SETTINGS')
    }
    expect(createTableDDL({ database: 'd', table: 't', columns: [{ name: 'a', type: 'String' }], engine: 'ReplacingMergeTree()' })).toContain('SETTINGS non_replicated_deduplication_window = 1000')
  })
})

describe('schemaColumnsOf', () => {
  it('reads saved columns and ignores junk', () => {
    expect(schemaColumnsOf({ create_table_columns: [{ name: 'a', type: 'String' }, { name: 1 }, null] })).toEqual([{ name: 'a', type: 'String' }])
    expect(schemaColumnsOf({ create_table_columns: null })).toEqual([])
  })
})

describe('sink fields', () => {
  it('has the dead-letter toggle on by default and a fractional error ratio', () => {
    const dl = CONNECTOR_FIELDS.sink_clickhouse.find((f) => f.key === 'dead_letter')
    const ratio = CONNECTOR_FIELDS.sink_clickhouse.find((f) => f.key === 'max_error_ratio')
    expect(dl?.default).toBe(true)
    expect(ratio?.decimal).toBe(true)
    expect(ratio?.max).toBe(1)
  })
})
