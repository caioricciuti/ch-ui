import { describe, expect, it } from 'vitest'
import { cellCopyText, inspectCellText } from './cell-inspect'

describe('inspectCellText', () => {
  it('pretty-prints arrays, maps and JSON strings', () => {
    expect(inspectCellText([1, 2])).toEqual({ text: '[\n  1,\n  2\n]', json: true })
    expect(inspectCellText({ a: 1 })).toEqual({ text: '{\n  "a": 1\n}', json: true })
    expect(inspectCellText('{"b":[true]}').text).toBe('{\n  "b": [\n    true\n  ]\n}')
  })

  it('leaves other values alone', () => {
    expect(inspectCellText('{not json}')).toEqual({ text: '{not json}', json: false })
    expect(inspectCellText(42)).toEqual({ text: '42', json: false })
    expect(inspectCellText(null)).toEqual({ text: 'NULL', json: false })
  })
})

describe('cellCopyText', () => {
  it('copies compact JSON for objects and nothing for NULL', () => {
    expect(cellCopyText({ a: [1] })).toBe('{"a":[1]}')
    expect(cellCopyText(null)).toBe('')
    expect(cellCopyText('x')).toBe('x')
  })
})
