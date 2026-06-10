// Parse ClickHouse error strings into structured form so the result panel can
// show a readable message, an actionable hint, and a jump-to-position action.
//
// Typical raw error (possibly wrapped by the connector/tunnel with prefixes
// like "ClickHouse error: "):
//   Code: 62. DB::Exception: Syntax error: failed at position 47 ('FRM')
//   (line 2, col 10): FRM events. Expected one of: FROM, ... (SYNTAX_ERROR)
//   (version 24.8.4.13 (official build))

export interface ParsedCHError {
  code: number | null
  name: string | null
  /** Cleaned, most specific message (falls back to raw when nothing parses). */
  message: string
  hint: string | null
  /** 1-based offset, as reported by ClickHouse. Only an offset into the
   * executed SQL when positionInQuery is true. */
  position: number | null
  /** False for sub-parse errors ("Syntax error (data type): …" indexes an
   * embedded string like a CAST type) and errors relayed from remote shards
   * ("Received from …" — the position indexes the rewritten shard query). */
  positionInQuery: boolean
  line: number | null
  col: number | null
  /** The offending token when reported and selectable (not truncated). */
  token: string | null
  raw: string
}

const CODE_RE = /Code:\s*(\d+)\./
const POSITION_START_RE = /failed at position (\d+)/
// Token segment directly after the position: ('quoted') on ClickHouse <= 25.1,
// unquoted with optional "..." truncation on >= 25.2, or (end of query).
const TOKEN_RE = /^\s*\((?!line \d)(?:'((?:[^'\\]|\\.)*)'|([^()]*?))(\.\.\.)?\)/
const LINE_COL_RE = /^\s*\(line (\d+), col (\d+)\)/
// Error names are ALL_CAPS with underscores; the underscore requirement
// avoids matching type/format names like (UInt64) or (CSV). ABORTED and
// UNFINISHED are the only real single-word ClickHouse error names.
const NAME_RE = /\(([A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+|ABORTED|UNFINISHED)\)/g
const NAME_SUFFIX_RE = /\s*\(([A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+|ABORTED|UNFINISHED)\)\s*\.?\s*$/
// DB::Exception, DB::ParsingException (<= 23.x), DB::NetException, …
const EXCEPTION_RE = /DB::\w*Exception:/g

const HINTS: Record<number, string> = {
  62: 'Check the SQL syntax near the highlighted token — a keyword may be misspelled or missing.',
  60: "The table doesn't exist — check the name and database (the message may suggest a close match).",
  81: "The database doesn't exist — check the name or create it first.",
  47: 'Unknown column or identifier — check the spelling against the table schema.',
  241: 'The query exceeded the memory limit — narrow the date range, add filters, or reduce columns and JOINs.',
  159: 'The query timed out — add filters or a LIMIT, or increase the query timeout.',
  209: 'The server took too long to respond — retry, or increase the timeout.',
  516: 'Authentication failed — check the ClickHouse username and password for this connection.',
  396: 'The result exceeded the configured row/byte limit — lower Max rows or add a LIMIT.',
  394: 'The query was cancelled on the server.',
  202: 'Too many simultaneous queries on the server — retry in a moment.',
}

/**
 * ClickHouse positions are 1-based BYTE offsets into the UTF-8 query; JS
 * strings index UTF-16 code units. Convert so jumps stay accurate when the
 * SQL contains non-ASCII characters (string literals, identifiers, comments).
 */
export function byteToCharOffset(s: string, byteOffset: number): number {
  if (byteOffset <= 0) return 0
  const bytes = new TextEncoder().encode(s)
  if (byteOffset >= bytes.length) return s.length
  return new TextDecoder().decode(bytes.slice(0, byteOffset)).length
}

export function parseCHError(raw: string): ParsedCHError {
  const codeMatch = CODE_RE.exec(raw)
  const code = codeMatch ? parseInt(codeMatch[1], 10) : null

  let name: string | null = null
  let m: RegExpExecArray | null
  NAME_RE.lastIndex = 0
  while ((m = NAME_RE.exec(raw)) !== null) name = m[1]

  let position: number | null = null
  let positionInQuery = false
  let token: string | null = null
  let line: number | null = null
  let col: number | null = null

  const posMatch = POSITION_START_RE.exec(raw)
  if (posMatch) {
    position = parseInt(posMatch[1], 10)
    // Only a plain top-level "Syntax error: failed at position" indexes the
    // executed query. "Syntax error (data type):" indexes an embedded string;
    // anything after "Received from" indexes a rewritten remote query.
    const prefix = raw.slice(0, posMatch.index)
    positionInQuery = /Syntax error:\s*$/.test(prefix) && !prefix.includes('Received from')

    let rest = raw.slice(posMatch.index + posMatch[0].length)
    const tokenMatch = TOKEN_RE.exec(rest)
    if (tokenMatch) {
      const truncated = tokenMatch[3] !== undefined
      const t = tokenMatch[1] ?? tokenMatch[2] ?? ''
      if (t && t !== 'end of query' && !truncated) token = t
      rest = rest.slice(tokenMatch[0].length)
    }
    const lineCol = LINE_COL_RE.exec(rest)
    if (lineCol) {
      line = parseInt(lineCol[1], 10)
      col = parseInt(lineCol[2], 10)
    }
  }

  // Distributed errors nest exceptions ("DB::Exception: Received from …:
  // DB::Exception: …") — the last segment is the most specific.
  let message = raw
  let lastExcEnd = -1
  EXCEPTION_RE.lastIndex = 0
  while ((m = EXCEPTION_RE.exec(raw)) !== null) lastExcEnd = m.index + m[0].length
  if (lastExcEnd >= 0) message = message.slice(lastExcEnd)
  const stackIdx = message.indexOf('Stack trace:')
  if (stackIdx >= 0) message = message.slice(0, stackIdx)
  message = message
    .replace(/\(version [^)]*(\([^)]*\))?[^)]*\)\s*\.?\s*$/, '')
    .replace(NAME_SUFFIX_RE, '')
    .trim()
  if (!message) message = raw

  return {
    code,
    name,
    message,
    hint: code !== null ? (HINTS[code] ?? null) : null,
    position,
    positionInQuery,
    line,
    col,
    token,
    raw,
  }
}
