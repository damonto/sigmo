import { useFetch } from '@/lib/fetch'

import type { MessagesResponse, SendMessageResponse } from '@/types/message'

export const useMessageApi = () => {
  const getMessages = (id: string) => {
    return useFetch<MessagesResponse>(`modems/${id}/messages`).get().json()
  }

  const getMessagesByParticipant = (id: string, participant: string) => {
    const encoded = encodeURIComponent(participant)
    return useFetch<MessagesResponse>(`modems/${id}/messages/${encoded}`).get().json()
  }

  const deleteMessagesByParticipant = (id: string, participant: string) => {
    const encoded = encodeURIComponent(participant)
    return useFetch<void>(`modems/${id}/messages/${encoded}`, {
      method: 'DELETE',
    }).json()
  }

  const sendMessage = (id: string, to: string, text: string) => {
    return useFetch<SendMessageResponse>(`modems/${id}/messages`, {
      method: 'POST',
      body: JSON.stringify({ to, text }),
    }).json()
  }

  return {
    getMessages,
    getMessagesByParticipant,
    deleteMessagesByParticipant,
    sendMessage,
  }
}
