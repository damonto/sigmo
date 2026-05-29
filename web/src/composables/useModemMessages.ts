import { computed, ref, watch, type ComputedRef, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { useMessageApi } from '@/apis/message'
import { formatListTimestamp } from '@/lib/datetime'
import { formatPhoneDisplay } from '@/lib/phoneNumberInput'
import type { MessageResponse } from '@/types/message'

export type ConversationItem = {
  key: string
  participantLabel: string
  participantValue: string
  participantSearchValue: string
  preview: string
  timestampLabel: string
}

export const useModemMessages = (
  modemId: ComputedRef<string>,
  defaultCountry?: ComputedRef<string>,
  searchQuery?: Readonly<Ref<string>>,
) => {
  const { t } = useI18n()
  const messageApi = useMessageApi()

  const conversations = ref<MessageResponse[]>([])
  const isLoading = ref(false)
  let requestID = 0

  const count = computed(() => conversations.value.length)
  const hasMessages = computed(() => conversations.value.length > 0)

  const getParticipantValue = (message: MessageResponse) => {
    const value = message.incoming ? message.sender : message.recipient
    return value.trim()
  }

  const getParticipantLabel = (message: MessageResponse) => {
    const participant = getParticipantValue(message)
    return (
      formatPhoneDisplay(participant, defaultCountry?.value) ||
      t('modemDetail.messages.unknownParticipant')
    )
  }

  const getParticipantSearchValue = (message: MessageResponse) => {
    const participant = getParticipantValue(message)
    const display = formatPhoneDisplay(participant, defaultCountry?.value)
    return [display, participant].join(' ').trim()
  }

  const items = computed<ConversationItem[]>(() =>
    conversations.value.map((message) => ({
      key: String(message.id),
      participantValue: getParticipantValue(message),
      participantLabel: getParticipantLabel(message),
      participantSearchValue: getParticipantSearchValue(message),
      preview: message.text,
      timestampLabel: formatListTimestamp(message.timestamp),
    })),
  )

  const currentSearchQuery = () => searchQuery?.value.trim() ?? ''

  const fetchMessages = async (id?: string, query?: string) => {
    const targetId = id ?? modemId.value
    if (!targetId || targetId === 'unknown') return
    const targetQuery = query ?? currentSearchQuery()
    const currentRequestID = ++requestID
    isLoading.value = true
    try {
      const { data } = await messageApi.getMessages(targetId, targetQuery)
      if (currentRequestID === requestID) {
        conversations.value = data.value ?? []
      }
    } finally {
      if (currentRequestID === requestID) {
        isLoading.value = false
      }
    }
  }

  const deleteConversation = async (participantValue: string) => {
    const targetId = modemId.value
    if (!targetId || targetId === 'unknown') return
    if (!participantValue.trim()) return
    await messageApi.deleteMessagesByParticipant(targetId, participantValue)
    await fetchMessages(targetId, currentSearchQuery())
  }

  watch(
    [modemId, () => currentSearchQuery()],
    async ([id, query]) => {
      if (!id || id === 'unknown') return
      await fetchMessages(id, query)
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
