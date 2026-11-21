import { computed, ref } from 'vue'

import { useCapabilityApi } from '@/apis/capability'

export const FEATURE = {
  esimTransfer: 'esimTransfer',
  volte: 'volte',
  wifiCalling: 'wifiCalling',
} as const

const features = ref<string[]>([])
const isLoading = ref(false)
const hasLoaded = ref(false)
let pending: Promise<void> | undefined
let refreshRequested = false

export const useCapabilities = () => {
  const capabilityApi = useCapabilityApi()

  const featureSet = computed(() => new Set(features.value))

  const hasFeature = (feature: string) => featureSet.value.has(feature)

  const fetchCapabilities = (force = false): Promise<void> => {
    if (force) refreshRequested = true
    // A forced refresh arriving during a request is included in the shared task.
    if (pending) return pending
    if (hasLoaded.value && !force) return Promise.resolve()
    isLoading.value = true
    pending = refreshCapabilities()
    return pending
  }

  const refreshCapabilities = async () => {
    try {
      do {
        refreshRequested = false
        await loadCapabilities()
      } while (refreshRequested)
    } finally {
      pending = undefined
      isLoading.value = false
    }
  }

  const loadCapabilities = async () => {
    try {
      const { data } = await capabilityApi.getCapabilities()
      // Authorization changed while this response was in flight. Wait for the
      // queued refresh before publishing capabilities from the current state.
      if (!refreshRequested) {
        features.value = data.value?.features ?? []
        hasLoaded.value = true
      }
    } catch (err) {
      if (!refreshRequested) {
        console.error('[useCapabilities] Failed to fetch capabilities:', err)
        features.value = []
      }
    }
  }

  return {
    features,
    isLoading,
    hasFeature,
    fetchCapabilities,
  }
}
