import { Eye, Table2, ArrowDownToLine, Zap } from 'lucide-svelte'
import type { Materialization, TestOutcome } from '../../types/models'

/** Any lucide-svelte icon; they all share this component type. */
export type IconComponent = typeof Eye

export interface MaterializationInfo {
  value: Materialization
  label: string
  /** One line on what a run does. */
  description: string
  icon: IconComponent
}

export const MATERIALIZATIONS: MaterializationInfo[] = [
  { value: 'view', label: 'View', description: 'computed on read', icon: Eye },
  { value: 'table', label: 'Table', description: 'rebuilt on every run', icon: Table2 },
  { value: 'incremental', label: 'Incremental', description: 'first run builds, later runs add new rows', icon: ArrowDownToLine },
  { value: 'materialized_view', label: 'Materialized view', description: 'a table fed on every insert by <name>__mv', icon: Zap },
]

export function materializationInfo(m: string): MaterializationInfo {
  return MATERIALIZATIONS.find((x) => x.value === m) ?? MATERIALIZATIONS[0]
}

/** Materializations that create a table, and so take engine options. */
export function buildsTable(m: string): boolean {
  return m === 'table' || m === 'incremental' || m === 'materialized_view'
}

/** Materializations that keep state between runs, so a full refresh means something. */
export function supportsFullRefresh(m: string): boolean {
  return m === 'incremental' || m === 'materialized_view'
}

/** Parses a run result's test_details; bad or empty JSON is no outcomes. */
export function parseTestOutcomes(details: string | null | undefined): TestOutcome[] {
  if (!details) return []
  try {
    const parsed: unknown = JSON.parse(details)
    return Array.isArray(parsed) ? (parsed as TestOutcome[]) : []
  } catch {
    return []
  }
}
