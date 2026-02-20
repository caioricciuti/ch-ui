import type { CompletionContext, CompletionResult, Completion } from '@codemirror/autocomplete'
import { snippetCompletion } from '@codemirror/autocomplete'
import { fetchCompletions, listColumns, listTables } from '../api/query'
import { getDatabases, loadDatabases } from '../stores/schema.svelte'
import type { Column } from '../types/schema'

// ── Cached server completions ───────────────────────────────────

let cachedFunctions: string[] | null = null
let cachedKeywords: string[] | null = null
let fetchPromise: Promise<void> | null = null

// ── Cached schema metadata for autocomplete ─────────────────────

let dbFetchPromise: Promise<void> | null = null
const tableCache = new Map<string, string[]>()
const columnCache = new Map<string, Column[]>()
const tableFetches = new Map<string, Promise<void>>()
const columnFetches = new Map<string, Promise<void>>()

type SqlContext = 'table' | 'column' | 'dot' | 'function' | 'default'

interface TableRef {
  db: string
  table: string
}

function normalizeIdent(id: string): string {
  return id.replace(/[`"']/g, '').trim()
}

function parseTableRef(ref: string): TableRef | null {
  const clean = normalizeIdent(ref)
  if (!clean) return null
  const parts = clean.split('.')
  if (parts.length >= 2) {
    return {
      db: normalizeIdent(parts[0]),
      table: normalizeIdent(parts[1]),
    }
  }
  // Table without DB; resolve later using known databases.
  return {
    db: '',
    table: normalizeIdent(parts[0]),
  }
}

function tableKey(db: string, table: string): string {
  return `${db}.${table}`
}

async function ensureFunctionKeywordCache(): Promise<void> {
  if (cachedFunctions !== null && cachedKeywords !== null) return
  if (fetchPromise) {
    await fetchPromise
    return
  }

  fetchPromise = fetchCompletions()
    .then(({ functions, keywords }) => {
      cachedFunctions = functions
      cachedKeywords = keywords
    })
    .catch(() => {
      cachedFunctions = []
      cachedKeywords = []
    })

  await fetchPromise
}

async function ensureDatabasesLoaded(): Promise<void> {
  if (getDatabases().length > 0) return
  if (dbFetchPromise) {
    await dbFetchPromise
    return
  }
  dbFetchPromise = loadDatabases().catch(() => {})
  await dbFetchPromise
}

async function ensureTablesCached(dbName: string): Promise<void> {
  if (tableCache.has(dbName)) return

  const dbInStore = getDatabases().find((d) => d.name === dbName)
  if (dbInStore?.tables) {
    tableCache.set(dbName, dbInStore.tables.map((t) => t.name))
    return
  }

  const existing = tableFetches.get(dbName)
  if (existing) {
    await existing
    return
  }

  const p = listTables(dbName)
    .then((tables) => {
      tableCache.set(dbName, tables)
    })
    .catch(() => {
      tableCache.set(dbName, [])
    })
    .finally(() => {
      tableFetches.delete(dbName)
    })

  tableFetches.set(dbName, p)
  await p
}

async function ensureColumnsCached(dbName: string, tableName: string): Promise<void> {
  const key = tableKey(dbName, tableName)
  if (columnCache.has(key)) return

  const dbInStore = getDatabases().find((d) => d.name === dbName)
  const tableInStore = dbInStore?.tables?.find((t) => t.name === tableName)
  if (tableInStore?.columns) {
    columnCache.set(key, tableInStore.columns)
    return
  }

  const existing = columnFetches.get(key)
  if (existing) {
    await existing
    return
  }

  const p = listColumns(dbName, tableName)
    .then((cols) => {
      columnCache.set(key, cols)
    })
    .catch(() => {
      columnCache.set(key, [])
    })
    .finally(() => {
      columnFetches.delete(key)
    })

  columnFetches.set(key, p)
  await p
}

function detectContext(doc: string, pos: number): SqlContext {
  const before = doc.slice(Math.max(0, pos - 1000), pos)

  if (/([`"\w]+)\.([`"\w]*)$/i.test(before)) return 'dot'

  if (/\b(?:FROM|JOIN|INTO|UPDATE|TABLE|DATABASE)\s+[`"\w.]*$/i.test(before)) return 'table'

  if (/\b(?:SELECT|WHERE|ORDER\s+BY|GROUP\s+BY|HAVING|AND|OR|ON|USING|SET|WITH|BY)\s+[`"\w.]*$/i.test(before)) {
    return 'column'
  }

  if (/\b\w+\(\s*[\w.`"]*$/i.test(before)) return 'function'

  return 'default'
}

function buildAliasMap(doc: string): Map<string, string> {
  const aliases = new Map<string, string>()
  const regex = /(?:FROM|JOIN)\s+([`"\w]+(?:\.[`"\w]+)?)(?:\s+(?:AS\s+)?([`"\w]+))?/gi
  let match: RegExpExecArray | null

  while ((match = regex.exec(doc)) !== null) {
    const tableRef = normalizeIdent(match[1])
    const alias = normalizeIdent(match[2] ?? '')
    if (alias) aliases.set(alias, tableRef)
  }

  return aliases
}

function buildReferencedTables(doc: string): string[] {
  const out = new Set<string>()
  const regex = /(?:FROM|JOIN)\s+([`"\w]+(?:\.[`"\w]+)?)/gi
  let match: RegExpExecArray | null
  while ((match = regex.exec(doc)) !== null) {
    const tableRef = normalizeIdent(match[1])
    if (tableRef) out.add(tableRef)
  }
  return [...out]
}

