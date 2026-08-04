import { computed, getCurrentScope, onScopeDispose, ref } from 'vue'

import { useModemApi } from '@/apis/modem'
import { createSIMKindRetry } from '@/composables/simKindRetry'
import type { Modem } from '@/types/modem'

// Global modems state
const modems = ref<Modem[]>([])
const isFetching = ref(false)
const simKindRetry = createSIMKindRetry()
const modemApi = useModemApi()
let consumers = 0

const scheduleSIMKindRetry = () => {
  const pending = consumers > 0 && modems.value.some((modem) => modem.simKind === 'unknown')
  simKindRetry.schedule(pending, () => void fetchModems(false))
}

const fetchModems = async (restartSIMKindRetry = true) => {
  if (restartSIMKindRetry) simKindRetry.reset()
  if (isFetching.value) return

  isFetching.value = true
  try {
    const { data } = await modemApi.getModems()

    if (data.value) {
      modems.value = data.value
    }
  } finally {
    isFetching.value = false
    scheduleSIMKindRetry()
  }
}

/**
 * Modem management composable
 * Error handling is centralized in lib/fetch.ts
 */
export const useModems = () => {
  if (getCurrentScope()) {
    consumers++
    if (consumers === 1) scheduleSIMKindRetry()
    onScopeDispose(() => {
      consumers--
      if (consumers === 0) simKindRetry.reset()
    })
  }

  /**
   * Get modem by ID
   */
  const getModemById = (id: string) => modems.value.find((modem) => modem.id === id) ?? null

  /**
   * Computed properties
   */
  const isLoading = computed(() => isFetching.value)

  return {
    // State
    modems,
    isLoading,

    // Actions
    fetchModems,
    getModemById,
  }
}
