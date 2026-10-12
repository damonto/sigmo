import { ref } from 'vue'

import { useAppApi } from '@/apis/app'

const version = ref('')
const isLoading = ref(false)
const hasLoaded = ref(false)

export const useAppInfo = () => {
  const appApi = useAppApi()

  const fetchAppInfo = async () => {
    if (isLoading.value || hasLoaded.value) return

    isLoading.value = true
    try {
      const { data } = await appApi.getAppInfo()
      version.value = data.value?.version ?? ''
      hasLoaded.value = true
    } catch (err) {
      console.error('[useAppInfo] Failed to fetch app info:', err)
      version.value = ''
    } finally {
      isLoading.value = false
    }
  }

  return {
    version,
    isLoading,
    fetchAppInfo,
  }
}
