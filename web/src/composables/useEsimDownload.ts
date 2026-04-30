import { computed, onBeforeUnmount, ref, type Ref } from 'vue'

import { getStoredToken } from '@/lib/auth-storage'

export type EsimDownloadState =
  | 'idle'
  | 'connecting'
  | 'progress'
  | 'preview'
  | 'confirmation'
  | 'completed'
  | 'error'

export type EsimDownloadStage = 'initializing' | 'connecting' | 'installing' | ''

export type EsimDownloadPreview = {
  iccid: string
  serviceProviderName: string
  profileName: string
  profileNickname?: string
  profileState: string
  icon?: string
  regionCode?: string
}

type InstallPayload = {
  smdp: string
  activationCode: string
  confirmationCode: string
}

type DownloadServerMessage = {
  type: string
  stage?: string
  profile?: EsimDownloadPreview
  message?: string
}

type DownloadErrorType = 'none' | 'failed' | 'disconnected'

type Options = {
  onCompleted?: () => void
}

const stageMap: Record<string, EsimDownloadStage> = {
  'Authenticating Client': 'initializing',
  'Authenticating Server': 'connecting',
  Installing: 'installing',
}

const progressSteps: Record<Exclude<EsimDownloadStage, ''>, number> = {
  initializing: 10,
  connecting: 40,
  installing: 40,
}

const installingCeiling = 90
const installingTickMs = 400
const installingStep = 2
const compactEsimValue = (value: string) => value.replace(/\s+/g, '')

const normalizeInstallPayload = (payload: InstallPayload): InstallPayload => ({
  smdp: compactEsimValue(payload.smdp),
  activationCode: compactEsimValue(payload.activationCode),
  confirmationCode: payload.confirmationCode.trim(),
})

