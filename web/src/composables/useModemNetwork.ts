import { computed, ref, watch, type ComputedRef } from 'vue'
import { useI18n } from 'vue-i18n'

import { useNetworkApi } from '@/apis/network'
import type {
  BandResponse,
  CellInfoResponse,
  ModeResponse,
  NetworkResponse,
  SetCurrentModesRequest,
} from '@/types/network'

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
  const modeOptions = ref<ModeResponse[]>([])
  const selectedMode = ref('')
  const supportedBands = ref<BandResponse[]>([])
  const selectedBands = ref<number[]>([])
  const cellInfo = ref<CellInfoResponse[]>([])
  const isNetworkLoading = ref(false)
  const isNetworkRegistering = ref(false)
  const isNetworkSettingsLoading = ref(false)
  const isModeUpdating = ref(false)
  const isBandUpdating = ref(false)
  const isCellInfoLoading = ref(false)

  const hasAvailableNetworks = computed(() => availableNetworks.value.length > 0)
  const hasNetworkSelection = computed(() => selectedNetwork.value.trim().length > 0)
  const hasModeSelection = computed(() => selectedMode.value.trim().length > 0)
  const hasBandSelection = computed(() => selectedBands.value.length > 0)
  const hasCells = computed(() => cellInfo.value.length > 0)
  const canUpdateMode = computed(() => hasModeSelection.value && !isModeUpdating.value)
  const canUpdateBands = computed(() => hasBandSelection.value && !isBandUpdating.value)
  let networkSettingsRequestID = 0
  let cellInfoRequestID = 0

  const resetNetworks = () => {
    networkDialogOpen.value = false
    availableNetworks.value = []
    selectedNetwork.value = ''
    modeOptions.value = []
    selectedMode.value = ''
    supportedBands.value = []
    selectedBands.value = []
    cellInfo.value = []
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

  const refreshNetworkSettings = async () => {
    const targetId = modemId.value
    if (!targetId || targetId === 'unknown') return
    const requestId = ++networkSettingsRequestID
    isNetworkSettingsLoading.value = true
    try {
      const [modes, bands, cells] = await Promise.allSettled([
        networkApi.getModes(targetId),
        networkApi.getBands(targetId),
        networkApi.getCells(targetId),
      ])
      if (requestId !== networkSettingsRequestID || modemId.value !== targetId) return

      if (modes.status === 'fulfilled') {
        const modesData = modes.value.data.value
        modeOptions.value = modesData?.supported ?? []
        selectedMode.value = modesData ? modeKey(modesData.current) : ''
      } else {
        console.error('[useModemNetwork] Failed to load network modes:', modes.reason)
        modeOptions.value = []
        selectedMode.value = ''
      }

      if (bands.status === 'fulfilled') {
        const bandsData = bands.value.data.value
        supportedBands.value = bandsData?.supported ?? []
        selectedBands.value = bandsData?.current ?? []
      } else {
        console.error('[useModemNetwork] Failed to load network bands:', bands.reason)
        supportedBands.value = []
        selectedBands.value = []
      }

      if (cells.status === 'fulfilled') {
        cellInfo.value = cells.value.data.value ?? []
      } else {
        console.error('[useModemNetwork] Failed to load cell info:', cells.reason)
        cellInfo.value = []
      }
    } catch (err) {
      if (requestId !== networkSettingsRequestID || modemId.value !== targetId) return
      console.error('[useModemNetwork] Failed to load network settings:', err)
      modeOptions.value = []
      selectedMode.value = ''
      supportedBands.value = []
      selectedBands.value = []
      cellInfo.value = []
    } finally {
      if (requestId === networkSettingsRequestID) {
        isNetworkSettingsLoading.value = false
      }
    }
  }

  const refreshCellInfo = async () => {
    const targetId = modemId.value
    if (!targetId || targetId === 'unknown') return
    const requestId = ++cellInfoRequestID
    isCellInfoLoading.value = true
    try {
      const { data } = await networkApi.getCells(targetId)
      if (requestId !== cellInfoRequestID || modemId.value !== targetId) return
      cellInfo.value = data.value ?? []
    } catch (err) {
      if (requestId !== cellInfoRequestID || modemId.value !== targetId) return
      console.error('[useModemNetwork] Failed to load cell info:', err)
      cellInfo.value = []
    } finally {
      if (requestId === cellInfoRequestID) {
        isCellInfoLoading.value = false
      }
    }
  }

  const handleModeUpdate = async () => {
    const targetId = modemId.value
    const mode = modeFromKey(selectedMode.value)
    if (!targetId || targetId === 'unknown' || !mode) return
    if (isModeUpdating.value) return
    isModeUpdating.value = true
    try {
      await networkApi.setCurrentModes(targetId, mode)
      await refreshNetworkSettings()
      onSuccess?.(t('modemDetail.settings.networkModeSuccess'))
    } catch (err) {
      console.error('[useModemNetwork] Failed to set current modes:', err)
    } finally {
      isModeUpdating.value = false
    }
  }

  const toggleBand = (band: number, checked: boolean) => {
    const anyBand = 256
    if (!checked) {
      selectedBands.value = selectedBands.value.filter((value) => value !== band)
      return
    }
    if (band === anyBand) {
      selectedBands.value = [anyBand]
      return
    }
    selectedBands.value = selectedBands.value.filter((value) => value !== anyBand)
    if (!selectedBands.value.includes(band)) {
      selectedBands.value = [...selectedBands.value, band]
    }
  }

  const handleBandUpdate = async () => {
    const targetId = modemId.value
    if (!targetId || targetId === 'unknown') return
    if (!hasBandSelection.value || isBandUpdating.value) return
    isBandUpdating.value = true
    try {
      await networkApi.setCurrentBands(targetId, { bands: selectedBands.value })
      await refreshNetworkSettings()
      onSuccess?.(t('modemDetail.settings.networkBandSuccess'))
    } catch (err) {
      console.error('[useModemNetwork] Failed to set current bands:', err)
    } finally {
      isBandUpdating.value = false
    }
  }

  watch(
    modemId,
    async (id) => {
      if (!id || id === 'unknown') {
        resetNetworks()
        return
      }
      await refreshNetworkSettings()
    },
    { immediate: true },
  )

  return {
    networkDialogOpen,
    availableNetworks,
    selectedNetwork,
    modeOptions,
    selectedMode,
    supportedBands,
    selectedBands,
    cellInfo,
    isNetworkLoading,
    isNetworkRegistering,
    isNetworkSettingsLoading,
    isModeUpdating,
    isBandUpdating,
    isCellInfoLoading,
    hasAvailableNetworks,
    hasNetworkSelection,
    hasModeSelection,
    hasBandSelection,
    hasCells,
    canUpdateMode,
    canUpdateBands,
    openNetworkDialog,
    handleNetworkRegister,
    refreshNetworkSettings,
    refreshCellInfo,
    handleModeUpdate,
    toggleBand,
    handleBandUpdate,
  }
}

const modeKey = (mode: Pick<ModeResponse, 'allowed' | 'preferred'>) =>
  `${mode.allowed}:${mode.preferred}`

const modeFromKey = (key: string): SetCurrentModesRequest | null => {
  const parts = key.split(':')
  if (parts.length !== 2) return null
  const allowedPart = parts[0]
  const preferredPart = parts[1]
  if (allowedPart === undefined || preferredPart === undefined) return null
  const allowed = Number(allowedPart)
  const preferred = Number(preferredPart)
  if (!Number.isFinite(allowed) || !Number.isFinite(preferred)) return null
  return { allowed, preferred }
}
