import { computed, ref, watch, type ComputedRef } from 'vue'
import { useI18n } from 'vue-i18n'

import { useNetworkApi } from '@/apis/network'
import type { NetworkResponse } from '@/types/network'

type Options = {
  modemId: ComputedRef<string>
  onRegistered?: (id: string) => Promise<void> | void
  onSuccess?: (message: string) => void
}

export const useModemNetwork = ({ modemId, onRegistered, onSuccess }: Options) => {
  const { t } = useI18n()
  const networkApi = useNetworkApi()

  const networkDialogOpen = ref(false)
  const availableNetworks = ref<NetworkResponse[]>([])
  const selectedNetwork = ref('')
  const isNetworkLoading = ref(false)
  const isNetworkRegistering = ref(false)

  const hasAvailableNetworks = computed(() => availableNetworks.value.length > 0)
  const hasNetworkSelection = computed(() => selectedNetwork.value.trim().length > 0)

  const resetNetworks = () => {
    networkDialogOpen.value = false
    availableNetworks.value = []
    selectedNetwork.value = ''
  }

  const openNetworkDialog = async () => {
    const targetId = modemId.value
    if (!targetId || targetId === 'unknown') return
    if (isNetworkLoading.value) return
    networkDialogOpen.value = true
    selectedNetwork.value = ''
    isNetworkLoading.value = true
    try {
      const { data } = await networkApi.scanNetworks(targetId)
      availableNetworks.value = data.value ?? []
    } catch (err) {
      console.error('[useModemNetwork] Failed to scan networks:', err)
      availableNetworks.value = []
    } finally {
      isNetworkLoading.value = false
    }
  }

  const handleNetworkRegister = async () => {
    const targetId = modemId.value
    if (!targetId || targetId === 'unknown') return
    if (!hasNetworkSelection.value || isNetworkRegistering.value) return
    isNetworkRegistering.value = true
    try {
      await networkApi.registerNetwork(targetId, selectedNetwork.value)
      await onRegistered?.(targetId)
      networkDialogOpen.value = false
      onSuccess?.(t('modemDetail.settings.networkSuccess'))
    } catch (err) {
      console.error('[useModemNetwork] Failed to register network:', err)
    } finally {
      isNetworkRegistering.value = false
    }
  }

  watch(
    modemId,
    (id) => {
      if (!id || id === 'unknown') {
        resetNetworks()
      }
    },
    { immediate: true },
  )

  return {
    networkDialogOpen,
    availableNetworks,
    selectedNetwork,
    isNetworkLoading,
    isNetworkRegistering,
    hasAvailableNetworks,
    hasNetworkSelection,
    openNetworkDialog,
    handleNetworkRegister,
  }
}
