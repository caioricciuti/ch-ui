// SPDX-License-Identifier: BUSL-1.1
/**
 * Side-by-side line diff for SQL and DDL previews. A plain longest-common-subsequence
 * over lines: entity definitions are short, so the O(n*m) table is fine.
 */

export type DiffRowKind = 'same' | 'removed' | 'added' | 'changed'

export interface DiffRow {
  kind: DiffRowKind
  left: string | null
  right: string | null
}

function splitLines(text: string): string[] {
  return text === '' ? [] : text.split('\n')
}

export function lineDiff(current: string, proposed: string): DiffRow[] {
  const a = splitLines(current)
  const b = splitLines(proposed)
  const n = a.length
  const m = b.length
  // lcs[i][j] = LCS length of a[i..] and b[j..]
  const lcs: number[][] = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0))
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1])
    }
  }

  const raw: DiffRow[] = []
  let i = 0
  let j = 0
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      raw.push({ kind: 'same', left: a[i], right: b[j] })
      i++
      j++
    } else if (lcs[i + 1][j] >= lcs[i][j + 1]) {
      raw.push({ kind: 'removed', left: a[i], right: null })
      i++
    } else {
      raw.push({ kind: 'added', left: null, right: b[j] })
      j++
    }
  }
  while (i < n) raw.push({ kind: 'removed', left: a[i++], right: null })
  while (j < m) raw.push({ kind: 'added', left: null, right: b[j++] })

  // Pair a run of removals with the run of additions right after it, so a
  // modified line sits next to its replacement.
  const rows: DiffRow[] = []
  let k = 0
  while (k < raw.length) {
    if (raw[k].kind !== 'removed') {
      rows.push(raw[k++])
      continue
    }
    const removed: string[] = []
    while (k < raw.length && raw[k].kind === 'removed') removed.push(raw[k++].left ?? '')
    const added: string[] = []
    while (k < raw.length && raw[k].kind === 'added') added.push(raw[k++].right ?? '')
    const pairs = Math.max(removed.length, added.length)
    for (let p = 0; p < pairs; p++) {
      const left = p < removed.length ? removed[p] : null
      const right = p < added.length ? added[p] : null
      rows.push({ kind: left !== null && right !== null ? 'changed' : left !== null ? 'removed' : 'added', left, right })
    }
  }
  return rows
}
