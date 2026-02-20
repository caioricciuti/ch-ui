import type { Database, Table, Column } from '../types/schema'
import { apiGet } from '../api/client'

let databases = $state<Database[]>([])
let loading = $state(false)

export function getDatabases(): Database[] {
  return databases
}

export function isSchemaLoading(): boolean {
  return loading
}

export async function loadDatabases(): Promise<void> {
  loading = true
  try {
    const res = await apiGet<{ databases: string[] }>('/api/query/databases')
    databases = (res.databases ?? []).map(name => ({ name }))
  } catch {
    databases = []
  } finally {
    loading = false
  }
}

export async function loadTables(dbName: string): Promise<void> {
  databases = databases.map(db => {
    if (db.name !== dbName) return db
    return { ...db, loading: true, expanded: true }
  })

  try {
    const res = await apiGet<{ tables: string[] }>(`/api/query/tables?database=${encodeURIComponent(dbName)}`)
    const tables: Table[] = (res.tables ?? []).map(name => ({ name }))
    databases = databases.map(db => {
      if (db.name !== dbName) return db
      return { ...db, tables, loading: false }
    })
  } catch {
    databases = databases.map(db => {
      if (db.name !== dbName) return db
      return { ...db, loading: false }
    })
  }
}

export async function loadColumns(dbName: string, tableName: string): Promise<void> {
  databases = databases.map(db => {
    if (db.name !== dbName) return db
    return {
      ...db,
      tables: db.tables?.map(t => {
        if (t.name !== tableName) return t
        return { ...t, loading: true, expanded: true }
      }),
    }
  })

  try {
    const res = await apiGet<{ columns: Column[] }>(`/api/query/columns?database=${encodeURIComponent(dbName)}&table=${encodeURIComponent(tableName)}`)
    const columns: Column[] = res.columns ?? []
    databases = databases.map(db => {
      if (db.name !== dbName) return db
      return {
        ...db,
        tables: db.tables?.map(t => {
          if (t.name !== tableName) return t
          return { ...t, columns, loading: false }
        }),
      }
    })
  } catch {
    databases = databases.map(db => {
      if (db.name !== dbName) return db
      return {
        ...db,
        tables: db.tables?.map(t => {
          if (t.name !== tableName) return t
          return { ...t, loading: false }
        }),
      }
    })
  }
}

export function toggleDatabase(dbName: string): void {
  const db = databases.find(d => d.name === dbName)
  if (!db) return
  if (db.expanded) {
    databases = databases.map(d => d.name === dbName ? { ...d, expanded: false } : d)
  } else {
    loadTables(dbName)
  }
}

export function toggleTable(dbName: string, tableName: string): void {
  const db = databases.find(d => d.name === dbName)
  const table = db?.tables?.find(t => t.name === tableName)
  if (!table) return
  if (table.expanded) {
    databases = databases.map(d => {
      if (d.name !== dbName) return d
      return {
        ...d,
        tables: d.tables?.map(t => t.name === tableName ? { ...t, expanded: false } : t),
      }
    })
  } else {
    loadColumns(dbName, tableName)
  }
}
