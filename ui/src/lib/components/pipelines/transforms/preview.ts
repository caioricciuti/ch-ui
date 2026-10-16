// Pure helpers for the transform preview: sample parsing, record alignment and field diffs.

import { PREVIEW_MAX_RECORDS, UNMATCHED_HANDLE, type PreviewResponse, type PreviewStep, type TransformNodeType } from '../../../types/transforms'

type Rec = Record<string, unknown>

/** Parses pasted sample records: one JSON object per line (a JSON array also works). */
export function parseSampleRecords(text: string): { records: Rec[]; error: string } {
  const t = text.trim()
  if (!t) return { records: [], error: 'Paste at least one JSON record' }
  if (t.startsWith('[')) {
    try {
      const arr: unknown = JSON.parse(t)
      if (!Array.isArray(arr)) return { records: [], error: 'Expected a JSON array of objects' }
      return checked(arr, (i) => `Item ${i + 1}`)
    } catch (e) {
      return { records: [], error: `Not valid JSON: ${e instanceof Error ? e.message : String(e)}` }
    }
  }
  const items: unknown[] = []
  const lines = t.split('\n')
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].trim()
    if (!line) continue
    try {
      items.push(JSON.parse(line))
    } catch (e) {
      return { records: [], error: `Line ${i + 1} is not valid JSON: ${e instanceof Error ? e.message : String(e)}` }
    }
  }
  return checked(items, (i) => `Record ${i + 1}`)
}

function checked(items: unknown[], name: (i: number) => string): { records: Rec[]; error: string } {
  if (items.length > PREVIEW_MAX_RECORDS) return { records: [], error: `At most ${PREVIEW_MAX_RECORDS} records; got ${items.length}` }
  for (let i = 0; i < items.length; i++) {
    const it = items[i]
    if (!it || typeof it !== 'object' || Array.isArray(it)) return { records: [], error: `${name(i)} is not a JSON object` }
  }
  return { records: items as Rec[], error: '' }
}

/** Flattens nested objects into dot paths; arrays and scalars are leaves. */
export function flatten(rec: Rec, prefix = '', out: Record<string, unknown> = {}): Record<string, unknown> {
  for (const [k, v] of Object.entries(rec)) {
    const path = prefix ? `${prefix}.${k}` : k
    if (v && typeof v === 'object' && !Array.isArray(v) && Object.keys(v).length > 0) flatten(v as Rec, path, out)
    else out[path] = v
  }
  return out
}

function same(a: unknown, b: unknown): boolean {
  if (a === b) return true
  return JSON.stringify(a) === JSON.stringify(b)
}

/** How much two records agree: equal leaf fields. */
function similarity(a: Record<string, unknown>, b: Record<string, unknown>): number {
  let n = 0
  for (const [k, v] of Object.entries(a)) if (k in b && same(v, b[k])) n++
  return n
}

/**
 * For each output record, the index of the input record it came from.
 * Uses the server's `kept` when present. Otherwise transforms keep order and
 * only drop, so outputs are a subsequence of inputs: pick the order-preserving
 * matching with the most equal fields.
 */
export function alignOutputs(input: Rec[], output: Rec[], kept?: number[] | null, dropped?: number[] | null): number[] {
  if (kept && kept.length === output.length) return kept
  // With the dropped indexes known, the survivors are the rest, in order.
  if (dropped) {
    const gone = new Set(dropped)
    const rest = input.map((_, i) => i).filter((i) => !gone.has(i))
    if (rest.length === output.length) return rest
  }
  if (output.length === input.length) return output.map((_, i) => i)
  if (output.length > input.length) return output.map((_, i) => (i < input.length ? i : -1))
  const n = input.length
  const m = output.length
  const fi = input.map((r) => flatten(r))
  const fo = output.map((r) => flatten(r))
  // best[i][j]: best score matching the first j outputs within the first i inputs.
  const NEG = -1
  const best: number[][] = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(NEG))
  for (let i = 0; i <= n; i++) best[i][0] = 0
  for (let i = 1; i <= n; i++) {
    for (let j = 1; j <= Math.min(i, m); j++) {
      const skip = best[i - 1][j]
      const take = best[i - 1][j - 1] === NEG ? NEG : best[i - 1][j - 1] + similarity(fi[i - 1], fo[j - 1]) + 1
      best[i][j] = Math.max(skip, take)
    }
  }
  const out = new Array<number>(m).fill(-1)
  let i = n
  let j = m
  while (j > 0 && i > 0) {
    const take = best[i - 1][j - 1] === NEG ? NEG : best[i - 1][j - 1] + similarity(fi[i - 1], fo[j - 1]) + 1
    if (best[i][j] === take && take !== NEG) {
      out[j - 1] = i - 1
      j--
    }
    i--
  }
  return out
}

