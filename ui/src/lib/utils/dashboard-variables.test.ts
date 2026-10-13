import { describe, expect, it } from 'vitest'
import type { DashboardVariable } from '../types/api'
import {
  dependsOn,
  fallbackValue,
  initialVariableValues,
  isValidVariableName,
  parseOptionList,
  readVariablesFromSearch,
  reconcileValues,
  referencesVariable,
  resolveSelection,
  staticOptions,
  writeVariablesToSearch,
} from './dashboard-variables'

const db: DashboardVariable = { name: 'db', type: 'query', query: 'SELECT name FROM system.databases' }
const table: DashboardVariable = { name: 'table', type: 'query', query: 'SELECT name FROM system.tables WHERE database IN (${db})', multi: true, include_all: true }
const env: DashboardVariable = { name: 'env', type: 'custom', options: 'prod, staging, prod,', default: ['staging'] }
const step: DashboardVariable = { name: 'step', type: 'interval', options: '1m,5m,1h' }
const tenant: DashboardVariable = { name: 'tenant', type: 'constant', default: ['acme'] }
const search: DashboardVariable = { name: 'search', type: 'text' }

const opts = (...values: string[]) => values.map((value) => ({ value, label: value }))

describe('URL values', () => {
  it('reads repeated var-* parameters and ignores the rest', () => {
    const params = new URLSearchParams('from=now-1h&to=now&var-db=a&var-table=t1&var-table=t2&var-=x&var-search=')
    expect(readVariablesFromSearch(params)).toEqual({ db: ['a'], table: ['t1', 't2'], search: [''] })
  })

  it('writes values back, keeping other parameters and leaving constants out', () => {
    const params = new URLSearchParams('from=now-1h&to=now&var-old=1&var-db=x')
    writeVariablesToSearch(params, [db, table, tenant], { db: ['b'], table: ['t1', 't2'], tenant: ['acme'] })
    expect(params.toString()).toBe('from=now-1h&to=now&var-db=b&var-table=t1&var-table=t2')
  })
})

describe('initial values', () => {
  it('prefers the URL, then the default, and leaves the rest unset', () => {
    const values = initialVariableValues([db, table, env, step, tenant], { table: ['a', 'b'], step: ['5m', '1h'] })
    expect(values).toEqual({ table: ['a', 'b'], env: ['staging'], step: ['5m'] })
  })

  it('drops All where it is not allowed and unknown names', () => {
    expect(reconcileValues([db, table], { db: ['$__all'], table: ['x', '$__all'], gone: ['1'] })).toEqual({ table: ['$__all'] })
  })
})

describe('selection', () => {
  it('keeps values that are still options', () => {
    expect(resolveSelection(table, ['a', 'z'], opts('a', 'b'))).toEqual(['a'])
    expect(resolveSelection(table, ['$__all'], opts('a'))).toEqual(['$__all'])
  })

  it('falls back like the server: default, then All for multi, then the first option', () => {
    expect(resolveSelection(env, ['gone'], opts('prod', 'staging'))).toEqual(['staging'])
    expect(resolveSelection(table, undefined, opts('a'))).toEqual(['$__all'])
    expect(resolveSelection(db, ['gone'], opts('a', 'b'))).toEqual(['a'])
    expect(fallbackValue(db, [])).toEqual([])
  })

  it('keeps values a truncated list cannot rule out', () => {
    expect(resolveSelection(db, ['far'], opts('a'), true)).toEqual(['far'])
  })
})

describe('options and dependencies', () => {
  it('parses comma lists', () => {
    expect(parseOptionList(' a, b ,,a ')).toEqual(['a', 'b'])
    expect(staticOptions(env)).toEqual(opts('prod', 'staging'))
    expect(staticOptions(step)).toEqual(opts('1m', '5m', '1h'))
    expect(staticOptions(db)).toBeNull()
  })

  it('finds references in all three forms', () => {
    expect(referencesVariable('WHERE d = $db', 'db')).toBe(true)
    expect(referencesVariable('WHERE d IN (${db})', 'db')).toBe(true)
    expect(referencesVariable('LIMIT ${db:number}', 'db')).toBe(true)
    expect(referencesVariable('WHERE d = $dbx', 'db')).toBe(false)
    expect(dependsOn(table, ['db'])).toBe(true)
    expect(dependsOn(table, ['env'])).toBe(false)
    expect(dependsOn({ name: 'p', type: 'query' }, ['env'])).toBe(true)
    expect(dependsOn(env, ['db'])).toBe(false)
  })

  it('checks names like the server', () => {
    expect(isValidVariableName('db_1')).toBe(true)
    expect(isValidVariableName('__x')).toBe(false)
    expect(isValidVariableName('1db')).toBe(false)
    expect(isValidVariableName(search.name)).toBe(true)
  })
})
