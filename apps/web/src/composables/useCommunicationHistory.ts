import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { sessionRevision } from '@/lib/authSession'
import { useRoute } from 'vue-router'
import { communicationApi } from '@/apis/communication'
import { useContacts } from '@/composables/useContacts'
import { useCapabilities } from '@/composables/useCapabilities'
import { formatPhoneDisplay } from '@/lib/phoneNumberInput'
import type { MessageThread, ThreadMessage, SIMLine } from '@/types/communication'
import type { CallRecord } from '@/types/call'

/** A page's generation prevents old searches and routes from replacing current history. */
export const useCommunicationHistory = (kind: 'messages' | 'thread' | 'calls') => {
  const route = useRoute()
  const contacts = useContacts()
  const capabilities = useCapabilities()
  const supported = computed(() =>
    capabilities.hasFeature(kind === 'calls' ? 'callHistory' : 'messageThreads'),
  )
  const search = ref('')
  const loading = ref(true)
  const error = ref('')
  const cursor = ref('')
  const threads = ref<MessageThread[]>([])
  const messages = ref<ThreadMessage[]>([])
  const calls = ref<CallRecord[]>([])
  const missedOnly = ref(false)
  const unreadOnly = ref(false)
  const visibleThreads = computed(() =>
    unreadOnly.value
      ? threads.value.filter((thread) => thread.unreadCount > 0 || thread.markedUnread)
      : threads.value,
  )
  const lines = ref<SIMLine[]>([])
  const linesByProfileId = computed(
    () => new Map(lines.value.map((line) => [line.profileId, line])),
  )
  const sourceLine = (profileID: string) => linesByProfileId.value.get(profileID)
  const thread = ref<MessageThread>()
  const threadID = computed(() =>
    kind === 'thread' && typeof route.params.threadId === 'string' ? route.params.threadId : '',
  )
  const participant = computed(() => thread.value?.participant ?? '')
  const formatNumber = (number: string, profileID = '') =>
    formatPhoneDisplay(number, sourceLine(profileID)?.region)
  const visibleCalls = computed(() =>
    missedOnly.value
      ? calls.value.filter(
          (call) => call.direction === 'incoming' && !!call.endedAt && !call.answeredAt,
        )
      : calls.value,
  )
  let generation = 0
  let disposed = false
  const acknowledgements = new Set<ThreadMessage>()
  watch(
    sessionRevision,
    () => {
      generation++
      threads.value = []
      messages.value = []
      calls.value = []
      cursor.value = ''
      lines.value = []
      thread.value = undefined
      error.value = ''
      acknowledgements.clear()
      loading.value = false
    },
    { flush: 'sync' },
  )
  let expanded = false
  const loadMetadata = async (current: number) => {
    // Names and line labels enrich history; unavailable metadata must not hide it.
    const [, inventory] = await Promise.allSettled([contacts.load(), communicationApi.lines(true)])
    if (current === generation && inventory.status === 'fulfilled')
      lines.value = inventory.value.items
  }
  const load = async (more = false) => {
    if (disposed) return
    expanded = more
    const current = ++generation
    loading.value = true
    error.value = ''
    try {
      await capabilities.fetchCapabilities()
      if (!supported.value || current !== generation) return
      void loadMetadata(current)
      if (kind === 'calls') {
        const page = await communicationApi.calls({
          q: search.value,
          missed: missedOnly.value ? 'true' : '',
          cursor: more ? cursor.value : '',
        })
        if (current !== generation) return
        calls.value = more ? [...calls.value, ...page.items] : page.items
        cursor.value = page.nextCursor
      } else if (threadID.value) {
        const detail = await communicationApi.thread(threadID.value)
        if (current !== generation) return
        thread.value = detail
        const page = await communicationApi.messages(threadID.value, more ? cursor.value : '')
        if (current !== generation) return
        messages.value = more ? [...page.items, ...messages.value] : page.items
        cursor.value = page.nextCursor
      } else {
        const page = await communicationApi.threads({
          q: search.value,
          cursor: more ? cursor.value : '',
        })
        if (current !== generation) return
        threads.value = more ? [...threads.value, ...page.items] : page.items
        cursor.value = page.nextCursor
      }
    } catch (reason) {
      if (current === generation)
        error.value = reason instanceof Error ? reason.message : String(reason)
    } finally {
      if (current === generation) loading.value = false
    }
  }
  const deleteCurrent = async (id: string) => {
    const revision = sessionRevision()
    const sourceThread = threadID.value
    const isCurrent = () =>
      !disposed && revision === sessionRevision() && sourceThread === threadID.value
    try {
      if (kind === 'calls') await communicationApi.deleteCall(id)
      else await communicationApi.deleteThread(id)
      if (!isCurrent()) return false
      if (kind !== 'thread') await load()
      return true
    } catch (reason) {
      if (isCurrent()) error.value = reason instanceof Error ? reason.message : String(reason)
      return false
    }
  }
  const acknowledge = async (id: number) => {
    const thread = threadID.value
    const message = messages.value.find((item) => item.id === id)
    if (
      !thread ||
      !message ||
      message.read ||
      acknowledgements.has(message) ||
      !message.incoming ||
      document.visibilityState !== 'visible'
    )
      return
    const current = generation
    acknowledgements.add(message)
    try {
      await communicationApi.read(thread, [id])
      if (current === generation) message.read = true
    } catch (reason) {
      if (current === generation)
        error.value = reason instanceof Error ? reason.message : String(reason)
    } finally {
      acknowledgements.delete(message)
    }
  }
  watch(
    [threadID, search, missedOnly],
    async (_, __, cleanup) => {
      let cancelled = false
      cleanup(() => {
        cancelled = true
      })
      generation++
      loading.value = true
      threads.value = []
      calls.value = []
      messages.value = []
      thread.value = undefined
      cursor.value = ''
      await nextTick()
      if (!cancelled) await load()
    },
    { immediate: true },
  )
  let refreshTimer: ReturnType<typeof setInterval> | undefined
  const refreshVisible = () => {
    if (document.visibilityState === 'visible' && !loading.value && !expanded) void load()
  }
  onMounted(() => {
    window.addEventListener('sigmo:communication-changed', refreshVisible)
    document.addEventListener('visibilitychange', refreshVisible)
    refreshTimer = setInterval(refreshVisible, 15_000)
  })
  onBeforeUnmount(() => {
    disposed = true
    generation++
    clearInterval(refreshTimer)
    window.removeEventListener('sigmo:communication-changed', refreshVisible)
    document.removeEventListener('visibilitychange', refreshVisible)
  })
  return {
    lines,
    search,
    loading,
    error,
    cursor,
    threads,
    visibleThreads,
    unreadOnly,
    messages,
    visibleCalls,
    missedOnly,
    threadID,
    participant,
    participantLabel: computed(() =>
      formatNumber(participant.value, thread.value?.latestMessage.profileId),
    ),
    originalModemID: computed(() => thread.value?.latestMessage.modemId ?? ''),
    supported,
    load,
    deleteCurrent,
    acknowledge,
    formatNumber,
    sourceLabel: (profileID: string, label = '') => {
      const line = sourceLine(profileID)
      return (
        label || line?.label || formatPhoneDisplay(line?.number ?? '', line?.region) || profileID
      )
    },
    sourceLine,
    lineDeleted: (id: string) =>
      ['deleted', 'assigned'].includes(sourceLine(id)?.deletionState ?? ''),
    match: contacts.match,
  }
}