function knownDatabases(): string[] {
  const fromStore = getDatabases().map((d) => d.name)
  return [...new Set([...fromStore, ...tableCache.keys()])]
}

function findTablesForDatabase(dbName: string): string[] {
  const fromStore = getDatabases().find((d) => d.name === dbName)?.tables?.map((t) => t.name) ?? []
  const fromCache = tableCache.get(dbName) ?? []
  return [...new Set([...fromStore, ...fromCache])]
}

function findColumns(dbName: string, tableName: string): Column[] {
  const key = tableKey(dbName, tableName)
  const fromCache = columnCache.get(key) ?? []
  if (fromCache.length > 0) return fromCache
  const fromStore = getDatabases().find((d) => d.name === dbName)?.tables?.find((t) => t.name === tableName)?.columns ?? []
  return fromStore
}

function fuzzyScore(text: string, term: string): number {
  const t = text.toLowerCase()
  const q = term.toLowerCase().trim()
  if (!q) return 1

  if (t === q) return 180
  if (t.startsWith(q)) return 130
  if (t.includes(`.${q}`)) return 110
  if (t.includes(` ${q}`)) return 95
  if (t.includes(q)) return 70

  let qi = 0
  let seqScore = 0
  for (let i = 0; i < t.length && qi < q.length; i++) {
    if (t[i] === q[qi]) {
      seqScore += i > 0 && /[._\s]/.test(t[i - 1]) ? 8 : 3
      qi++
    }
  }
  return qi === q.length ? seqScore : -1
}

function rankCompletions(items: Completion[], term: string): Completion[] {
  return items
    .map((item) => {
      const hay = `${item.label} ${item.detail ?? ''}`
      const score = fuzzyScore(hay, term)
      return { item, score: score + (item.boost ?? 0) }
    })
    .filter((x) => x.score >= 0)
    .sort((a, b) => b.score - a.score)
    .slice(0, 300)
    .map((x) => x.item)
}

function dedupeCompletions(items: Completion[]): Completion[] {
  const seen = new Set<string>()
  const out: Completion[] = []
  for (const item of items) {
    const key = `${item.label}|${item.type}|${item.detail ?? ''}`
    if (seen.has(key)) continue
    seen.add(key)
    out.push(item)
  }
  return out
}

async function resolveUnqualifiedTableRefs(tableName: string): Promise<TableRef[]> {
  const refs: TableRef[] = []
  for (const db of knownDatabases()) {
    await ensureTablesCached(db)
    if (findTablesForDatabase(db).includes(tableName)) {
      refs.push({ db, table: tableName })
    }
  }
  return refs
}

