import { fetchJson } from '@/lib/fetch'

import type {
  ConversationRead,
  ConversationMessagesResponse,
  ConversationsResponse,
  MessageState,
  SendMessageResponse,
} from '@/types/message'

/** History filters do not require the selected SIM to be enabled. */
export interface ConversationFilter {
  modemId?: string
  profileId?: string
  q?: string
  unassigned?: boolean
}

export const useMessageApi = () => {
  const sendMessage = (id: string, to: string, text: string, expectedProfileId?: string) => {
    return fetchJson<SendMessageResponse>(`modems/${id}/messages`, {
      method: 'POST',
      body: JSON.stringify({ to, text, expectedProfileId }),
    })
  }

  const getConversations = (filter: ConversationFilter = {}) => {
    const query = new URLSearchParams()
    if (filter.modemId) query.set('modemId', filter.modemId)
    if (filter.profileId) query.set('profileId', filter.profileId)
    if (filter.q?.trim()) query.set('q', filter.q.trim())
    if (filter.unassigned) query.set('unassigned', 'true')
    return fetchJson<ConversationsResponse>(`conversations?${query}`)
  }

  const getConversationMessages = (id: string, cursor = '') => {
    const query = new URLSearchParams({ limit: '50' })
    if (cursor) query.set('cursor', cursor)
    return fetchJson<ConversationMessagesResponse>(
      `conversations/${encodeURIComponent(id)}/messages?${query}`,
    )
  }

  const updateConversation = (id: string, update: ConversationRead) =>
    fetchJson<MessageState>(`conversations/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(update),
    })

  const deleteConversation = (id: string) =>
    fetchJson<void>(`conversations/${encodeURIComponent(id)}`, { method: 'DELETE' })

  return {
    sendMessage,
    getConversations,
    getConversationMessages,
    updateConversation,
    deleteConversation,
  }
}
