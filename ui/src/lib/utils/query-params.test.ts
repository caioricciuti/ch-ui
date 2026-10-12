import { describe, it, expect } from 'vitest'
import { detectQueryParams } from './query-params'

describe('detectQueryParams', () => {
  it('returns nothing for an empty or parameterless query', () => {
    expect(detectQueryParams('')).toEqual([])
    expect(detectQueryParams('SELECT 1')).toEqual([])
  })

  it('extracts name and type, ignoring whitespace', () => {
    expect(detectQueryParams('SELECT * FROM t WHERE id = { id : UInt64 }')).toEqual([
      { name: 'id', type: 'UInt64' },
    ])
  })

  it('keeps parametric types intact', () => {
    expect(detectQueryParams('SELECT {ids:Array(UInt32)}, {amount:Decimal(10, 2)}')).toEqual([
      { name: 'ids', type: 'Array(UInt32)' },
      { name: 'amount', type: 'Decimal(10, 2)' },
    ])
  })

  it('returns each name once, first type wins, in first-seen order', () => {
    expect(detectQueryParams('SELECT {b:String}, {a:Int8}, {b:UInt8}')).toEqual([
      { name: 'b', type: 'String' },
      { name: 'a', type: 'Int8' },
    ])
  })

  it('does not match identifiers that start with a digit', () => {
    expect(detectQueryParams("SELECT '{1x:String}'")).toEqual([])
  })

  it('gives the same answer on repeated calls (regex state is reset)', () => {
    const sql = 'SELECT {x:UInt8}'
    expect(detectQueryParams(sql)).toEqual(detectQueryParams(sql))
  })
})
