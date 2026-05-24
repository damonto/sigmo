import { computed, ref } from 'vue'

import { useCapabilityApi } from '@/apis/capability'

export const FEATURE = {
  esimTransfer: 'esimTransfer',
  wifiCalling: 'wifiCalling',
} as const

export const useCapabilities = () => {
  const capabilityApi = useCapabilityApi()
  const features = ref<string[]>([])
  const isLoading = ref(false)

  const featureSet = computed(() => new Set(features.value))

  const hasFeature = (feature: string) => featureSet.value.has(feature)

  const fetchCapabilities = async () => {
    if (isLoading.value) return

    isLoading.value = true
    try {
      const { data } = await capabilityApi.getCapabilities()
      features.value = data.value?.features ?? []
    } catch (err) {
      console.error('[useCapabilities] Failed to fetch capabilities:', err)
      features.value = []
    } finally {
      isLoading.value = false
    }
  }

  return {
    features,
    isLoading,
    hasFeature,
    fetchCapabilities,
  }
}
