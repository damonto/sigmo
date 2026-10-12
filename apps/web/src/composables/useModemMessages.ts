import { computed, onMounted, onUnmounted, ref, watch, type ComputedRef, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { useMessageApi } from '@/apis/message'
import { formatListTimestamp } from '@/lib/datetime'
import { localizeErrorMessage } from '@/lib/errorHandler'
import { formatPhoneDisplay } from '@/lib/phoneNumberInput'
import type { Conversation } from '@/types/message'

/** Display values retain the stable SIM-specific identity for every action. */
export interface ConversationItem {
  key: string
  profileId: string
  participantLabel: string
  participantValue: string
  participantSearchValue: string
  preview: string
  timestampLabel: string
  lineLabel: string
  unreadCount: number
}

/** Polls visible history and applies backend-owned unread state. */
export const useModemMessages = (
  modemId: ComputedRef<string>,
  defaultCountry?: ComputedRef<string>,
  searchQuery?: Readonly<Ref<string>>,
) => {
  const { t } = useI18n()
  const messageApi = useMessageApi()
  const conversations = ref<Conversation[]>([])
  const isLoading = ref(false)
  const unreadOnly = ref(false)
  const historical = ref(false)
  const error = ref('')
  let requestID = 0
  let disposed = false

  const count = computed(() => conversations.value.length)
  const hasMessages = computed(() => count.value > 0)
  const unreadCount = computed(() =>
    conversations.value.reduce((sum, value) => sum + value.unreadCount, 0),
  )
  const items = computed<ConversationItem[]>(() =>
    conversations.value
      .filter((value) => !unreadOnly.value || value.unreadCount > 0)
      .map((value) => {
        const region =
          value.modemId === modemId.value && value.canSend ? defaultCountry?.value : undefined
        const display = formatPhoneDisplay(value.participant, region)
        return {
          key: value.id,
          profileId: value.profileId,
          participantValue: value.participant,
          participantLabel: display || t('modemDetail.messages.unknownParticipant'),
          participantSearchValue: `${display} ${value.participant}`,
          preview: value.latestMessage.text,
          timestampLabel: formatListTimestamp(value.latestMessage.timestamp),
          lineLabel:
            value.lineLabel ||
            formatPhoneDisplay(value.lineNumber, region) ||
            t('modemDetail.messages.lineSuffix', { suffix: value.profileId.slice(-6) }),
          unreadCount: value.unreadCount,
        }
      }),
  )

  const fetchMessages = async () => {
    const id = modemId.value
    if (disposed || !id || id === 'unknown') return
    const current = ++requestID
    isLoading.value = true
    try {
      const { data } = await messageApi.getConversations({
        modemId: historical.value ? undefined : id,
        q: searchQuery?.value.trim() ?? '',
        unassigned: historical.value,
      })
      if (current !== requestID || disposed) return
      conversations.value = data.value?.items ?? []
      error.value = ''
    } catch (err) {
      if (current === requestID && !disposed) error.value = localizeErrorMessage(err)
    } finally {
      if (current === requestID && !disposed) isLoading.value = false
    }
  }

  const deleteConversation = async (id: string) => {
    const generation = requestID
    await messageApi.deleteConversation(id)
    if (!disposed && generation === requestID) await fetchMessages()
  }

  watch(
    [modemId, () => searchQuery?.value, historical],
    ([id], previous) => {
      if (id !== previous?.[0]) conversations.value = []
      void fetchMessages()
    },
    { immediate: true },
  )

  const refreshVisible = () => {
    if (document.visibilityState === 'visible') void fetchMessages()
  }
  let timer: ReturnType<typeof setInterval> | undefined
  onMounted(() => {
    timer = setInterval(refreshVisible, 10_000)
    document.addEventListener('visibilitychange', refreshVisible)
    window.addEventListener('sigmo:messages-changed', refreshVisible)
  })
  onUnmounted(() => {
    disposed = true
    requestID++
    clearInterval(timer)
    document.removeEventListener('visibilitychange', refreshVisible)
    window.removeEventListener('sigmo:messages-changed', refreshVisible)
  })

  return {
    conversations,
    items,
    count,
    unreadCount,
    unreadOnly,
    historical,
    error,
    hasMessages,
    isLoading,
    fetchMessages,
    deleteConversation,
  }
}
