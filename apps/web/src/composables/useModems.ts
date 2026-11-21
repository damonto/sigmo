import { computed, getCurrentScope, onScopeDispose, ref, shallowRef, watch } from 'vue'

import { useModemApi } from '@/apis/modem'
import { createSIMKindRetry } from '@/composables/simKindRetry'
import { useAppUpdate } from '@/composables/useAppUpdate'
import { sessionRevision } from '@/lib/authSession'
import type { Modem } from '@/types/modem'

// Global modems state
const modems = ref<Modem[]>([])
const pendingRequest = shallowRef<{ revision: number }>()
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
  const revision = sessionRevision()
  if (updating.value || pendingRequest.value?.revision === revision) return

  const request = { revision }
  pendingRequest.value = request
  try {
    const { data } = await modemApi.getModems({ quiet })

    if (pendingRequest.value === request && revision === sessionRevision() && data.value) {
      modems.value = data.value
    }
  } finally {
    if (pendingRequest.value === request) {
      pendingRequest.value = undefined
      scheduleSIMKindRetry()
    }
  }
}

const fetchModems = () => {
  simKindRetry.reset()
  return loadModems()
}

/** Refreshes the list after a mutation and discards earlier list requests. */
export const refreshModemList = (): Promise<void> => {
  pendingRequest.value = undefined
  return fetchModems()
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
  const isLoading = computed(() => pendingRequest.value !== undefined)

  return {
    // State
    modems,
    isLoading,

    // Actions
    fetchModems,
    getModemById,
  }
}
