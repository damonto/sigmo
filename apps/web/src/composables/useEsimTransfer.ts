import { computed, onBeforeUnmount, ref, type Ref } from 'vue'

import { useEsimApi } from '@/apis/esim'
import {
  TRANSFER_STAGE,
  TRANSFER_STATE,
  transferStageProgress,
  type EsimTransferState,
} from '@/constants/esimTransfer'
import {
  useEsimTransferSession,
  type TransferUserInput,
} from '@/composables/useEsimTransferSession'
import type {
  EsimDownloadPreview,
  EsimTransferProfile,
  EsimTransferSource,
  EsimTransferWebsheet,
} from '@/types/esim'

type Options = {
  onCompleted?: () => void
}

export type TransferStatus = {
  state: EsimTransferState
  stage: string
  progress: number
  errorMessage: string
}

export type TransferEvent =
  | { type: 'reset' }
  | { type: 'loading_sources' }
  | { type: 'loading_profiles' }
  | { type: 'ready' }
  | { type: 'connecting' }
  | { type: 'progress'; stage?: string }
  | { type: 'user_input' }
  | { type: 'source_deletion' }
  | { type: 'websheet' }
  | { type: 'completed' }
  | { type: 'error'; message: string }

const initialTransferStatus = (): TransferStatus => ({
  state: TRANSFER_STATE.idle,
  stage: '',
  progress: 0,
  errorMessage: '',
})

export const reduceEsimTransferStatus = (
  current: TransferStatus,
  event: TransferEvent,
): TransferStatus => {
  switch (event.type) {
    case 'reset':
      return initialTransferStatus()
    case 'loading_sources':
      return { ...initialTransferStatus(), state: TRANSFER_STATE.loadingSources }
    case 'loading_profiles':
      return { ...current, state: TRANSFER_STATE.loadingProfiles, errorMessage: '' }
    case 'ready':
      return { ...current, state: TRANSFER_STATE.ready, errorMessage: '' }
    case 'connecting':
      return {
        ...current,
        state: TRANSFER_STATE.connecting,
        stage: TRANSFER_STAGE.preparing,
        progress: transferStageProgress[TRANSFER_STAGE.preparing],
        errorMessage: '',
      }
    case 'progress': {
      const stage = event.stage ?? current.stage
      return {
        ...current,
        state: TRANSFER_STATE.progress,
        stage,
        progress: progressForStage(stage, current.progress),
      }
    }
    case 'user_input':
      return { ...current, state: TRANSFER_STATE.userInput }
    case 'source_deletion':
      return { ...current, state: TRANSFER_STATE.sourceDeletion }
    case 'websheet':
      return { ...current, state: TRANSFER_STATE.websheet }
    case 'completed':
      return { ...current, state: TRANSFER_STATE.completed, progress: 100, errorMessage: '' }
    case 'error':
      if (
        current.state === TRANSFER_STATE.completed ||
        current.state === TRANSFER_STATE.error ||
        current.state === TRANSFER_STATE.idle
      ) {
        return current
      }
      return { ...current, state: TRANSFER_STATE.error, errorMessage: event.message }
  }
}

const progressForStage = (stage: string, current: number) => {
  const next = transferStageProgress[stage]
  if (next === undefined) return current
  return Math.min(Math.max(next, 0), 100)
}

