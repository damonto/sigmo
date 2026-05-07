import { ref } from 'vue'

import { useConfigApi } from '@/apis/config'
import type { ConfigResponse, ConfigValues } from '@/types/config'

const cloneValues = (values: ConfigValues): ConfigValues => {
  return JSON.parse(JSON.stringify(values)) as ConfigValues
}

export const useConfig = () => {
  const configApi = useConfigApi()

  const config = ref<ConfigResponse | null>(null)
  const values = ref<ConfigValues | null>(null)
  const isLoading = ref(false)
  const isSaving = ref(false)

  const fetchConfig = async () => {
    if (isLoading.value) return
    isLoading.value = true
    try {
      const { data } = await configApi.getConfig()
      if (!data.value) return
      config.value = data.value
      values.value = cloneValues(data.value.values)
    } finally {
      isLoading.value = false
    }
  }

  const saveConfig = async () => {
    if (!values.value || isSaving.value) return null
    isSaving.value = true
    try {
      const { data } = await configApi.updateConfig(values.value)
      if (!data.value) return null
      config.value = data.value
      values.value = cloneValues(data.value.values)
      return data.value
    } finally {
      isSaving.value = false
    }
  }

  return {
    config,
    values,
    isLoading,
    isSaving,
    fetchConfig,
    saveConfig,
  }
}
