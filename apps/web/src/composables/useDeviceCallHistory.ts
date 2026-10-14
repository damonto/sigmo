import { computed, onBeforeUnmount, onMounted, ref, watch, type ComputedRef } from 'vue'
import { communicationApi } from '@/apis/communication'
import { useCapabilities } from '@/composables/useCapabilities'
import { useCallApi } from '@/apis/call'
import { sessionRevision } from '@/lib/authSession'
import type { CallRecord } from '@/types/call'

/** Device pages read their own history independently of the globally owned audio session. */
export const useDeviceCallHistory = (modemID: ComputedRef<string>) => {
  const api = useCallApi()
  const capabilities = useCapabilities()
  const recentCalls = ref<CallRecord[]>([])
  const isLoading = ref(false)
  const isDeletingCallID = ref('')
  const errorMessage = ref('')
  const query = ref('')
  let generation = 0
  const loadCalls = async () => {
    const current = ++generation,
      id = modemID.value,
      revision = sessionRevision()
    if (!id || id === 'unknown') return
    isLoading.value = true
    try {
      const records = capabilities.hasFeature('callHistory')
        ? (await communicationApi.calls({ modemId: id, q: query.value })).items
        : ((await api.listCalls(id, query.value)).data.value ?? [])
      if (current !== generation || revision !== sessionRevision()) return
      recentCalls.value = records
      errorMessage.value = ''
    } catch (error) {
      if (current === generation && revision === sessionRevision())
        errorMessage.value = error instanceof Error ? error.message : String(error)
    } finally {
      if (current === generation) isLoading.value = false
    }
  }
  const deleteRecord = async (call: CallRecord): Promise<boolean> => {
    if (isDeletingCallID.value) return false
    const id = modemID.value,
      revision = sessionRevision()
    isDeletingCallID.value = call.callID
    try {
      if (capabilities.hasFeature('callHistory')) await communicationApi.deleteCall(call.callID)
      else await api.deleteCall(id, call.callID)
      if (id !== modemID.value || revision !== sessionRevision()) return false
      recentCalls.value = recentCalls.value.filter((item) => item.callID !== call.callID)
      return true
    } catch (error) {
      if (id === modemID.value && revision === sessionRevision())
        errorMessage.value = error instanceof Error ? error.message : String(error)
      return false
    } finally {
      if (id === modemID.value) isDeletingCallID.value = ''
    }
  }
  watch(
    [modemID, query, () => capabilities.hasFeature('callHistory')],
    () => {
      recentCalls.value = []
      isDeletingCallID.value = ''
      void loadCalls()
    },
    { immediate: true },
  )
  watch(
    sessionRevision,
    () => {
      generation++
      recentCalls.value = []
      isLoading.value = false
      isDeletingCallID.value = ''
    },
    { flush: 'sync' },
  )
  const refresh = () => {
    if (document.visibilityState === 'visible' && !isLoading.value) void loadCalls()
  }
  onMounted(() => window.addEventListener('sigmo:communication-changed', refresh))
  onBeforeUnmount(() => {
    generation++
    window.removeEventListener('sigmo:communication-changed', refresh)
  })
  return {
    recentCalls,
    hasRecentCalls: computed(() => recentCalls.value.length > 0),
    isLoading,
    isDeletingCallID,
    errorMessage,
    loadCalls,
    deleteRecord,
    setSearchQuery: (value: string) => {
      query.value = value
    },
  }
}
