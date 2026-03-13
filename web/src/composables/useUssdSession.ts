import { computed, ref, watch, type ComputedRef } from 'vue'

import { useUssdApi } from '@/apis/ussd'
import type { UssdAction } from '@/types/ussd'

type UssdEntry = {
  id: string
  text: string
  incoming: boolean
  timestamp: string
}

export type UssdMessageItem = {
  id: string
  text: string
  incoming: boolean
  timestampLabel: string
}

const formatTimestamp = (value: string) => {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

const createEntry = (text: string, incoming: boolean): UssdEntry => ({
  id: `${Date.now()}-${Math.random().toString(16).slice(2)}`,
  text,
  incoming,
  timestamp: new Date().toISOString(),
})

export const useUssdSession = (modemId: ComputedRef<string>) => {
  const ussdApi = useUssdApi()

  const entries = ref<UssdEntry[]>([])
  const draft = ref('')
  const isSending = ref(false)
  const isSessionActive = ref(false)

  const hasEntries = computed(() => entries.value.length > 0)

  const items = computed<UssdMessageItem[]>(() =>
    entries.value.map((entry) => ({
      id: entry.id,
      text: entry.text,
      incoming: entry.incoming,
      timestampLabel: formatTimestamp(entry.timestamp),
    })),
  )

  const resetSession = () => {
    entries.value = []
    draft.value = ''
    isSessionActive.value = false
  }

  const sendMessage = async () => {
    const targetId = modemId.value
    if (!targetId || targetId === 'unknown') return
    const code = draft.value.trim()
    if (!code || isSending.value) return
    const action: UssdAction = isSessionActive.value ? 'reply' : 'initialize'
    entries.value.push(createEntry(code, false))
    draft.value = ''
    isSending.value = true
    try {
      const { data } = await ussdApi.executeUssd(targetId, action, code)
      const reply = data.value?.reply
      if (reply) {
        entries.value.push(createEntry(reply, true))
      }
      isSessionActive.value = true
    } catch (err) {
      console.error('[useUssdSession] Failed to send USSD:', err)
    } finally {
      isSending.value = false
    }
  }

  watch(
    modemId,
    (id) => {
      if (!id || id === 'unknown') return
      resetSession()
    },
    { immediate: true },
  )

  return {
    items,
    draft,
    isSending,
    hasEntries,
    resetSession,
    sendMessage,
  }
}
