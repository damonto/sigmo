import { fetchJson } from '@/lib/fetch'

import type {
  ConversationRead,
  ConversationsResponse,
  MessageState,
  MessagesResponse,
  SendMessageResponse,
} from '@/types/message'

/** History filters do not require the selected SIM to be enabled. */
export interface ConversationFilter {
  modemId?: string
  profileId?: string
  q?: string
  unassigned?: boolean
}

const messagesPath = (id: string, query?: string) => {
  const path = `modems/${id}/messages`
  const params = new URLSearchParams()
  const trimmed = query?.trim()
  if (trimmed) {
    params.set('q', trimmed)
  }
  const search = params.toString()
  return search ? `${path}?${search}` : path
}

export const useMessageApi = () => {
  const getMessages = (id: string, query?: string) => {
    return fetchJson<MessagesResponse>(messagesPath(id, query))
  }

  const getMessagesByParticipant = (id: string, participant: string) => {
    const encoded = encodeURIComponent(participant)
    return fetchJson<MessagesResponse>(`modems/${id}/messages/${encoded}`)
  }

  const deleteMessagesByParticipant = (id: string, participant: string) => {
    const encoded = encodeURIComponent(participant)
    return fetchJson<void>(`modems/${id}/messages/${encoded}`, {
      method: 'DELETE',
    })
  }

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

  const getConversationMessages = (id: string) =>
    fetchJson<MessagesResponse>(`conversations/${encodeURIComponent(id)}/messages`)

  const updateConversation = (id: string, update: ConversationRead) =>
    fetchJson<MessageState>(`conversations/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(update),
    })

  const deleteConversation = (id: string) =>
    fetchJson<void>(`conversations/${encodeURIComponent(id)}`, { method: 'DELETE' })

  return {
    getMessages,
    getMessagesByParticipant,
    deleteMessagesByParticipant,
    sendMessage,
    getConversations,
    getConversationMessages,
    updateConversation,
    deleteConversation,
  }
}
