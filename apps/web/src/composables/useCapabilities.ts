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

export const useCapabilities = () => {
  const capabilityApi = useCapabilityApi()

  const featureSet = computed(() => new Set(features.value))

  const hasFeature = (feature: string) => featureSet.value.has(feature)

  const fetchCapabilities = async () => {
    if (hasLoaded.value) return
    // Split views mount together; every consumer must await the same capability read.
    if (pending) return pending

    isLoading.value = true
    pending = loadCapabilities()
    try {
      await pending
    } finally {
      pending = undefined
      isLoading.value = false
    }
  }

  const loadCapabilities = async () => {
    try {
      const { data } = await capabilityApi.getCapabilities()
      features.value = data.value?.features ?? []
      hasLoaded.value = true
    } catch (err) {
      console.error('[useCapabilities] Failed to fetch capabilities:', err)
      features.value = []
    }
  }

  return {
    features,
    isLoading,
    hasFeature,
    fetchCapabilities,
  }
}
