// SPDX-License-Identifier: BUSL-1.1
import { apiGet, apiPost, apiDel } from './client'

export type TimelineSource = 'queries' | 'parts' | 'health' | 'incidents' | 'comments' | 'deployments'
export interface TimelineEvent {
  id: string
  at: string
  source: TimelineSource
  kind: string
  title: string
  details?: string
  severity: string
  incident_id?: string
  actor?: string
  values?: Record<string, unknown>
}
export interface TimelineCoverage {
  source: TimelineSource
  available: boolean
  message: string
  events: number
  truncated: boolean
}
export interface IncidentTimeline {
  events: TimelineEvent[]
  coverage: TimelineCoverage[]
  from: string
  to: string
  incident_id: string
  bucket_seconds: number
}

export function fetchIncidentTimeline(from: string, to: string, incidentId = '') {
  const params = new URLSearchParams({ from, to })
  if (incidentId) params.set('incident_id', incidentId)
  return apiGet<IncidentTimeline>(`/api/incident-timeline/?${params}`)
}

export function createDeploymentAnnotation(input: { occurred_at: string; title: string; details: string }) {
  return apiPost<{ id: string }>('/api/incident-timeline/annotations', input)
}

export function deleteDeploymentAnnotation(id: string) {
  return apiDel(`/api/incident-timeline/annotations/${encodeURIComponent(id)}`)
}
