import { describe, it, expect } from 'vitest'
import { parseCHError, byteToCharOffset } from './ch-error'

describe('byteToCharOffset', () => {
  it('is identity for ASCII', () => {
    expect(byteToCharOffset('SELECT 1', 7)).toBe(7)
  })

  it('accounts for multi-byte UTF-8 characters', () => {
    // 'é' is 2 bytes in UTF-8 but 1 UTF-16 code unit
    expect(byteToCharOffset("SELECT 'café', x", 15)).toBe(14)
  })

  it('clamps out-of-range offsets', () => {
    expect(byteToCharOffset('abc', 100)).toBe(3)
    expect(byteToCharOffset('abc', -1)).toBe(0)
  })
})

describe('parseCHError', () => {
  it('parses a syntax error with position, token, line and col (quoted format, <= 25.1)', () => {
    const raw =
      "Code: 62. DB::Exception: Syntax error: failed at position 47 ('FRM') (line 2, col 10): FRM events LIMIT 10. Expected one of: FROM, PREWHERE, WHERE. (SYNTAX_ERROR) (version 24.8.4.13 (official build))"
    const p = parseCHError(raw)
    expect(p.code).toBe(62)
    expect(p.name).toBe('SYNTAX_ERROR')
    expect(p.position).toBe(47)
    expect(p.positionInQuery).toBe(true)
    expect(p.token).toBe('FRM')
    expect(p.line).toBe(2)
    expect(p.col).toBe(10)
    expect(p.hint).toContain('syntax')
    expect(p.message).toContain('Syntax error')
    expect(p.message).not.toContain('version 24.8')
    expect(p.message).not.toContain('(SYNTAX_ERROR)')
  })

  it('parses the unquoted token format (>= 25.2)', () => {
    const raw =
      'Code: 62. DB::Exception: Syntax error: failed at position 9 (FRM) (line 1, col 9): FRM events. (SYNTAX_ERROR) (version 25.8.1.1)'
    const p = parseCHError(raw)
    expect(p.position).toBe(9)
    expect(p.positionInQuery).toBe(true)
    expect(p.token).toBe('FRM')
    expect(p.line).toBe(1)
    expect(p.col).toBe(9)
  })

  it('treats truncated token previews as unselectable but keeps position and line/col', () => {
    const raw =
      'Code: 62. DB::Exception: Syntax error: failed at position 5 (SELEC...) (line 1, col 5): rest. (SYNTAX_ERROR)'
    const p = parseCHError(raw)
    expect(p.position).toBe(5)
    expect(p.token).toBeNull()
    expect(p.line).toBe(1)
    expect(p.col).toBe(5)
  })

  it('handles the (end of query) variant and still captures line/col', () => {
    const raw =
      'Code: 62. DB::Exception: Syntax error: failed at position 16 (end of query) (line 3, col 5): . Expected one of: FROM. (SYNTAX_ERROR)'
    const p = parseCHError(raw)
    expect(p.position).toBe(16)
    expect(p.token).toBeNull()
    expect(p.line).toBe(3)
    expect(p.col).toBe(5)
  })

  it('flags sub-parse positions (data type, table structure) as not query-mappable', () => {
    const raw =
      "Code: 62. DB::Exception: Syntax error (data type): failed at position 5 ('Type'): Bad Type. (SYNTAX_ERROR)"
    const p = parseCHError(raw)
    expect(p.position).toBe(5)
    expect(p.positionInQuery).toBe(false)
  })

  it('flags positions relayed from remote shards as not query-mappable', () => {
    const raw =
      "Code: 62. DB::Exception: Received from shard-2:9000. DB::Exception: Syntax error: failed at position 30 ('x'): bad. (SYNTAX_ERROR)"
    const p = parseCHError(raw)
    expect(p.position).toBe(30)
    expect(p.positionInQuery).toBe(false)
  })

  it('recognizes DB::ParsingException and DB::NetException segments', () => {
    const p = parseCHError(
      'ClickHouse error: Code: 27. DB::ParsingException: Cannot parse input: expected \\t before: abc. (CANNOT_PARSE_INPUT_ASSERTION_FAILED)'
    )
    expect(p.message).not.toContain('ParsingException')
    expect(p.message).toContain('Cannot parse input')

    const n = parseCHError('Code: 210. DB::NetException: Connection refused (localhost:9000). (NETWORK_ERROR)')
    expect(n.message).not.toContain('NetException')
    expect(n.message).toContain('Connection refused')
  })

  it('recognizes single-word error names ABORTED and UNFINISHED', () => {
    const p = parseCHError(
      'Code: 341. DB::Exception: Distributed DDL task is not finished on 1 of 3 hosts. (UNFINISHED) (version 24.8.4.13 (official build))'
    )
    expect(p.name).toBe('UNFINISHED')
    expect(p.message).not.toContain('(UNFINISHED)')

    const a = parseCHError('Code: 236. DB::Exception: Cancelled mutating parts. (ABORTED)')
    expect(a.name).toBe('ABORTED')
    expect(a.message).not.toContain('(ABORTED)')
  })

  it('parses through connector/tunnel prefixes', () => {
    const raw =
      'ClickHouse error: Code: 60. DB::Exception: Table default.evnts does not exist. Maybe you meant default.events? (UNKNOWN_TABLE) (version 24.3.1)'
    const p = parseCHError(raw)
    expect(p.code).toBe(60)
    expect(p.name).toBe('UNKNOWN_TABLE')
    expect(p.position).toBeNull()
    expect(p.message).toContain('Maybe you meant default.events?')
    expect(p.message).not.toContain('ClickHouse error:')
  })

  it('uses the most specific message for nested distributed exceptions', () => {
    const raw =
      'Code: 60. DB::Exception: Received from ch-node-2:9000. DB::Exception: Table default.x does not exist. (UNKNOWN_TABLE) (version 24.1.1)'
    const p = parseCHError(raw)
    expect(p.message).toBe('Table default.x does not exist.')
  })

  it('does not mistake type or format names for error names', () => {
    const raw =
      "Code: 6. DB::Exception: Cannot parse string 'abc' as (UInt64): syntax error at begin of string. (CANNOT_PARSE_TEXT) (version 24.2)"
    const p = parseCHError(raw)
    expect(p.name).toBe('CANNOT_PARSE_TEXT')
  })

  it('strips stack traces from the message', () => {
    const raw =
      'Code: 241. DB::Exception: Memory limit (for query) exceeded: would use 9.32 GiB. Stack trace:\n0. DB::Exception::Exception()\n1. ...'
    const p = parseCHError(raw)
    expect(p.code).toBe(241)
    expect(p.message).not.toContain('Stack trace')
    expect(p.hint).toContain('memory')
  })

  it('handles position without token or line info', () => {
    const raw = 'Code: 62. DB::Exception: Syntax error: failed at position 12: bad. (SYNTAX_ERROR)'
    const p = parseCHError(raw)
    expect(p.position).toBe(12)
    expect(p.token).toBeNull()
    expect(p.line).toBeNull()
  })

  it('falls back gracefully for non-ClickHouse errors', () => {
    const raw = 'request failed: dial tcp 127.0.0.1:8123: connect: connection refused'
    const p = parseCHError(raw)
    expect(p.code).toBeNull()
    expect(p.name).toBeNull()
    expect(p.position).toBeNull()
    expect(p.hint).toBeNull()
    expect(p.message).toBe(raw)
  })

  it('returns no hint for unknown codes', () => {
    const p = parseCHError('Code: 999. DB::Exception: Something odd. (WHATEVER_ODD)')
    expect(p.code).toBe(999)
    expect(p.hint).toBeNull()
  })
})
