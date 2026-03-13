import { computed, ref } from 'vue'

import { useModemApi } from '@/apis/modem'
import type { Modem } from '@/types/modem'

// Global modems state
const modems = ref<Modem[]>([])
const isFetching = ref(false)

/**
 * Modem management composable
 * Error handling is centralized in lib/fetch.ts
 */
export const useModems = () => {
  const modemApi = useModemApi()

  /**
   * Fetch modems from API and update state
   */
  const fetchModems = async () => {
    if (isFetching.value) return

    isFetching.value = true
    try {
      const { data } = await modemApi.getModems()

      if (data.value) {
        modems.value = data.value
      }
    } finally {
      isFetching.value = false
    }
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