export type FieldChange = 'same' | 'changed' | 'added' | 'removed'

export interface FieldDiff {
  path: string
  before: unknown
  after: unknown
  change: FieldChange
}

/** Field-level diff of two records, in the output's field order, removed fields last. */
export function diffRecords(before: Rec, after: Rec): FieldDiff[] {
  const b = flatten(before)
  const a = flatten(after)
  const out: FieldDiff[] = []
  for (const [k, v] of Object.entries(a)) {
    if (!(k in b)) out.push({ path: k, before: undefined, after: v, change: 'added' })
    else out.push({ path: k, before: b[k], after: v, change: same(b[k], v) ? 'same' : 'changed' })
  }
  for (const [k, v] of Object.entries(b)) if (!(k in a)) out.push({ path: k, before: v, after: undefined, change: 'removed' })
  return out
}

export interface PreviewRow {
  /** Index in this step's input. */
  index: number
  dropped: boolean
  /** Why it was dropped, or the parse error that was kept. */
  reason: string
  error: string
  fields: FieldDiff[]
}

function defaultDropReason(type: TransformNodeType | undefined): string {
  switch (type) {
    case 'transform_parse': return 'Parse failed (on error: drop)'
    case 'transform_filter': return 'Filtered out'
    case 'transform_sample': return 'Not sampled'
    default: return 'Dropped'
  }
}

/** The records entering a step: the request input, or the previous step's output. */
export function stepInput(res: PreviewResponse, stepIndex: number): Rec[] {
  return stepIndex <= 0 ? res.input : (res.steps[stepIndex - 1]?.output ?? [])
}

/** One row per input record of the step: kept with a field diff, or dropped with a reason. */
export function buildRows(input: Rec[], step: PreviewStep, nodeType?: TransformNodeType): PreviewRow[] {
  const map = alignOutputs(input, step.output, step.kept, step.drops ? step.drops.map((d) => d.index) : null)
  const outFor = new Map<number, Rec>()
  map.forEach((inIdx, outIdx) => { if (inIdx >= 0) outFor.set(inIdx, step.output[outIdx]) })
  const errors = new Map((step.errors ?? []).map((e) => [e.index, e.message]))
  const reasons = new Map((step.drops ?? []).map((d) => [d.index, d.reason]))
  return input.map((rec, index) => {
    const out = outFor.get(index)
    const error = errors.get(index) ?? ''
    if (out) return { index, dropped: false, reason: '', error, fields: diffRecords(rec, out) }
    const reason = reasons.get(index) || (error ? `Parse failed: ${error}` : defaultDropReason(nodeType))
    return { index, dropped: true, reason, error, fields: diffRecords(rec, rec) }
  })
}

/** Short text for a cell value. */
export function valueText(v: unknown): string {
  if (v === undefined) return ''
  if (v === null) return 'null'
  if (typeof v === 'string') return v
  return JSON.stringify(v)
}

export interface RouteRow {
  /** Index in the route step's input. */
  index: number
  /** Output handles the record went to, in output order. */
  handles: string[]
  /** It matched no route. */
  unmatched: boolean
  record: Rec
}

/**
 * One row per record entering a route step: the outputs it went to, or
 * unmatched. Records missing from every output and from the unmatched list
 * count as unmatched too.
 */
export function buildRouteRows(input: Rec[], step: PreviewStep, order: string[]): RouteRow[] {
  const rank = (h: string) => {
    const i = order.indexOf(h)
    return i < 0 ? order.length : i
  }
  const to = new Map<number, string[]>()
  for (const r of step.routes ?? []) {
    for (const i of r.kept ?? []) {
      const list = to.get(i) ?? []
      if (!list.includes(r.handle)) list.push(r.handle)
      to.set(i, list)
    }
  }
  return input.map((record, index) => {
    const handles = (to.get(index) ?? []).filter((h) => h !== UNMATCHED_HANDLE).sort((a, b) => rank(a) - rank(b))
    return { index, handles, unmatched: handles.length === 0, record }
  })
}

/** Records per output (and unmatched) in a route preview. */
export function routeTotals(rows: RouteRow[]): { perHandle: Record<string, number>; unmatched: number } {
  const perHandle: Record<string, number> = {}
  let unmatched = 0
  for (const r of rows) {
    if (r.unmatched) unmatched++
    for (const h of r.handles) perHandle[h] = (perHandle[h] ?? 0) + 1
  }
  return { perHandle, unmatched }
}
