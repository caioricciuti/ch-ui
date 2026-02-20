import type { ColumnMeta } from '../types/query'
import { getDisplayType, type DisplayType } from './ch-types'

export interface ColumnStats {
  name: string
  type: string
  displayType: DisplayType
  count: number
  nulls: number
  nullPct: number
  // numeric
  min?: number
  max?: number
  avg?: number
  sum?: number
  // string
  minLen?: number
  maxLen?: number
  avgLen?: number
  distinct?: number
  // date
  earliest?: string
  latest?: string
}

const DISTINCT_SAMPLE = 10000

/** Compute per-column statistics in a single pass */
export function computeColumnStats(meta: ColumnMeta[], data: unknown[][]): ColumnStats[] {
  return meta.map((col, ci) => {
    const dt = getDisplayType(col.type)
    const total = data.length
    let nulls = 0

    if (dt === 'number') {
      let min = Infinity
      let max = -Infinity
      let sum = 0
      let numCount = 0

      for (let r = 0; r < total; r++) {
        const v = data[r][ci]
        if (v === null || v === undefined || v === '') { nulls++; continue }
        const n = Number(v)
        if (Number.isNaN(n)) { nulls++; continue }
        numCount++
        sum += n
        if (n < min) min = n
        if (n > max) max = n
      }

      return {
        name: col.name, type: col.type, displayType: dt,
        count: total, nulls, nullPct: total > 0 ? (nulls / total) * 100 : 0,
        min: numCount > 0 ? min : undefined,
        max: numCount > 0 ? max : undefined,
        avg: numCount > 0 ? sum / numCount : undefined,
        sum: numCount > 0 ? sum : undefined,
      }
    }

    if (dt === 'string') {
      let minLen = Infinity
      let maxLen = 0
      let totalLen = 0
      let strCount = 0
      const seen = new Set<string>()
      const sampleLimit = Math.min(total, DISTINCT_SAMPLE)

      for (let r = 0; r < total; r++) {
        const v = data[r][ci]
        if (v === null || v === undefined) { nulls++; continue }
        const s = String(v)
        strCount++
        totalLen += s.length
        if (s.length < minLen) minLen = s.length
        if (s.length > maxLen) maxLen = s.length
        if (r < sampleLimit) seen.add(s)
      }

      return {
        name: col.name, type: col.type, displayType: dt,
        count: total, nulls, nullPct: total > 0 ? (nulls / total) * 100 : 0,
        minLen: strCount > 0 ? minLen : undefined,
        maxLen: strCount > 0 ? maxLen : undefined,
        avgLen: strCount > 0 ? totalLen / strCount : undefined,
        distinct: strCount > 0 ? seen.size : undefined,
      }
    }

    if (dt === 'date') {
      let earliest = ''
      let latest = ''

      for (let r = 0; r < total; r++) {
        const v = data[r][ci]
        if (v === null || v === undefined || v === '') { nulls++; continue }
        const s = String(v)
        if (!earliest || s < earliest) earliest = s
        if (!latest || s > latest) latest = s
      }

      return {
        name: col.name, type: col.type, displayType: dt,
        count: total, nulls, nullPct: total > 0 ? (nulls / total) * 100 : 0,
        earliest: earliest || undefined,
        latest: latest || undefined,
      }
    }

    // bool / json / unknown — just count + nulls
    for (let r = 0; r < total; r++) {
      const v = data[r][ci]
      if (v === null || v === undefined) nulls++
    }

    return {
      name: col.name, type: col.type, displayType: dt,
      count: total, nulls, nullPct: total > 0 ? (nulls / total) * 100 : 0,
    }
  })
}
