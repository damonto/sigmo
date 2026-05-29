import { computed, type ComputedRef } from 'vue'

import { useModemResource } from '@/composables/useModemResource'

export const useModemPhoneCountry = (modemId: ComputedRef<string>) => {
  const { modem } = useModemResource(modemId)

  return {
    phoneCountry: computed(() => modem.value?.sim.regionCode ?? ''),
  }
}
