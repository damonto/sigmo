import { fetchJson } from '@/lib/fetch'

import type { MessagesResponse, SendMessageResponse } from '@/types/message'

export const useMessageApi = () => {
  const getMessages = (id: string) => {
    return fetchJson<MessagesResponse>(`modems/${id}/messages`)
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
