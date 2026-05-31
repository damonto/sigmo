import { computed, ref, watch, type ComputedRef, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { useModemApi } from '@/apis/modem'
import type { Modem } from '@/types/modem'

type Options = {
  modemId: ComputedRef<string>
  modem: Readonly<Ref<Modem | null>>
  refreshModem: () => Promise<void>
  onSuccess?: (message: string) => void
}

export const useSimSlotSwitch = ({ modemId, modem, refreshModem, onSuccess }: Options) => {
  const { t } = useI18n()
  const modemApi = useModemApi()

  const currentSimIdentifier = ref('')

  const modemSlots = (value: Modem | null) => {
    return Array.isArray(value?.slots) ? value.slots : []
  }

  const simSlots = computed(() => modemSlots(modem.value))

  const getSimLabel = (identifier: string) => {
    const index = simSlots.value.findIndex((slot) => slot.identifier === identifier)
    if (index === 0) return t('modemDetail.sim.sim1')
    if (index === 1) return t('modemDetail.sim.sim2')
    if (index >= 0) return `SIM ${index + 1}`
    return ''
  }

  const handleSimSwitch = async (identifier: string) => {
    if (!modemId.value || modemId.value === 'unknown') {
      throw new Error('Modem ID is unavailable')
    }
    await modemApi.switchSimSlot(modemId.value, identifier)
    await refreshModem()
    const simLabel = getSimLabel(identifier)
    if (simLabel) {
      onSuccess?.(t('modemDetail.sim.switchSuccess', { sim: simLabel }))
    } else {
      onSuccess?.(t('modemDetail.sim.switchSuccessFallback'))
    }
  }

  watch(
    modem,
    (newModem) => {
      if (!newModem) {
        currentSimIdentifier.value = ''
        return
      }
      const slots = modemSlots(newModem)
      const activeSlot = slots.find((slot) => slot.active)
      currentSimIdentifier.value = activeSlot?.identifier ?? slots[0]?.identifier ?? ''
    },
    { immediate: true },
  )

  return {
    currentSimIdentifier,
    simSlots,
    handleSimSwitch,
  }
}
