// A small ClickHouse SQL lexer: just enough to know which characters are
// code and which sit inside a string, a quoted identifier, a heredoc or a
// comment. Used to split a document into statements and to ignore text that
// only looks like SQL (a ';' or a {param:Type} inside a string or comment).

export type SqlSpanKind = 'code' | 'string' | 'identifier' | 'comment'

export interface SqlSpan {
  kind: SqlSpanKind
  from: number
  to: number
}

export interface SqlStatement {
  /** Statement text, trimmed, without the terminating ';'. */
  text: string
  /** Document offset of the first character of text. */
  from: number
  /** Document offset just past the last character of text. */
  to: number
}

function quotedEnd(sql: string, start: number, quote: string): number {
  let i = start + 1
  while (i < sql.length) {
    const ch = sql[i]
    if (ch === '\\') {
      i += 2
      continue
    }
    if (ch === quote) {
      // A doubled quote is an escaped quote, not the end.
      if (sql[i + 1] === quote) {
        i += 2
        continue
      }
      return i + 1
    }
    i++
  }
  return sql.length
}

function blockCommentEnd(sql: string, start: number): number {
  // ClickHouse block comments nest.
  let depth = 0
  let i = start
  while (i < sql.length) {
    if (sql.startsWith('/*', i)) {
      depth++
      i += 2
    } else if (sql.startsWith('*/', i)) {
      depth--
      i += 2
      if (depth === 0) return i
    } else {
      i++
    }
  }
  return sql.length
}

const HEREDOC_OPEN = /^\$([A-Za-z_][A-Za-z0-9_]*)?\$/

/** Split SQL into code / string / identifier / comment spans covering the whole text. */
export function scanSql(sql: string): SqlSpan[] {
  const spans: SqlSpan[] = []
  let codeStart = 0
  let i = 0
  const push = (kind: SqlSpanKind, from: number, to: number) => {
    if (codeStart < from) spans.push({ kind: 'code', from: codeStart, to: from })
    spans.push({ kind, from, to })
    codeStart = to
    i = to
  }
  while (i < sql.length) {
    const ch = sql[i]
    const next = sql[i + 1]
    if (ch === "'") push('string', i, quotedEnd(sql, i, "'"))
    else if (ch === '"' || ch === '`') push('identifier', i, quotedEnd(sql, i, ch))
    else if (ch === '-' && next === '-') {
      const nl = sql.indexOf('\n', i)
      push('comment', i, nl < 0 ? sql.length : nl)
    } else if (ch === '#' && (next === ' ' || next === '!')) {
      const nl = sql.indexOf('\n', i)
      push('comment', i, nl < 0 ? sql.length : nl)
    } else if (ch === '/' && next === '*') push('comment', i, blockCommentEnd(sql, i))
    else if (ch === '$') {
      const m = HEREDOC_OPEN.exec(sql.slice(i, i + 66))
      if (m) {
        const tag = m[0]
        const close = sql.indexOf(tag, i + tag.length)
        push('string', i, close < 0 ? sql.length : close + tag.length)
      } else {
        i++
      }
    } else {
      i++
    }
  }
  if (codeStart < sql.length) spans.push({ kind: 'code', from: codeStart, to: sql.length })
  return spans
}

/**
 * Replace everything inside strings, quoted identifiers and comments with
 * spaces (newlines kept), so regexes over the result only see code. Offsets
 * are preserved. keepIdentifiers leaves `quoted` and "quoted" names in place.
 */
export function maskNonCode(sql: string, opts: { keepIdentifiers?: boolean } = {}): string {
  let out = ''
  for (const span of scanSql(sql)) {
    const text = sql.slice(span.from, span.to)
    const keep = span.kind === 'code' || (opts.keepIdentifiers === true && span.kind === 'identifier')
    out += keep ? text : text.replace(/[^\n]/g, ' ')
  }
  return out
}

/** True when the text holds nothing but whitespace and comments. */
function isBlank(sql: string): boolean {
  return maskNonCode(sql).trim() === ''
}

/**
 * Split a document into statements on ';' outside strings, quoted identifiers,
 * heredocs and comments. Statements holding only whitespace or comments are
 * dropped. Offsets refer to the original text.
 */
export function splitStatements(sql: string): SqlStatement[] {
  const out: SqlStatement[] = []
  let start = 0
  const add = (from: number, to: number) => {
    const raw = sql.slice(from, to)
    if (isBlank(raw)) return
    const lead = raw.length - raw.trimStart().length
    const text = raw.trim()
    out.push({ text, from: from + lead, to: from + lead + text.length })
  }
  for (const span of scanSql(sql)) {
    if (span.kind !== 'code') continue
    for (let i = span.from; i < span.to; i++) {
      if (sql[i] === ';') {
        add(start, i)
        start = i + 1
      }
    }
  }
  add(start, sql.length)
  return out
}

/**
 * The statement a cursor position belongs to: the one containing it, else the
 * nearest one before it (cursor after a ';' or on a blank line), else the first.
 */
export function statementAt(statements: SqlStatement[], pos: number): SqlStatement | null {
  if (statements.length === 0) return null
  let best: SqlStatement | null = null
  for (const st of statements) {
    if (pos >= st.from && pos <= st.to) return st
    if (st.to <= pos) best = st
  }
  return best ?? statements[0]
}

/** The first keyword of the SQL, upper-cased, ignoring leading comments ('' when none). */
export function firstKeyword(sql: string): string {
  const m = /^\s*([A-Za-z_]+)/.exec(maskNonCode(sql))
  return m ? m[1].toUpperCase() : ''
}
