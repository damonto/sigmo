import { computed, ref, watch, type ComputedRef, type Ref } from 'vue'

import { useEsimApi } from '@/apis/esim'
import type { EsimDiscoverItem } from '@/types/esim'

type Options = {
  modemId: ComputedRef<string> | Ref<string>
  installDialogOpen: Ref<boolean>
  applyDiscoverAddress: (address: string) => void
}

export const useEsimDiscover = ({ modemId, installDialogOpen, applyDiscoverAddress }: Options) => {
  const esimApi = useEsimApi()

  const discoverDialogOpen = ref(false)
  const discoverOptions = ref<EsimDiscoverItem[]>([])
  const selectedDiscoverAddress = ref('')
  const isDiscoverLoading = ref(false)
  const restoreInstallDialog = ref(false)

  const hasDiscoverOptions = computed(() => discoverOptions.value.length > 0)
  const hasDiscoverSelection = computed(() => selectedDiscoverAddress.value.trim().length > 0)

  const resetDiscoverState = () => {
    discoverOptions.value = []
    selectedDiscoverAddress.value = ''
  }

  const openDiscoverDialog = async () => {
    const targetId = modemId.value
    if (!targetId || targetId === 'unknown') return
    if (isDiscoverLoading.value) return
    restoreInstallDialog.value = true
    installDialogOpen.value = false
    discoverDialogOpen.value = true
    resetDiscoverState()
    isDiscoverLoading.value = true
    try {
      const { data, error } = await esimApi.discoverEsims(targetId)
      if (!discoverDialogOpen.value) return
      if (error.value) {
        discoverDialogOpen.value = false
        return
      }
      discoverOptions.value = data.value ?? []
    } finally {
      isDiscoverLoading.value = false
    }
  }

  const confirmDiscoverSelection = () => {
    const address = selectedDiscoverAddress.value.trim()
    if (!address) return
    restoreInstallDialog.value = false
    applyDiscoverAddress(address)
    discoverDialogOpen.value = false
  }

  watch(discoverDialogOpen, (value) => {
    if (!value) {
      selectedDiscoverAddress.value = ''
      if (restoreInstallDialog.value) {
        installDialogOpen.value = true
        restoreInstallDialog.value = false
      }
    }
  })

  watch(
    modemId,
    (id) => {
      if (id && id !== 'unknown') return
      discoverDialogOpen.value = false
      restoreInstallDialog.value = false
      resetDiscoverState()
    },
    { immediate: true },
  )

  return {
    discoverDialogOpen,
    discoverOptions,
    selectedDiscoverAddress,
    isDiscoverLoading,
    hasDiscoverOptions,
    hasDiscoverSelection,
    openDiscoverDialog,
    confirmDiscoverSelection,
  }
}