async function buildDotCompletions(doc: string, beforeCursor: string): Promise<Completion[]> {
  const match = beforeCursor.match(/([`"\w]+)\.([`"\w]*)$/)
  if (!match) return []

  const lhs = normalizeIdent(match[1])
  const aliasMap = buildAliasMap(doc)
  const aliasTableRef = aliasMap.get(lhs)

  // alias.column -> resolve alias table and columns
  if (aliasTableRef) {
    const parsed = parseTableRef(aliasTableRef)
    if (!parsed) return []

    const refs: TableRef[] = parsed.db
      ? [parsed]
      : await resolveUnqualifiedTableRefs(parsed.table)

    const options: Completion[] = []
    for (const ref of refs) {
      await ensureColumnsCached(ref.db, ref.table)
      for (const col of findColumns(ref.db, ref.table)) {
        options.push({
          label: col.name,
          detail: `${col.type} (${ref.db}.${ref.table})`,
          type: 'property',
          boost: 22,
        })
      }
    }
    return options
  }

  // db.table -> suggest table list
  const dbNames = knownDatabases()
  if (dbNames.includes(lhs)) {
    await ensureTablesCached(lhs)
    return findTablesForDatabase(lhs).map((t) => ({
      label: t,
      detail: lhs,
      type: 'class',
      boost: 18,
    }))
  }

  // table.column (unqualified table name) -> resolve across dbs
  const refs = await resolveUnqualifiedTableRefs(lhs)
  const options: Completion[] = []
  for (const ref of refs) {
    await ensureColumnsCached(ref.db, ref.table)
    for (const col of findColumns(ref.db, ref.table)) {
      options.push({
        label: col.name,
        detail: `${col.type} (${ref.db}.${ref.table})`,
        type: 'property',
        boost: 16,
      })
    }
  }
  return options
}

function buildDatabaseCompletions(): Completion[] {
  return knownDatabases().map((db) => ({
    label: db,
    type: 'namespace',
    boost: 5,
  }))
}

async function buildTableCompletions(term: string): Promise<Completion[]> {
  const options: Completion[] = []
  const termClean = normalizeIdent(term)
  const dotIdx = termClean.indexOf('.')

  // If user typed "db." in FROM/JOIN context, prioritize tables of that DB
  if (dotIdx >= 0) {
    const dbName = termClean.slice(0, dotIdx)
    if (dbName) {
      await ensureTablesCached(dbName)
      for (const t of findTablesForDatabase(dbName)) {
        options.push({
          label: t,
          detail: dbName,
          type: 'class',
          boost: 24,
        })
      }
      return options
    }
  }

  for (const dbName of knownDatabases()) {
    await ensureTablesCached(dbName)
    for (const table of findTablesForDatabase(dbName)) {
      options.push({
        label: `${dbName}.${table}`,
        type: 'class',
        boost: 16,
      })
      options.push({
        label: table,
        detail: dbName,
        type: 'class',
        boost: 10,
      })
    }
  }

  return options
}

async function buildReferencedColumnCompletions(doc: string): Promise<Completion[]> {
  const aliasMap = buildAliasMap(doc)
  const tableRefs = buildReferencedTables(doc)
  const options: Completion[] = []

  // map tableRef -> aliases for alias-qualified completion
  const refToAliases = new Map<string, string[]>()
  for (const [alias, ref] of aliasMap.entries()) {
    const list = refToAliases.get(ref) ?? []
    list.push(alias)
    refToAliases.set(ref, list)
  }

  for (const refRaw of tableRefs) {
    const parsed = parseTableRef(refRaw)
    if (!parsed) continue

    const refs: TableRef[] = parsed.db
      ? [parsed]
      : await resolveUnqualifiedTableRefs(parsed.table)

    for (const ref of refs) {
      await ensureColumnsCached(ref.db, ref.table)
      const columns = findColumns(ref.db, ref.table)
      for (const col of columns) {
        options.push({
          label: col.name,
          detail: `${col.type} (${ref.db}.${ref.table})`,
          type: 'property',
          boost: 20,
        })

        // alias.column suggestions for JOIN/ON precision
        const aliases = refToAliases.get(refRaw) ?? []
        for (const alias of aliases) {
          options.push({
            label: `${alias}.${col.name}`,
            detail: `${col.type} (${ref.db}.${ref.table})`,
            type: 'property',
            boost: 24,
          })
        }
      }
    }
  }

  return options
}

function buildFunctionCompletions(): Completion[] {
  return (cachedFunctions ?? []).map((fn) => ({
    label: fn,
    type: 'function',
    boost: 7,
    detail: 'Function',
  }))
}

function buildKeywordCompletions(): Completion[] {
  return (cachedKeywords ?? []).map((kw) => ({
    label: kw,
    type: 'keyword',
    boost: 4,
  }))
}

function buildSnippetCompletions(): Completion[] {
  return [
    snippetCompletion('SELECT ${columns}\nFROM ${database}.${table}\nLIMIT ${1000}', {
      label: 'SELECT … FROM',
      type: 'snippet',
      detail: 'Query starter',
      boost: 30,
    }),
    snippetCompletion('SELECT ${a}.*, ${b}.*\nFROM ${table_a} ${a}\nJOIN ${table_b} ${b} ON ${a}.${id} = ${b}.${id}', {
      label: 'JOIN Template',
      type: 'snippet',
      detail: 'Join two tables',
      boost: 28,
    }),
    snippetCompletion('WITH ${cte_name} AS (\n  ${query}\n)\nSELECT *\nFROM ${cte_name}', {
      label: 'CTE Template',
      type: 'snippet',
      detail: 'WITH clause',
      boost: 26,
    }),
    snippetCompletion('countIf(${condition})', {
      label: 'countIf()',
      type: 'snippet',
      detail: 'Conditional count',
      boost: 20,
    }),
    snippetCompletion('sumIf(${value}, ${condition})', {
      label: 'sumIf()',
      type: 'snippet',
      detail: 'Conditional sum',
      boost: 20,
    }),
    snippetCompletion('uniqExact(${value})', {
      label: 'uniqExact()',
      type: 'snippet',
      detail: 'Exact cardinality',
      boost: 18,
    }),
    snippetCompletion('toStartOfInterval(${timestamp}, INTERVAL ${5} minute)', {
      label: 'toStartOfInterval()',
      type: 'snippet',
      detail: 'Time bucket',
      boost: 18,
    }),
  ]
}

// ── Main completion source ──────────────────────────────────────

export async function clickhouseCompletionSource(
  context: CompletionContext,
): Promise<CompletionResult | null> {
  const word = context.matchBefore(/[\w.`"]*/)
  if (!word) return null
  if (word.from === word.to && !context.explicit) return null

  await Promise.all([ensureFunctionKeywordCache(), ensureDatabasesLoaded()])

  const doc = context.state.doc.toString()
  const beforeCursor = doc.slice(0, context.pos)
  const sqlCtx = detectContext(doc, word.from)
  const term = word.text.replace(/[`"]/g, '')

  let options: Completion[] = []

  switch (sqlCtx) {
    case 'dot':
      options = await buildDotCompletions(doc, beforeCursor)
      break
    case 'table':
      options = [
        ...(await buildTableCompletions(term)),
        ...buildDatabaseCompletions(),
        ...buildKeywordCompletions().filter((k) => ['JOIN', 'ON', 'USING', 'WHERE'].includes(k.label)),
      ]
      break
    case 'column':
      options = [
        ...(await buildReferencedColumnCompletions(doc)),
        ...buildFunctionCompletions(),
        ...buildSnippetCompletions(),
        ...buildKeywordCompletions(),
      ]
      break
    case 'function':
      options = [
        ...buildFunctionCompletions(),
        ...(await buildReferencedColumnCompletions(doc)),
      ]
      break
    default:
      options = [
        ...buildSnippetCompletions(),
        ...buildKeywordCompletions(),
        ...buildFunctionCompletions(),
        ...buildDatabaseCompletions(),
        ...(await buildTableCompletions(term)),
        ...(await buildReferencedColumnCompletions(doc)),
      ]
      break
  }

  const ranked = rankCompletions(dedupeCompletions(options), term)

  return {
    from: word.from,
    options: ranked,
    validFor: /^[\w.`"]*$/,
  }
}