export const useEsimDownload = (modemId: Ref<string>, options?: Options) => {
  const downloadState = ref<EsimDownloadState>('idle')
  const downloadStage = ref<EsimDownloadStage>('')
  const progress = ref(0)
  const errorType = ref<DownloadErrorType>('none')
  const errorMessage = ref('')
  const previewProfile = ref<EsimDownloadPreview | null>(null)

  let ws: WebSocket | null = null
  let installingTimer: number | null = null

  const downloadedName = computed(() => {
    const profile = previewProfile.value
    return profile?.profileName || profile?.serviceProviderName || profile?.profileNickname || ''
  })

  const resetState = () => {
    downloadState.value = 'idle'
    downloadStage.value = ''
    progress.value = 0
    errorType.value = 'none'
    errorMessage.value = ''
    previewProfile.value = null
  }

  const stopInstallingTimer = () => {
    if (installingTimer === null) return
    window.clearInterval(installingTimer)
    installingTimer = null
  }

  const closeWebSocket = () => {
    if (!ws) return
    ws.close()
    ws = null
  }

  const setProgress = (value: number) => {
    progress.value = Math.min(Math.max(value, 0), 100)
  }

  const setStage = (stage: EsimDownloadStage) => {
    if (stage === '') return
    if (downloadStage.value === stage) return
    downloadStage.value = stage
    stopInstallingTimer()

    setProgress(progressSteps[stage] ?? progress.value)
    if (stage === 'installing') {
      installingTimer = window.setInterval(() => {
        if (progress.value >= installingCeiling) {
          stopInstallingTimer()
          return
        }
        setProgress(Math.min(progress.value + installingStep, installingCeiling))
      }, installingTickMs)
    }
  }

  const sendMessage = (payload: object) => {
    if (!ws || ws.readyState !== WebSocket.OPEN) return
    ws.send(JSON.stringify(payload))
  }

  const buildWsUrl = (id: string) => {
    const rawBase = import.meta.env.VITE_API_BASE_URL as string | undefined
    const base = rawBase && rawBase.trim().length > 0 ? rawBase.replace(/\/$/, '') : '/api/v1'
    const apiUrl = new URL(base, window.location.origin)
    apiUrl.protocol = apiUrl.protocol === 'https:' ? 'wss:' : 'ws:'
    apiUrl.pathname = `${apiUrl.pathname.replace(/\/$/, '')}/modems/${id}/esims/download-sessions`
    const token = getStoredToken()
    if (token) {
      apiUrl.searchParams.set('token', token)
    }
    return apiUrl.toString()
  }

  const handleServerMessage = (message: DownloadServerMessage) => {
    if (
      downloadState.value === 'idle' ||
      downloadState.value === 'error' ||
      downloadState.value === 'completed'
    ) {
      return
    }
    switch (message.type) {
      case 'progress': {
        const nextStage = stageMap[message.stage ?? ''] ?? ''
        if (nextStage) {
          downloadState.value = 'progress'
          setStage(nextStage)
        }
        return
      }
      case 'preview':
        previewProfile.value = message.profile ?? null
        downloadState.value = 'preview'
        stopInstallingTimer()
        return
      case 'confirmation_code_required':
        downloadState.value = 'confirmation'
        stopInstallingTimer()
        return
      case 'completed':
        downloadState.value = 'completed'
        setProgress(100)
        stopInstallingTimer()
        closeWebSocket()
        options?.onCompleted?.()
        return
      case 'error':
        downloadState.value = 'error'
        errorType.value = 'failed'
        errorMessage.value = message.message?.trim() ?? ''
        stopInstallingTimer()
        closeWebSocket()
        return
      default:
        return
    }
  }

  const startDownload = (payload: InstallPayload) => {
    if (!modemId.value || modemId.value === 'unknown') return
    closeWebSocket()
    resetState()
    const normalizedPayload = normalizeInstallPayload(payload)

    downloadState.value = 'connecting'
    setStage('initializing')

    ws = new WebSocket(buildWsUrl(modemId.value))
    ws.onopen = () => {
      sendMessage({
        type: 'start',
        smdp: normalizedPayload.smdp,
        activationCode: normalizedPayload.activationCode,
        confirmationCode: normalizedPayload.confirmationCode,
      })
    }
    ws.onmessage = (event) => {
      try {
        const message = JSON.parse(event.data) as DownloadServerMessage
        handleServerMessage(message)
      } catch (err) {
        console.error('[useEsimDownload] Failed to parse message:', err)
      }
    }
    ws.onerror = () => {
      if (downloadState.value === 'completed') return
      downloadState.value = 'error'
      errorType.value = 'failed'
      stopInstallingTimer()
      closeWebSocket()
    }
    ws.onclose = () => {
      if (
        downloadState.value === 'completed' ||
        downloadState.value === 'error' ||
        downloadState.value === 'idle'
      ) {
        return
      }
      downloadState.value = 'error'
      errorType.value = 'disconnected'
      stopInstallingTimer()
      closeWebSocket()
    }
  }

  const confirmPreview = (accept: boolean) => {
    sendMessage({ type: 'confirm', accept })
    if (!accept) {
      sendMessage({ type: 'cancel' })
      closeWebSocket()
      resetState()
      return
    }
    downloadState.value = 'progress'
  }

  const submitConfirmationCode = (code: string) => {
    const normalized = code.trim()
    if (!normalized) return
    sendMessage({ type: 'confirmation_code', code: normalized })
    downloadState.value = 'progress'
  }

  const cancelDownload = () => {
    sendMessage({ type: 'cancel' })
    closeWebSocket()
    resetState()
  }

  const closeDialog = () => {
    closeWebSocket()
    resetState()
  }

  onBeforeUnmount(() => {
    closeWebSocket()
    stopInstallingTimer()
  })

  return {
    downloadState,
    downloadStage,
    progress,
    errorType,
    errorMessage,
    previewProfile,
    downloadedName,
    startDownload,
    confirmPreview,
    submitConfirmationCode,
    cancelDownload,
    closeDialog,
  }
}
