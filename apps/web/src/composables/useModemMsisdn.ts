import { computed, ref, watch, type ComputedRef, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { useModemApi } from '@/apis/modem'
import {
  formatPhoneDisplay,
  formatPhoneInput,
  normalizePhoneSubmission,
} from '@/lib/phoneNumberInput'
import type { Modem } from '@/types/modem'

type Options = {
  modemId: ComputedRef<string>
  modem: Ref<Modem | null>
  refreshModem: (id: string) => Promise<void>
  onSuccess?: (message: string) => void
}

export const useModemMsisdn = ({ modemId, modem, refreshModem, onSuccess }: Options) => {
  const { t } = useI18n()
  const modemApi = useModemApi()

  const msisdnText = ref('')
  const isMsisdnUpdating = ref(false)

  const phoneCountry = computed(() => modem.value?.sim?.regionCode ?? '')
  const msisdnInput = computed({
    get: () => msisdnText.value,
    set: (value: string) => {
      msisdnText.value = formatPhoneInput(value, phoneCountry.value)
    },
  })
  const msisdnValue = computed(() => normalizePhoneSubmission(msisdnText.value, phoneCountry.value))
  const isMsisdnValid = computed(() => msisdnValue.value.length > 0)
  const resetMsisdnInput = () => {
    const value = modem.value
    msisdnText.value = value?.number ? formatPhoneDisplay(value.number, value.sim?.regionCode) : ''
  }

  watch(
    modem,
    () => resetMsisdnInput(),
    { immediate: true },
  )

  const handleMsisdnUpdate = async () => {
    const targetId = modemId.value
    if (!targetId) return false
    if (!isMsisdnValid.value || isMsisdnUpdating.value) return false
    isMsisdnUpdating.value = true
    try {
      await modemApi.updateMsisdn(targetId, msisdnValue.value)
      await refreshModem(targetId)
      onSuccess?.(t('modemDetail.settings.msisdnSuccess'))
      return true
    } catch (err) {
      console.error('[useModemMsisdn] Failed to update MSISDN:', err)
      return false
    } finally {
      isMsisdnUpdating.value = false
    }
  }

  return {
    msisdnInput,
    isMsisdnUpdating,
    isMsisdnValid,
    resetMsisdnInput,
    handleMsisdnUpdate,
  }
}
