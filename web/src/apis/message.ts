import { fetchJson } from '@/lib/fetch'

import type { MessagesResponse, SendMessageResponse } from '@/types/message'

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

  const sendMessage = (id: string, to: string, text: string) => {
    return fetchJson<SendMessageResponse>(`modems/${id}/messages`, {
      method: 'POST',
      body: JSON.stringify({ to, text }),
    })
  }

  return {
    getMessages,
    getMessagesByParticipant,
    deleteMessagesByParticipant,
    sendMessage,
  }
}
