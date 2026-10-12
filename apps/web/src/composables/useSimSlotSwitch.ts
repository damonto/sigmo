import { computed, ref, watch, type ComputedRef, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { useModemApi } from '@/apis/modem'
import type { Modem, SlotInfo } from '@/types/modem'

type Options = {
  modemId: ComputedRef<string>
  modem: Readonly<Ref<Modem | null>>
  refreshModem: () => Promise<void>
  onSuccess?: (message: string) => void
}

export const useSimSlotSwitch = ({ modemId, modem, refreshModem, onSuccess }: Options) => {
  const { t } = useI18n()
  const modemApi = useModemApi()

  const currentSimSlot = ref('')

  const modemSlots = (value: Modem | null) => {
    return Array.isArray(value?.slots) ? value.slots : []
  }

  const simSlots = computed(() => modemSlots(modem.value))

  const getSimLabel = (slot: number) => {
    if (slot === 1) return t('modemDetail.sim.sim1')
    if (slot === 2) return t('modemDetail.sim.sim2')
    return `SIM ${slot}`
  }

  const handleSimSwitch = async (slot: SlotInfo) => {
    if (!modemId.value || modemId.value === 'unknown') {
      throw new Error('Modem ID is unavailable')
    }
    await modemApi.switchSimSlot(modemId.value, slot.slot)
    await refreshModem()
    onSuccess?.(t('modemDetail.sim.switchSuccess', { sim: getSimLabel(slot.slot) }))
  }

  watch(
    modem,
    (newModem) => {
      if (!newModem) {
        currentSimSlot.value = ''
        return
      }
      const slots = modemSlots(newModem)
      const activeSlot = slots.find((slot) => slot.active)
      currentSimSlot.value = String(activeSlot?.slot ?? slots[0]?.slot ?? '')
    },
    { immediate: true },
  )

  return {
    currentSimSlot,
    simSlots,
    handleSimSwitch,
  }
}