export const useEsimTransfer = (modemId: Readonly<Ref<string>>, options?: Options) => {
  const esimApi = useEsimApi()
  const state = ref<EsimTransferState>(TRANSFER_STATE.idle)
  const stage = ref('')
  const progress = ref(0)
  const errorMessage = ref('')
  const ccidError = ref('')
  const sources = ref<EsimTransferSource[]>([])
  const profiles = ref<EsimTransferProfile[]>([])
  const selectedSource = ref<EsimTransferSource | null>(null)
  const selectedProfile = ref<EsimTransferProfile | null>(null)
  const sourceImei = ref('')
  const userInput = ref<TransferUserInput | null>(null)
  const userInputResponse = ref('')
  const sourceDeletionICCID = ref('')
  const previewProfile = ref<EsimDownloadPreview | null>(null)
  const carrierWebsheet = ref<EsimTransferWebsheet | null>(null)

  const hasSources = computed(() => sources.value.length > 0)
  const downloadedName = computed(() => {
    const profile = previewProfile.value
    return (
      profile?.profileName ||
      profile?.serviceProviderName ||
      profile?.profileNickname ||
      selectedProfile.value?.name ||
      ''
    )
  })
  const needsSourceImei = computed(() => selectedSource.value?.requiresSourceImei ?? false)
  const canStartTransfer = computed(() => {
    if (!selectedSource.value || !selectedProfile.value?.supported) return false
    if (!needsSourceImei.value) return true
    return sourceImei.value.trim().length > 0
  })

  const resetSelection = () => {
    profiles.value = []
    selectedProfile.value = null
    userInput.value = null
    userInputResponse.value = ''
    sourceDeletionICCID.value = ''
    previewProfile.value = null
    carrierWebsheet.value = null
  }

  const resetAll = () => {
    state.value = TRANSFER_STATE.idle
    stage.value = ''
    progress.value = 0
    errorMessage.value = ''
    ccidError.value = ''
    sources.value = []
    selectedSource.value = null
    sourceImei.value = ''
    resetSelection()
  }

  const applyTransferEvent = (event: TransferEvent) => {
    const next = reduceEsimTransferStatus(
      {
        state: state.value,
        stage: stage.value,
        progress: progress.value,
        errorMessage: errorMessage.value,
      },
      event,
    )
    state.value = next.state
    stage.value = next.stage
    progress.value = next.progress
    errorMessage.value = next.errorMessage
  }

  const session = useEsimTransferSession({
    onProgress: (nextStage) => {
      applyTransferEvent({ type: 'progress', stage: nextStage })
    },
    onPreview: (profile) => {
      previewProfile.value = profile ?? previewProfile.value
      applyTransferEvent({ type: 'progress' })
    },
    onUserInput: (input) => {
      userInput.value = input
      userInputResponse.value = ''
      applyTransferEvent({ type: 'user_input' })
    },
    onSourceDeletion: (iccid) => {
      sourceDeletionICCID.value = iccid
      applyTransferEvent({ type: 'source_deletion' })
    },
    onWebsheet: (websheet) => {
      carrierWebsheet.value = websheet
      applyTransferEvent({ type: 'websheet' })
    },
    onCompleted: () => {
      applyTransferEvent({ type: 'completed' })
      options?.onCompleted?.()
    },
    onError: (message) => {
      applyTransferEvent({ type: 'error', message })
    },
  })

  const loadSources = async () => {
    const targetId = modemId.value
    if (!targetId || targetId === 'unknown') return
    session.close()
    resetAll()
    applyTransferEvent({ type: 'loading_sources' })
    try {
      const { data } = await esimApi.getTransferSources(targetId)
      sources.value = data.value?.sources ?? []
      ccidError.value = data.value?.ccidError ?? ''
      applyTransferEvent({ type: 'ready' })
    } catch (err) {
      applyTransferEvent({ type: 'error', message: err instanceof Error ? err.message : '' })
    }
  }

  const selectSource = (source: EsimTransferSource) => {
    selectedSource.value = source
    sourceImei.value = source.requiresSourceImei && modemId.value !== 'unknown' ? modemId.value : ''
    resetSelection()
  }

  const loadProfiles = async () => {
    const targetId = modemId.value
    const source = selectedSource.value
    if (!targetId || targetId === 'unknown' || !source) return
    applyTransferEvent({ type: 'loading_profiles' })
    resetSelection()
    try {
      const { data } = await esimApi.getTransferProfiles(targetId, {
        sourceType: source.type,
        sourceId: source.id,
        sourceImei: sourceImei.value.trim(),
      })
      profiles.value = data.value ?? []
      applyTransferEvent({ type: 'ready' })
    } catch (err) {
      applyTransferEvent({ type: 'error', message: err instanceof Error ? err.message : '' })
    }
  }

  const startTransfer = (seId: string) => {
    const targetId = modemId.value
    const source = selectedSource.value
    const profile = selectedProfile.value
    if (!targetId || targetId === 'unknown' || !seId.trim() || !source || !profile?.supported) return
    session.close()
    applyTransferEvent({ type: 'connecting' })
    previewProfile.value = null
    session.start(targetId, {
      seId: seId.trim(),
      sourceType: source.type,
      sourceId: source.id,
      profileId: profile.id,
      sourceImei: sourceImei.value.trim(),
    })
  }

  const submitUserInput = (accept: boolean) => {
    session.submitUserInput(accept, userInputResponse.value.trim())
    applyTransferEvent({ type: 'progress' })
  }

  const confirmSourceDeletion = (accept: boolean) => {
    session.confirmSourceDeletion(accept)
    applyTransferEvent({ type: 'progress' })
  }

  const completeWebsheet = () => {
    applyTransferEvent({ type: 'progress' })
  }

  const cancelTransfer = () => {
    session.cancel()
    resetAll()
  }

  onBeforeUnmount(() => {
    session.close()
  })

  return {
    state,
    stage,
    progress,
    errorMessage,
    ccidError,
    sources,
    profiles,
    selectedSource,
    selectedProfile,
    sourceImei,
    userInput,
    userInputResponse,
    sourceDeletionICCID,
    previewProfile,
    carrierWebsheet,
    hasSources,
    downloadedName,
    needsSourceImei,
    canStartTransfer,
    loadSources,
    selectSource,
    loadProfiles,
    startTransfer,
    submitUserInput,
    confirmSourceDeletion,
    completeWebsheet,
    cancelTransfer,
    resetAll,
  }
}
