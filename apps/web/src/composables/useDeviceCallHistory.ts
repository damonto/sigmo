import { computed, onBeforeUnmount, onMounted, ref, watch, type ComputedRef } from 'vue'
import { communicationApi } from '@/apis/communication'
import { sessionRevision } from '@/lib/authSession'
import type { CallRecord } from '@/types/call'

/** Device pages read their own history independently of the globally owned audio session. */
export const useDeviceCallHistory = (modemID: ComputedRef<string>) => {
  const recentCalls = ref<CallRecord[]>([])
  const isLoading = ref(false)
  const isDeletingCallID = ref('')
  const errorMessage = ref('')
  const query = ref('')
  const cursor = ref('')
  let generation = 0
  let expanded = false
  const loadCalls = async (more = false) => {
    if (more && (isLoading.value || !cursor.value)) return
    const current = ++generation,
      id = modemID.value,
      revision = sessionRevision()
    if (!id || id === 'unknown') {
      isLoading.value = false
      return
    }
    isLoading.value = true
    try {
      const page = await communicationApi.calls({
        modemId: id,
        q: query.value,
        cursor: more ? cursor.value : '',
      })
      if (current !== generation || revision !== sessionRevision()) return
      recentCalls.value = more ? [...recentCalls.value, ...page.items] : page.items
      cursor.value = page.nextCursor
      expanded = more
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
      await communicationApi.deleteCall(call.callID)
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
    [modemID, query],
    () => {
      recentCalls.value = []
      cursor.value = ''
      expanded = false
      errorMessage.value = ''
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
      cursor.value = ''
      expanded = false
      errorMessage.value = ''
      isLoading.value = false
      isDeletingCallID.value = ''
    },
    { flush: 'sync' },
  )
  const refresh = () => {
    if (document.visibilityState === 'visible' && !isLoading.value && !expanded) void loadCalls()
  }
  onMounted(() => window.addEventListener('sigmo:communication-changed', refresh))
  onBeforeUnmount(() => {
    generation++
    window.removeEventListener('sigmo:communication-changed', refresh)
  })
  return {
    recentCalls,
    hasRecentCalls: computed(() => recentCalls.value.length > 0),
    hasMore: computed(() => cursor.value !== ''),
    isLoading,
    isDeletingCallID,
    errorMessage,
    loadCalls,
    loadMoreCalls: () => loadCalls(true),
    deleteRecord,
    setSearchQuery: (value: string) => {
      query.value = value
    },
  }
}
