import { apiGet } from '../api/client'

// Connection id to name, for items saved on another connection than the
// session's (saved queries, schedules). Read once per page load; a failed
// read is retried on the next call.
let names: Promise<Map<string, string>> | null = null

export function loadConnectionNames(): Promise<Map<string, string>> {
  names ??= apiGet<{ id: string; name: string }[]>('/api/connections')
    .then((list) => new Map(list.map((c) => [c.id, c.name])))
    .catch(() => {
      names = null
      return new Map<string, string>()
    })
  return names
}

/** The connection an item belongs to, when it is not the current one; else null. */
export function otherConnectionId(itemConnectionId: string | null | undefined, currentConnectionId: string | null | undefined): string | null {
  if (!itemConnectionId || !currentConnectionId || itemConnectionId === currentConnectionId) return null
  return itemConnectionId
}
