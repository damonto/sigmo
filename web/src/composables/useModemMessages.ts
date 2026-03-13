import { computed, ref, watch, type ComputedRef } from 'vue'
import { useI18n } from 'vue-i18n'

import { useMessageApi } from '@/apis/message'
import type { MessageResponse } from '@/types/message'

export type ConversationItem = {
  key: string
  participantLabel: string
  participantValue: string
  preview: string
  timestampLabel: string
}

const formatTimestamp = (value: string) => {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

export const useModemMessages = (modemId: ComputedRef<string>) => {
  const { t } = useI18n()
  const messageApi = useMessageApi()

  const conversations = ref<MessageResponse[]>([])
  const isLoading = ref(false)

  const count = computed(() => conversations.value.length)
  const hasMessages = computed(() => conversations.value.length > 0)

  const getParticipantValue = (message: MessageResponse) => {
    const value = message.incoming ? message.sender : message.recipient
    return value.trim()
  }

  const getParticipantLabel = (message: MessageResponse) => {
    const participant = getParticipantValue(message)
    return participant || t('modemDetail.messages.unknownParticipant')
  }

  const items = computed<ConversationItem[]>(() =>
    conversations.value.map((message) => ({
      key: `${message.sender}-${message.recipient}-${message.timestamp}`,
      participantValue: getParticipantValue(message),
      participantLabel: getParticipantLabel(message),
      preview: message.text,
      timestampLabel: formatTimestamp(message.timestamp),
    })),
  )

  const fetchMessages = async (id?: string) => {
    const targetId = id ?? modemId.value
    if (!targetId || targetId === 'unknown') return
    if (isLoading.value) return
    isLoading.value = true
    try {
      const { data } = await messageApi.getMessages(targetId)
      conversations.value = data.value ?? []
    } finally {
      isLoading.value = false
    }
  }

  const deleteConversation = async (participantValue: string) => {
    const targetId = modemId.value
    if (!targetId || targetId === 'unknown') return
    if (!participantValue.trim()) return
    await messageApi.deleteMessagesByParticipant(targetId, participantValue)
    await fetchMessages(targetId)
  }

  watch(
    modemId,
    async (id) => {
      if (!id || id === 'unknown') return
      await fetchMessages(id)
    },
    { immediate: true },
  )

  return {
    conversations,
    items,
    count,
    hasMessages,
    isLoading,
    fetchMessages,
    deleteConversation,
  }
}
