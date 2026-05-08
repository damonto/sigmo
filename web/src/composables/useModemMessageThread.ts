import { computed, onMounted, onUnmounted, ref, watch, type ComputedRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import { useMessageApi } from '@/apis/message'
import { formatMessageTimestamp } from '@/lib/datetime'
import type { MessageResponse } from '@/types/message'

export type ThreadMessageItem = {
  key: string
  incoming: boolean
  text: string
  timestampLabel: string
  status: string
}

export const useModemMessageThread = ({
  modemId,
  participant,
  isNewConversation,
}: {
  modemId: ComputedRef<string>
  participant: ComputedRef<string>
  isNewConversation: ComputedRef<boolean>
}) => {
  const { t } = useI18n()
  const router = useRouter()
  const messageApi = useMessageApi()

  const threadMessages = ref<MessageResponse[]>([])
  const isLoading = ref(false)
  const isSending = ref(false)
  const isDeleting = ref(false)
  const messageDraft = ref('')
  const newRecipient = ref('')

  const participantLabel = computed(() => {
    if (isNewConversation.value) {
      const value = newRecipient.value.trim()
      return value.length > 0 ? value : t('modemDetail.messages.newConversation')
    }
    const value = participant.value.trim()
    return value.length > 0 ? value : t('modemDetail.messages.unknownParticipant')
  })

  const items = computed<ThreadMessageItem[]>(() =>
    threadMessages.value.map((message) => ({
      key: `${message.sender}-${message.recipient}-${message.timestamp}`,
      incoming: message.incoming,
      text: message.text,
      timestampLabel: formatMessageTimestamp(message.timestamp),
      status: message.status,
    })),
  )

  const fetchThreadMessages = async (id: string, target: string) => {
    if (isLoading.value) return
    if (isNewConversation.value) {
      threadMessages.value = []
      return
    }
    if (!target.trim()) {
      threadMessages.value = []
      return
    }
    isLoading.value = true
    try {
      const { data } = await messageApi.getMessagesByParticipant(id, target)
      threadMessages.value = data.value ?? []
    } finally {
      isLoading.value = false
    }
  }

  const deleteThread = async () => {
    const targetId = modemId.value
    const targetParticipant = participant.value.trim()
    if (!targetId || targetId === 'unknown') return
    if (!targetParticipant) return
    isDeleting.value = true
    try {
      await messageApi.deleteMessagesByParticipant(targetId, targetParticipant)
      await router.push({ name: 'modem-messages', params: { id: targetId } })
    } catch (err) {
      console.error('[useModemMessageThread] Failed to delete messages:', err)
    } finally {
      isDeleting.value = false
    }
  }

  const sendMessage = async () => {
    const targetId = modemId.value
    if (!targetId || targetId === 'unknown') return
    const target = isNewConversation.value ? newRecipient.value.trim() : participant.value.trim()
    const text = messageDraft.value.trim()
    if (!target || !text || isSending.value) return
    messageDraft.value = ''
    isSending.value = true
    try {
      await messageApi.sendMessage(targetId, target, text)
      if (isNewConversation.value) {
        await router.replace({
          name: 'modem-message-thread',
          params: { id: targetId, participant: target },
        })
        newRecipient.value = ''
      }
      await fetchThreadMessages(targetId, target)
    } catch (err) {
      console.error('[useModemMessageThread] Failed to send message:', err)
    } finally {
      isSending.value = false
    }
  }

  watch(
    [modemId, participant],
    async ([id, target]) => {
      if (!id || id === 'unknown') return
      await fetchThreadMessages(id, target)
    },
    { immediate: true },
  )

  let refreshTimer: ReturnType<typeof setInterval> | null = null

  onMounted(() => {
    refreshTimer = setInterval(() => {
      const id = modemId.value
      if (!id || id === 'unknown') return
      void fetchThreadMessages(id, participant.value)
    }, 10000)
  })

  onUnmounted(() => {
    if (refreshTimer) {
      clearInterval(refreshTimer)
      refreshTimer = null
    }
  })

  return {
    items,
    isLoading,
    isSending,
    isDeleting,
    messageDraft,
    newRecipient,
    participantLabel,
    sendMessage,
    deleteThread,
  }
}
