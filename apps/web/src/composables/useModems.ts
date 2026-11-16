import { computed, getCurrentScope, onScopeDispose, ref, watch } from 'vue'

import { useModemApi } from '@/apis/modem'
import { createSIMKindRetry } from '@/composables/simKindRetry'
import { useAppUpdate } from '@/composables/useAppUpdate'
import type { Modem } from '@/types/modem'

// Global modems state
const modems = ref<Modem[]>([])
const isFetching = ref(false)
const simKindRetry = createSIMKindRetry()
const modemApi = useModemApi()
const { updating } = useAppUpdate()
let consumers = 0

const scheduleSIMKindRetry = () => {
  const pending =
    consumers > 0 && !updating.value && modems.value.some((modem) => modem.simKind === 'unknown')
  simKindRetry.schedule(pending, () => {
    // Retain cached inventory after background failures and let the next retry run.
    void loadModems(true).catch(() => undefined)
  })
}

const loadModems = async (quiet = false) => {
  if (isFetching.value || updating.value) return

  isFetching.value = true
  try {
    const { data } = await modemApi.getModems({ quiet })

    if (data.value) {
      modems.value = data.value
    }
  } finally {
    isFetching.value = false
    scheduleSIMKindRetry()
  }
}

const fetchModems = async () => {
  simKindRetry.reset()
  await loadModems()
}

/**
 * Modem management composable
 * Error handling is centralized in lib/fetch.ts
 */
export const useModems = () => {
  if (getCurrentScope()) {
    consumers++
    if (consumers === 1) scheduleSIMKindRetry()
    watch(updating, scheduleSIMKindRetry, { flush: 'sync' })
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
