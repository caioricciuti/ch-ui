import { apiDel, apiGet, apiPost, apiPut } from './client'
import type {
  BrainArtifact,
  BrainChat,
  BrainMessage,
  BrainModelOption,
  BrainProviderAdmin,
  BrainSkill,
} from '../types/brain'

export async function listBrainModels(): Promise<BrainModelOption[]> {
  const res = await apiGet<{ success: boolean; models: BrainModelOption[] }>('/api/brain/models')
  return res.models ?? []
}

export async function listBrainChats(includeArchived = false): Promise<BrainChat[]> {
  const res = await apiGet<{ success: boolean; chats: BrainChat[] }>(`/api/brain/chats?includeArchived=${includeArchived}`)
  return res.chats ?? []
}

export async function createBrainChat(payload: { title?: string; modelId?: string }): Promise<BrainChat> {
  const res = await apiPost<{ success: boolean; chat: BrainChat }>('/api/brain/chats', payload)
  return res.chat
}

export async function updateBrainChat(chatId: string, payload: { title?: string; archived?: boolean; modelId?: string; contextDatabase?: string; contextTable?: string; contextTables?: string }): Promise<void> {
  await apiPut(`/api/brain/chats/${encodeURIComponent(chatId)}`, payload)
}

export async function deleteBrainChat(chatId: string): Promise<void> {
  await apiDel(`/api/brain/chats/${encodeURIComponent(chatId)}`)
}

export async function listBrainMessages(chatId: string): Promise<BrainMessage[]> {
  const res = await apiGet<{ success: boolean; messages: BrainMessage[] }>(`/api/brain/chats/${encodeURIComponent(chatId)}/messages`)
  return res.messages ?? []
}

export async function listBrainArtifacts(chatId: string): Promise<BrainArtifact[]> {
  const res = await apiGet<{ success: boolean; artifacts: BrainArtifact[] }>(`/api/brain/chats/${encodeURIComponent(chatId)}/artifacts`)
  return res.artifacts ?? []
}

export async function runBrainQueryArtifact(chatId: string, payload: { query: string; title?: string; messageId?: string; timeout?: number }): Promise<any> {
  return apiPost(`/api/brain/chats/${encodeURIComponent(chatId)}/artifacts/query`, payload)
}

export interface StreamEvent {
  type: 'delta' | 'done' | 'error'
  delta?: string
  error?: string
  messageId?: string
  chatId?: string
}

export async function streamBrainMessage(
  chatId: string,
  payload: { content: string; modelId?: string; schemaContext?: any; schemaContexts?: any[] },
  onEvent: (event: StreamEvent) => void,
): Promise<void> {
  const response = await fetch(`/api/brain/chats/${encodeURIComponent(chatId)}/messages/stream`, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })

  if (!response.ok) {
    const body = await response.json().catch(() => ({}))
    throw new Error(body.error ?? `Request failed (${response.status})`)
  }

  const reader = response.body?.getReader()
  if (!reader) throw new Error('No response body')

  const decoder = new TextDecoder()
  let buffer = ''

  while (true) {
    const { done, value } = await reader.read()
    if (done) break

    buffer += decoder.decode(value, { stream: true })
    const lines = buffer.split('\n')
    buffer = lines.pop() ?? ''

    for (const line of lines) {
      if (!line.startsWith('data: ')) continue
      const raw = line.slice(6)
      if (!raw) continue
      try {
        const parsed = JSON.parse(raw) as StreamEvent
        onEvent(parsed)
      } catch {
        // ignore malformed chunks
      }
    }
  }
}

// -------- Admin endpoints --------

export async function adminListBrainProviders(): Promise<BrainProviderAdmin[]> {
  const res = await apiGet<{ success: boolean; providers: BrainProviderAdmin[] }>('/api/admin/brain/providers')
  return res.providers ?? []
}

export async function adminCreateBrainProvider(payload: {
  name: string
  kind: string
  baseUrl?: string
  apiKey?: string
  isActive?: boolean
  isDefault?: boolean
}): Promise<void> {
  await apiPost('/api/admin/brain/providers', payload)
}

export async function adminUpdateBrainProvider(id: string, payload: {
  name?: string
  kind?: string
  baseUrl?: string
  apiKey?: string
  isActive?: boolean
  isDefault?: boolean
}): Promise<void> {
  await apiPut(`/api/admin/brain/providers/${encodeURIComponent(id)}`, payload)
}

export async function adminDeleteBrainProvider(id: string): Promise<void> {
  await apiDel(`/api/admin/brain/providers/${encodeURIComponent(id)}`)
}

export async function adminSyncBrainProviderModels(id: string): Promise<void> {
  await apiPost(`/api/admin/brain/providers/${encodeURIComponent(id)}/sync-models`)
}

export async function adminListBrainModels(): Promise<BrainModelOption[]> {
  const res = await apiGet<{ success: boolean; models: BrainModelOption[] }>('/api/admin/brain/models')
  return res.models ?? []
}

export async function adminUpdateBrainModel(id: string, payload: {
  displayName?: string
  isActive?: boolean
  isDefault?: boolean
}): Promise<void> {
  await apiPut(`/api/admin/brain/models/${encodeURIComponent(id)}`, payload)
}

export async function adminBulkUpdateBrainModels(payload: {
  providerId: string
  action: 'deactivate_all' | 'activate_all' | 'activate_recommended'
}): Promise<void> {
  await apiPost('/api/admin/brain/models/bulk', payload)
}

export async function adminListBrainSkills(): Promise<BrainSkill[]> {
  const res = await apiGet<{ success: boolean; skills: BrainSkill[] }>('/api/admin/brain/skills')
  return res.skills ?? []
}

export async function adminCreateBrainSkill(payload: {
  name: string
  content: string
  isActive?: boolean
  isDefault?: boolean
}): Promise<void> {
  await apiPost('/api/admin/brain/skills', payload)
}

export async function adminUpdateBrainSkill(id: string, payload: {
  name?: string
  content?: string
  isActive?: boolean
  isDefault?: boolean
}): Promise<void> {
  await apiPut(`/api/admin/brain/skills/${encodeURIComponent(id)}`, payload)
}
