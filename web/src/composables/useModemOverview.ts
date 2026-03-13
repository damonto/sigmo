import { computed, ref, watch, type ComputedRef } from 'vue'
import { useI18n } from 'vue-i18n'

import { useModemApi } from '@/apis/modem'
import type { Modem } from '@/types/modem'

export const useModemOverview = (modemId: ComputedRef<string>) => {
  const { t } = useI18n()
  const modemApi = useModemApi()

  const modem = ref<Modem | null>(null)
  const isModemLoading = ref(false)

  const currentOperatorLabel = computed(() => {
    const unknown = t('modemDetail.settings.networkUnknown')
    const name = modem.value?.registeredOperator?.name?.trim() ?? ''
    const code = modem.value?.registeredOperator?.code?.trim() ?? ''
    if (!name && !code) return unknown
    if (name && code) return `${name} (${code})`
    return name || code || unknown
  })

  const currentRegistrationState = computed(() => {
    const value = modem.value?.registrationState?.trim()
    return value && value.length > 0 ? value : t('modemDetail.settings.networkUnknown')
  })

  const currentAccessTechnology = computed(() => {
    const value = modem.value?.accessTechnology?.trim()
    return value && value.length > 0 ? value : t('modemDetail.settings.networkUnknown')
  })

  const fetchModem = async (id: string) => {
    if (isModemLoading.value) return
    isModemLoading.value = true
    try {
      const { data } = await modemApi.getModem(id)
      modem.value = data.value ?? null
    } finally {
      isModemLoading.value = false
    }
  }

  watch(
    modemId,
    async (id) => {
      if (!id || id === 'unknown') {
        modem.value = null
        return
      }
      await fetchModem(id)
    },
    { immediate: true },
  )

  return {
    modem,
    isModemLoading,
    currentOperatorLabel,
    currentRegistrationState,
    currentAccessTechnology,
    fetchModem,
  }
}
