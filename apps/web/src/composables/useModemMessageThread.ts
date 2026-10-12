import { computed, onMounted, onUnmounted, ref, watch, type ComputedRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import { useMessageApi } from '@/apis/message'
import { formatListTimestamp } from '@/lib/datetime'
import { localizeErrorMessage } from '@/lib/errorHandler'
import { formatPhoneDisplay, normalizeAddressSubmission } from '@/lib/phoneNumberInput'
import type { Conversation, MessageResponse } from '@/types/message'

/** A message rendered by the timeline; key is the acknowledged backend ID. */
export interface ThreadMessageItem {
  key: string
  incoming: boolean
  text: string
  timestampLabel: string
  status: string
  wifiCalling: boolean
}

/** Keeps a draft and sends bound to the conversation's SIM, including inactive history. */
export const useModemMessageThread = ({
  modemId,
  participant,
  isNewConversation,
  defaultCountry,
  conversationId,
  activeProfileId,
  profileId,
}: {
  modemId: ComputedRef<string>
  participant: ComputedRef<string>
  isNewConversation: ComputedRef<boolean>
  activeProfileId: ComputedRef<string>
  defaultCountry?: ComputedRef<string>
  conversationId?: ComputedRef<string>
  profileId?: ComputedRef<string>
}) => {
  const { t } = useI18n()
  const router = useRouter()
  const api = useMessageApi()
  const threadMessages = ref<MessageResponse[]>([])
  const conversation = ref<Conversation | null>(null)
  const isLoading = ref(false)
  const isSending = ref(false)
  const isDeleting = ref(false)
  const messageDraft = ref('')
  const newRecipient = ref('')
  const error = ref('')
  const sendError = ref('')
  const missing = ref(false)
  let generation = 0
  let requestID = 0
  let disposed = false
  let acknowledged = 0
  const drafts = new Map<string, string>()
  const identity = computed(
    () =>
      `${modemId.value}:${conversationId?.value || profileId?.value || activeProfileId.value}:${participant.value}`,
  )
  const selectedProfile = computed(
    () => conversation.value?.profileId || profileId?.value || activeProfileId.value,
  )
  const canSend = computed(
    () =>
      !missing.value &&
      !!selectedProfile.value &&
      selectedProfile.value === activeProfileId.value &&
      (conversation.value?.canSend ?? true),
  )
  const lineLabel = computed(
    () =>
      conversation.value?.lineLabel ||
      conversation.value?.lineNumber ||
      t('modemDetail.messages.lineSuffix', { suffix: selectedProfile.value.slice(-6) }),
  )
  const participantLabel = computed(() => {
    const value = isNewConversation.value ? newRecipient.value.trim() : participant.value.trim()
    return value
      ? formatPhoneDisplay(value, defaultCountry?.value)
      : t(
          isNewConversation.value
            ? 'modemDetail.messages.newConversation'
            : 'modemDetail.messages.unknownParticipant',
        )
  })
  const items = computed<ThreadMessageItem[]>(() =>
    threadMessages.value.map((message) => ({
      key: String(message.id),
      incoming: message.incoming,
      text: message.text,
      timestampLabel: formatListTimestamp(message.timestamp),
      status: message.status,
      wifiCalling: message.wifiCalling,
    })),
  )

  const fetchThreadMessages = async () => {
    if (disposed || isNewConversation.value || isSending.value) return
    const id = modemId.value
    if (!id || id === 'unknown' || !participant.value.trim()) return
    const current = generation
    const request = ++requestID
    const target = conversationId?.value
    const profile = selectedProfile.value
    const remote = participant.value.trim()
    isLoading.value = true
    try {
      const { data } = await api.getConversations(target ? {} : { modemId: id, profileId: profile })
      if (current !== generation || request !== requestID || disposed) return
      const selected = data.value?.items.find((item) =>
        target ? item.id === target : item.profileId === profile && item.participant === remote,
      )
      conversation.value = selected ?? null
      missing.value = !!target && !selected
      if (!selected) {
        threadMessages.value = []
        error.value = missing.value ? t('modemDetail.messages.deleted') : ''
        return
      }
      const result = await api.getConversationMessages(selected.id)
      if (current !== generation || request !== requestID || disposed) return
      threadMessages.value = result.data.value ?? []
      acknowledged = Math.max(acknowledged, selected.readThroughMessageId)
      error.value = ''
    } catch (err) {
      if (current === generation && request === requestID && !disposed)
        error.value = localizeErrorMessage(err)
    } finally {
      if (current === generation && request === requestID && !disposed) isLoading.value = false
    }
  }

  const markDisplayed = async (id: number) => {
    const selected = conversation.value
    if (
      !selected ||
      document.visibilityState !== 'visible' ||
      (id <= acknowledged && !selected.markedUnread)
    )
      return
    if (!threadMessages.value.some((message) => message.id === id)) return
    const current = generation
    try {
      await api.updateConversation(selected.id, { readThroughMessageId: id, markedUnread: false })
      if (current !== generation || disposed) return
      acknowledged = Math.max(acknowledged, id)
      selected.markedUnread = false
      window.dispatchEvent(new Event('sigmo:messages-changed'))
    } catch {
      /* The next visible snapshot retries this watermark. */
    }
  }

  const deleteThread = async () => {
    const id = conversation.value?.id
    if (!id || isDeleting.value) return
    const current = generation
    const modem = modemId.value
    isDeleting.value = true
    try {
      await api.deleteConversation(id)
      if (current !== generation || disposed) return
      drafts.delete(identity.value)
      await router.push({ name: 'modem-messages', params: { id: modem } })
    } catch (err) {
      if (current === generation && !disposed) error.value = localizeErrorMessage(err)
    } finally {
      if (current === generation && !disposed) isDeleting.value = false
    }
  }

  const sendMessage = async () => {
    const id = modemId.value
    const target = isNewConversation.value
      ? normalizeAddressSubmission(newRecipient.value)
      : participant.value.trim()
    const text = messageDraft.value.trim()
    const profile = selectedProfile.value
    if (!id || id === 'unknown' || !target || !text || !canSend.value || isSending.value) return
    const current = generation
    const creating = isNewConversation.value
    isSending.value = true
    sendError.value = ''
    try {
      const { data } = await api.sendMessage(id, target, text, profile)
      if (current !== generation || disposed) return
      messageDraft.value = ''
      drafts.delete(identity.value)
      isSending.value = false
      const sentTo = data.value?.to?.trim() || target
      if (creating) {
        await router.replace({
          name: 'modem-message-thread',
          params: { id, participant: sentTo },
          query: { conversation: data.value?.conversationId, profile },
        })
        if (current === generation) newRecipient.value = ''
      } else {
        await fetchThreadMessages()
      }
      window.dispatchEvent(new Event('sigmo:messages-changed'))
    } catch (err) {
      if (current !== generation || disposed) return
      const status =
        typeof err === 'object' && err !== null && 'status' in err ? err.status : undefined
      sendError.value =
        typeof status === 'number' && status >= 400 && status < 500 && status !== 408
          ? localizeErrorMessage(err)
          : t('modemDetail.messages.resultUnknown')
      isSending.value = false
      await fetchThreadMessages()
    } finally {
      if (current === generation && !disposed) isSending.value = false
    }
  }

  watch(
    identity,
    (value, previous) => {
      if (previous) drafts.set(previous, messageDraft.value)
      generation++
      requestID++
      acknowledged = 0
      threadMessages.value = []
      conversation.value = null
      messageDraft.value = drafts.get(value) ?? ''
      isLoading.value = false
      isSending.value = false
      isDeleting.value = false
      missing.value = false
      error.value = ''
      sendError.value = ''
      void fetchThreadMessages()
    },
    { immediate: true },
  )

  const refreshVisible = () => {
    if (document.visibilityState === 'visible') void fetchThreadMessages()
  }
  let timer: ReturnType<typeof setInterval> | undefined
  onMounted(() => {
    timer = setInterval(refreshVisible, 10_000)
    document.addEventListener('visibilitychange', refreshVisible)
  })
  onUnmounted(() => {
    disposed = true
    generation++
    clearInterval(timer)
    document.removeEventListener('visibilitychange', refreshVisible)
    drafts.clear()
  })

  return {
    items,
    isLoading,
    isSending,
    isDeleting,
    messageDraft,
    newRecipient,
    participantLabel,
    canSend,
    lineLabel,
    error,
    sendError,
    markDisplayed,
    sendMessage,
    deleteThread,
    fetchThreadMessages,
  }
}
