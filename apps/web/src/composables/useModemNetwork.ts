import { computed, onUnmounted, ref, watch, type ComputedRef } from 'vue'
import { useI18n } from 'vue-i18n'

import { useNetworkApi } from '@/apis/network'
import type {
  BandResponse,
  BandValue,
  ModeResponse,
  NetworkScanResponse,
  NetworkResponse,
  RegistrationMode,
  SetCurrentModesRequest,
} from '@/types/network'

type Options = {
  modemId: ComputedRef<string>
  onRegistered?: (id: string) => Promise<void> | void
  onChanged?: (id: string) => Promise<void> | void
  onSuccess?: (message: string) => void
  onError?: (message: string) => void
}

export const useModemNetwork = ({
  modemId,
  onRegistered,
  onChanged,
  onSuccess,
  onError,
}: Options) => {
  const { t } = useI18n()
  const networkApi = useNetworkApi()

  const networkDialogOpen = ref(false)
  const availableNetworks = ref<NetworkResponse[]>([])
  const selectedNetwork = ref('')
  // The modem, not the browser, owns the selection mode. A manual choice
  // survives restarts and profile switches, so it is read back from the
  // server instead of being remembered locally.
  const registrationMode = ref<RegistrationMode>('automatic')
  const registeredOperatorCode = ref('')
  const modeOptions = ref<ModeResponse[]>([])
  const selectedMode = ref('')
  const currentMode = ref('')
  const supportedBands = ref<BandResponse[]>([])
  const selectedBands = ref<BandValue[]>([])
  const currentBands = ref<BandValue[]>([])
  const airplaneModeSupported = ref(false)
  const airplaneModeEnabled = ref(false)
  const isNetworkLoading = ref(false)
  const isNetworkRegistering = ref(false)
  const isRegistrationUpdating = ref(false)
  const isNetworkSettingsLoading = ref(false)
  const isModeUpdating = ref(false)
  const isBandUpdating = ref(false)
  const isAirplaneModeUpdating = ref(false)

  const hasAvailableNetworks = computed(() => availableNetworks.value.length > 0)
  const hasNetworkSelection = computed(() => selectedNetwork.value.trim().length > 0)
  const hasModeSelection = computed(() => selectedMode.value.trim().length > 0)
  const hasBandSelection = computed(() => selectedBands.value.length > 0)
  const canScanNetworks = computed(
    () => !isNetworkLoading.value && !isRegistrationUpdating.value && !airplaneModeEnabled.value,
  )
  const canUpdateRegistration = computed(
    () =>
      !isNetworkSettingsLoading.value &&
      !isNetworkLoading.value &&
      !isRegistrationUpdating.value &&
      !airplaneModeEnabled.value,
  )
  const canUpdateMode = computed(
    () =>
      modeOptions.value.length > 1 &&
      hasModeSelection.value &&
      selectedMode.value !== currentMode.value &&
      !isModeUpdating.value &&
      !airplaneModeEnabled.value,
  )
  const canUpdateBands = computed(
    () =>
      hasBandSelection.value &&
      !sameBandValues(selectedBands.value, currentBands.value) &&
      !isBandUpdating.value &&
      !airplaneModeEnabled.value,
  )
  const canUpdateAirplaneMode = computed(
    () => airplaneModeSupported.value && !isAirplaneModeUpdating.value,
  )
  let networkSettingsRequestID = 0
  let networkScanRequestID = 0
  let networkScanTimer: ReturnType<typeof setTimeout> | undefined
  let resolveNetworkScanTimer: (() => void) | undefined
  const networkScanPollInterval = 1000

  // A scan is shared by every browser using this modem. Leaving the view only
  // stops this browser's polling; the server-owned task continues for others.
  const stopNetworkScanPolling = () => {
    networkScanRequestID += 1
    if (networkScanTimer !== undefined) {
      clearTimeout(networkScanTimer)
      networkScanTimer = undefined
    }
    resolveNetworkScanTimer?.()
    resolveNetworkScanTimer = undefined
  }

  const isCurrentNetworkScan = (requestID: number, targetId: string) =>
    requestID === networkScanRequestID && modemId.value === targetId

  const waitForNetworkScan = async (
    targetId: string,
    scanID: string,
    requestID: number,
  ): Promise<NetworkScanResponse | null> => {
    while (isCurrentNetworkScan(requestID, targetId)) {
      const { data } = await networkApi.getNetworkScan(targetId, scanID)
      if (!isCurrentNetworkScan(requestID, targetId)) return null
      const response = data.value
      if (!response) throw new Error('network scan response is empty')
      if (response.status !== 'running') return response

      await new Promise<void>((resolve) => {
        resolveNetworkScanTimer = resolve
        networkScanTimer = setTimeout(() => {
          networkScanTimer = undefined
          resolveNetworkScanTimer = undefined
          resolve()
        }, networkScanPollInterval)
      })
    }
    return null
  }

  const resetNetworks = () => {
    networkDialogOpen.value = false
    availableNetworks.value = []
    selectedNetwork.value = ''
    registrationMode.value = 'automatic'
    registeredOperatorCode.value = ''
    modeOptions.value = []
    selectedMode.value = ''
    currentMode.value = ''
    supportedBands.value = []
    selectedBands.value = []
    currentBands.value = []
    airplaneModeSupported.value = false
    airplaneModeEnabled.value = false
    isNetworkLoading.value = false
  }

  const openNetworkDialog = async () => {
    const targetId = modemId.value
    if (!targetId) return
    if (!canScanNetworks.value) return
    stopNetworkScanPolling()
    const requestID = networkScanRequestID
    selectedNetwork.value = ''
    isNetworkLoading.value = true
    try {
      const { data } = await networkApi.startNetworkScan(targetId)
      if (!isCurrentNetworkScan(requestID, targetId)) return
      const started = data.value
      if (!started) throw new Error('network scan start response is empty')
      const response =
        started.status === 'running'
          ? await waitForNetworkScan(targetId, started.id, requestID)
          : started
      if (!response || !isCurrentNetworkScan(requestID, targetId)) return
      if (response.status !== 'completed') {
        throw new Error(response.errorCode ?? `network scan ${response.status}`)
      }
      availableNetworks.value = response.networks ?? []
      networkDialogOpen.value = true
    } catch (err) {
      if (!isCurrentNetworkScan(requestID, targetId)) return
      console.error('[useModemNetwork] Failed to scan networks:', err)
      availableNetworks.value = []
      networkDialogOpen.value = false
    } finally {
      if (isCurrentNetworkScan(requestID, targetId)) {
        isNetworkLoading.value = false
      }
    }
  }

  const handleNetworkRegister = async () => {
    const targetId = modemId.value
    if (!targetId) return
    if (!hasNetworkSelection.value || isNetworkRegistering.value) return
    isNetworkRegistering.value = true
    try {
      await networkApi.setRegistration(targetId, {
        mode: 'manual',
        operatorCode: selectedNetwork.value,
      })
      networkDialogOpen.value = false
      await refreshNetworkSettings()
      await onRegistered?.(targetId)
      onSuccess?.(t('modemDetail.settings.networkSuccess'))
    } catch (err) {
      console.error('[useModemNetwork] Failed to register network:', err)
    } finally {
      isNetworkRegistering.value = false
    }
  }

  // Manual mode is only entered by picking an operator, so switching the
  // selector to manual opens the scan dialog and leaves the reported mode
  // untouched until a registration succeeds. Cancelling the dialog therefore
  // falls back to the mode the modem still applies.
  const handleRegistrationModeChange = async (mode: RegistrationMode) => {
    const targetId = modemId.value
    if (!targetId || !canUpdateRegistration.value) return
    if (mode === registrationMode.value) return
    if (mode === 'manual') {
      await openNetworkDialog()
      return
    }
    isRegistrationUpdating.value = true
    try {
      await networkApi.setRegistration(targetId, { mode: 'automatic' })
      await refreshNetworkSettings()
      await onChanged?.(targetId)
      onSuccess?.(t('modemDetail.settings.networkRegistrationAutomaticSuccess'))
    } catch (err) {
      console.error('[useModemNetwork] Failed to set automatic registration:', err)
      onError?.(t('modemDetail.settings.networkRegistrationUpdateFailed'))
    } finally {
      isRegistrationUpdating.value = false
    }
  }

  const refreshNetworkSettings = async () => {
    const targetId = modemId.value
    if (!targetId) return
    const requestId = ++networkSettingsRequestID
    isNetworkSettingsLoading.value = true
    try {
      const [registration, modes, bands, airplane] = await Promise.allSettled([
        networkApi.getRegistration(targetId),
        networkApi.getModes(targetId),
        networkApi.getBands(targetId),
        networkApi.getAirplaneMode(targetId),
      ])
      if (requestId !== networkSettingsRequestID || modemId.value !== targetId) return

      if (registration.status === 'fulfilled') {
        const registrationData = registration.value.data.value
        registrationMode.value = registrationData?.mode === 'manual' ? 'manual' : 'automatic'
        registeredOperatorCode.value =
          registrationMode.value === 'manual' ? (registrationData?.operatorCode ?? '') : ''
      } else {
        console.error('[useModemNetwork] Failed to load network registration:', registration.reason)
        registrationMode.value = 'automatic'
        registeredOperatorCode.value = ''
      }

      if (modes.status === 'fulfilled') {
        const modesData = modes.value.data.value
        const options = modesData?.supported ?? []
        const current = modesData ? modeKey(modesData.current) : ''
        if (current && options.some((mode) => modeKey(mode) === current)) {
          modeOptions.value = options
          selectedMode.value = current
          currentMode.value = current
        } else {
          console.error('[useModemNetwork] Current network mode is not supported')
          modeOptions.value = []
          selectedMode.value = ''
          currentMode.value = ''
        }
      } else {
        console.error('[useModemNetwork] Failed to load network modes:', modes.reason)
        modeOptions.value = []
        selectedMode.value = ''
        currentMode.value = ''
      }

      if (bands.status === 'fulfilled') {
        const bandsData = bands.value.data.value
        supportedBands.value = bandsData?.supported ?? []
        selectedBands.value = cloneBandValues(bandsData?.current ?? [])
        currentBands.value = cloneBandValues(bandsData?.current ?? [])
      } else {
        console.error('[useModemNetwork] Failed to load network bands:', bands.reason)
        supportedBands.value = []
        selectedBands.value = []
        currentBands.value = []
      }

      if (airplane.status === 'fulfilled') {
        const airplaneData = airplane.value.data.value
        airplaneModeSupported.value = airplaneData?.supported ?? false
        airplaneModeEnabled.value = airplaneData?.enabled ?? false
      } else {
        console.error('[useModemNetwork] Failed to load airplane mode:', airplane.reason)
        airplaneModeSupported.value = false
        airplaneModeEnabled.value = false
      }
    } catch (err) {
      if (requestId !== networkSettingsRequestID || modemId.value !== targetId) return
      console.error('[useModemNetwork] Failed to load network settings:', err)
      registrationMode.value = 'automatic'
      registeredOperatorCode.value = ''
      modeOptions.value = []
      selectedMode.value = ''
      currentMode.value = ''
      supportedBands.value = []
      selectedBands.value = []
      currentBands.value = []
      airplaneModeSupported.value = false
      airplaneModeEnabled.value = false
    } finally {
      if (requestId === networkSettingsRequestID) {
        isNetworkSettingsLoading.value = false
      }
    }
  }

  const handleModeUpdate = async () => {
    const targetId = modemId.value
    const mode = modeFromKey(selectedMode.value)
    if (!targetId || !mode || !canUpdateMode.value) return
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

  const toggleBand = (band: BandValue, checked: boolean) => {
    const key = bandKey(band)
    if (!checked) {
      selectedBands.value = selectedBands.value.filter((value) => bandKey(value) !== key)
      return
    }
    if (!selectedBands.value.some((value) => bandKey(value) === key)) {
      selectedBands.value = [...selectedBands.value, band]
    }
  }

  const handleBandUpdate = async () => {
    const targetId = modemId.value
    if (!targetId || !canUpdateBands.value) return
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

  const handleAirplaneModeUpdate = async (enabled: boolean) => {
    const targetId = modemId.value
    if (!targetId) return
    if (!canUpdateAirplaneMode.value) return
    isAirplaneModeUpdating.value = true
    try {
      await networkApi.setAirplaneMode(targetId, { enabled })
      await refreshNetworkSettings()
      await onChanged?.(targetId)
      onSuccess?.(
        enabled
          ? t('modemDetail.settings.networkAirplaneModeEnabledSuccess')
          : t('modemDetail.settings.networkAirplaneModeDisabledSuccess'),
      )
    } catch (err) {
      console.error('[useModemNetwork] Failed to set airplane mode:', err)
      onError?.(t('modemDetail.settings.networkAirplaneModeUpdateFailed'))
    } finally {
      isAirplaneModeUpdating.value = false
    }
  }

  watch(
    modemId,
    async (id) => {
      stopNetworkScanPolling()
      networkSettingsRequestID += 1
      resetNetworks()
      if (!id) {
        return
      }
      await refreshNetworkSettings()
    },
    { immediate: true },
  )

  onUnmounted(stopNetworkScanPolling)

  return {
    networkDialogOpen,
    availableNetworks,
    selectedNetwork,
    registrationMode,
    registeredOperatorCode,
    modeOptions,
    selectedMode,
    supportedBands,
    selectedBands,
    airplaneModeSupported,
    airplaneModeEnabled,
    isNetworkLoading,
    isNetworkRegistering,
    isRegistrationUpdating,
    isNetworkSettingsLoading,
    isModeUpdating,
    isBandUpdating,
    isAirplaneModeUpdating,
    hasAvailableNetworks,
    hasNetworkSelection,
    hasModeSelection,
    hasBandSelection,
    canScanNetworks,
    canUpdateRegistration,
    canUpdateMode,
    canUpdateBands,
    canUpdateAirplaneMode,
    openNetworkDialog,
    handleNetworkRegister,
    handleRegistrationModeChange,
    refreshNetworkSettings,
    handleModeUpdate,
    toggleBand,
    handleBandUpdate,
    handleAirplaneModeUpdate,
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
  if (
    !Number.isSafeInteger(allowed) ||
    !Number.isSafeInteger(preferred) ||
    allowed < 0 ||
    preferred < 0
  ) {
    return null
  }
  return { allowed, preferred }
}

const bandKey = (band: BandValue) => `${band.technology}:${band.number}`

const cloneBandValues = (bands: BandValue[]) => bands.map((band) => ({ ...band }))

const sameBandValues = (left: BandValue[], right: BandValue[]) => {
  if (left.length !== right.length) return false
  const leftKeys = new Set(left.map(bandKey))
  const rightKeys = new Set(right.map(bandKey))
  if (leftKeys.size !== left.length || rightKeys.size !== right.length) return false
  return [...leftKeys].every((key) => rightKeys.has(key))
}
