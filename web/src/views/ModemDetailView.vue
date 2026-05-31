<script setup lang="ts">
import { Download } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { toast } from 'vue-sonner'

import EsimDiscoverDialog from '@/components/esim/EsimDiscoverDialog.vue'
import EsimDownloadConfirmationModal from '@/components/esim/EsimDownloadConfirmationModal.vue'
import EsimDownloadPreviewModal from '@/components/esim/EsimDownloadPreviewModal.vue'
import EsimDownloadProgressModal from '@/components/esim/EsimDownloadProgressModal.vue'
import EsimDownloadResultModal from '@/components/esim/EsimDownloadResultModal.vue'
import EsimInstallDialog from '@/components/esim/EsimInstallDialog.vue'
import EsimProfileSection from '@/components/esim/EsimProfileSection.vue'
import EsimSummaryCard from '@/components/esim/EsimSummaryCard.vue'
import EsimTransferDialog from '@/components/esim/EsimTransferDialog.vue'
import DraggableFab from '@/components/fab/DraggableFab.vue'
import ModemDetailCard from '@/components/modem/ModemDetailCard.vue'
import ModemDetailHeader from '@/components/modem/ModemDetailHeader.vue'
import SimPinUnlockDialog from '@/components/modem/SimPinUnlockDialog.vue'
import SimSlotSwitcher from '@/components/modem/SimSlotSwitcher.vue'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { ScrollArea } from '@/components/ui/scroll-area'
import { useModemApi } from '@/apis/modem'
import { FEATURE, useCapabilities } from '@/composables/useCapabilities'
import { useEsimDiscover } from '@/composables/useEsimDiscover'
import { useEsimDownload } from '@/composables/useEsimDownload'
import { useModemDetail } from '@/composables/useModemDetail'
import { useSimSlotSwitch } from '@/composables/useSimSlotSwitch'

const route = useRoute()
const { t } = useI18n()
const { hasFeature, fetchCapabilities } = useCapabilities()
const modemApi = useModemApi()

const modemId = computed(() => (route.params.id ?? 'unknown') as string)
const canTransferEsim = computed(() => hasFeature(FEATURE.esimTransfer))
const {
  modem,
  euicc,
  esimProfiles,
  isLoading,
  isEsimProfilesLoading,
  isPhysicalModem,
  isEsimModem,
  fetchModemDetail,
  fetchEsimProfiles,
} = useModemDetail()

const installDialogRef = ref<{ applyDiscoverAddress: (address: string) => void } | null>(null)

const installDialogOpen = ref(false)
const transferDialogOpen = ref(false)
const detailDialogOpen = ref(false)
const pinUnlockDialogOpen = ref(false)
const pinUnlockInput = ref('')
const pinUnlockError = ref('')
const pinUnlockDismissedFor = ref('')
const isPinUnlocking = ref(false)
const confirmationCode = ref('')
const resultState = ref<'completed' | 'error' | null>(null)
const resultErrorMessage = ref('')
const resultErrorType = ref<'none' | 'failed' | 'disconnected'>('none')
const resultName = ref('')

const {
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
} = useEsimDownload(modemId, {
  onCompleted: () => {
    if (!modemId.value || modemId.value === 'unknown') return
    void fetchEsimProfiles(modemId.value)
  },
})

const isProgressModalOpen = computed(
  () => downloadState.value === 'connecting' || downloadState.value === 'progress',
)
const isPreviewModalOpen = computed(() => downloadState.value === 'preview')
const isConfirmationModalOpen = computed(() => downloadState.value === 'confirmation')
const isResultModalOpen = computed(
  () => downloadState.value === 'completed' || downloadState.value === 'error',
)

const stageLabel = computed(() => {
  if (downloadStage.value === 'initializing') return t('modemDetail.esim.downloadStageInitializing')
  if (downloadStage.value === 'connecting') return t('modemDetail.esim.downloadStageConnecting')
  if (downloadStage.value === 'installing') return t('modemDetail.esim.downloadStageInstalling')
  return t('modemDetail.esim.downloadConnecting')
})

const progressTitle = computed(() => t('modemDetail.esim.downloadTitle'))
const resultTone = computed(() => (resultState.value === 'error' ? 'error' : 'success'))
const resultTitle = computed(() => {
  if (resultState.value === 'error') {
    return t('modemDetail.esim.downloadErrorTitle')
  }
  if (resultState.value === 'completed') {
    return t('modemDetail.esim.downloadCompletedTitle')
  }
  return ''
})
const resultMessage = computed(() => {
  if (resultState.value === 'error') {
    if (resultErrorMessage.value) return resultErrorMessage.value
    return resultErrorType.value === 'disconnected'
      ? t('modemDetail.esim.downloadDisconnected')
      : t('modemDetail.esim.downloadErrorFallback')
  }
  if (resultState.value === 'completed') {
    const fallbackName = t('modemDetail.esim.downloadCompletedFallbackName')
    const name = resultName.value || fallbackName
    return t('modemDetail.esim.downloadCompletedMessage', { name })
  }
  return ''
})

const confirmationTitle = computed(() => t('modemDetail.esim.downloadConfirmationTitle'))
const confirmationHint = computed(() => t('modemDetail.esim.downloadConfirmationHint'))
const confirmationPlaceholder = computed(() =>
  t('modemDetail.esim.downloadConfirmationPlaceholder'),
)

const refreshModem = async () => {
  if (!modemId.value || modemId.value === 'unknown') return
  await fetchModemDetail(modemId.value)
}

const needsPinUnlock = computed(() => {
  return modem.value?.state === 'locked' && modem.value.unlockSupported
})

const handlePinUnlockCancel = () => {
  pinUnlockDismissedFor.value = modemId.value
  pinUnlockError.value = ''
}

const openPinUnlockDialog = () => {
  pinUnlockDismissedFor.value = ''
  pinUnlockError.value = ''
  pinUnlockDialogOpen.value = true
}

const handlePinUnlockSubmit = async () => {
  if (!modemId.value || modemId.value === 'unknown' || isPinUnlocking.value) return

  isPinUnlocking.value = true
  pinUnlockError.value = ''
  try {
    await modemApi.unlockSim(modemId.value, pinUnlockInput.value)
    pinUnlockDismissedFor.value = ''
    pinUnlockDialogOpen.value = false
    pinUnlockInput.value = ''
    toast.success(t('modemDetail.unlock.success'))
    await refreshModem()
  } catch (err) {
    pinUnlockError.value = err instanceof Error ? err.message : t('modemDetail.unlock.error')
  } finally {
    isPinUnlocking.value = false
  }
}

const showSuccess = (message: string) => {
  toast.success(message)
}

const { currentSimIdentifier, simSlots, handleSimSwitch } = useSimSlotSwitch({
  modemId,
  modem,
  refreshModem,
  onSuccess: showSuccess,
})

const {
  discoverDialogOpen,
  discoverOptions,
  selectedDiscoverAddress,
  isDiscoverLoading,
  hasDiscoverOptions,
  hasDiscoverSelection,
  openDiscoverDialog,
  confirmDiscoverSelection,
} = useEsimDiscover({
  modemId,
  installDialogOpen,
  applyDiscoverAddress: (address: string) => {
    installDialogRef.value?.applyDiscoverAddress(address)
  },
})

watch(downloadState, (value) => {
  if (value === 'confirmation') {
    confirmationCode.value = ''
  }
  if (value === 'connecting') {
    resultState.value = null
    resultErrorMessage.value = ''
    resultErrorType.value = 'none'
    resultName.value = ''
  }
  if (value === 'error') {
    resultState.value = 'error'
    resultErrorMessage.value = errorMessage.value
    resultErrorType.value = errorType.value
  }
  if (value === 'completed') {
    resultState.value = 'completed'
    resultName.value = downloadedName.value
  }
})

watch(
  [needsPinUnlock, modemId],
  ([needsUnlock, id]) => {
    if (!needsUnlock) {
      pinUnlockDialogOpen.value = false
      pinUnlockDismissedFor.value = ''
      pinUnlockError.value = ''
      return
    }
    if (pinUnlockDismissedFor.value !== id) {
      pinUnlockDialogOpen.value = true
    }
  },
  { immediate: true },
)

// Fetch modem detail when route changes or on mount
watch(
  modemId,
  async (id) => {
    if (!id || id === 'unknown') return
    await fetchModemDetail(id)
  },
  { immediate: true },
)

const handleConfirmationSubmit = () => {
  submitConfirmationCode(confirmationCode.value)
}

const handlePreviewConfirm = () => {
  confirmPreview(true)
}

const handlePreviewCancel = () => {
  confirmPreview(false)
}

const handleResultConfirm = () => {
  closeDialog()
}

const openTransferDialog = () => {
  if (!canTransferEsim.value) return

  installDialogOpen.value = false
  transferDialogOpen.value = true
}

void fetchCapabilities()
</script>

<template>
  <ModemDetailHeader
    :modem="modem"
    :is-loading="isLoading"
    :show-details-action="isEsimModem"
    @open-details="detailDialogOpen = true"
  />

  <SimPinUnlockDialog
    v-if="modem"
    v-model:open="pinUnlockDialogOpen"
    v-model:pin="pinUnlockInput"
    :is-submitting="isPinUnlocking"
    :error="pinUnlockError"
    :lock-type="modem.unlockRequired"
    @submit="handlePinUnlockSubmit"
    @cancel="handlePinUnlockCancel"
  />

  <div
    v-if="!modem && !isLoading"
    class="rounded-2xl border border-dashed border-border p-8 text-sm text-muted-foreground"
  >
    {{ t('modemDetail.unknown') }}
  </div>

  <Alert v-if="needsPinUnlock && !pinUnlockDialogOpen">
    <AlertTitle>{{ t('modemDetail.unlock.lockedTitle') }}</AlertTitle>
    <AlertDescription class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <span>{{ t('modemDetail.unlock.lockedDescription') }}</span>
      <Button type="button" size="sm" class="w-full sm:w-auto" @click="openPinUnlockDialog">
        {{ t('modemDetail.unlock.submit') }}
      </Button>
    </AlertDescription>
  </Alert>

  <!-- SIM Slot Switcher -->
  <SimSlotSwitcher
    v-if="modem && !needsPinUnlock"
    v-model="currentSimIdentifier"
    :slots="simSlots"
    :signal-quality="modem.signalQuality"
    :registration-state="modem.registrationState"
    :access-technology="modem.accessTechnology"
    :registered-operator-name="modem.registeredOperator.name"
    :wifi-calling-connected="modem.wifiCallingConnected"
    :on-switch="handleSimSwitch"
  />

  <!-- eSIM modem: show original layout -->
  <div v-if="modem && isEsimModem" class="space-y-3">
    <EsimSummaryCard :modem="modem" :euicc="euicc" />
    <EsimProfileSection
      v-model:profiles="esimProfiles"
      :loading="isEsimProfilesLoading"
      :modem-id="modemId"
      :refresh-modem="refreshModem"
      @success="showSuccess"
    />
  </div>

  <!-- Physical modem: show detail card -->
  <div v-if="modem && isPhysicalModem" class="space-y-3">
    <ModemDetailCard :modem="modem" :euicc="null" />
  </div>

  <Dialog v-model:open="detailDialogOpen">
    <DialogContent v-if="modem && isEsimModem" class="sm:max-w-lg">
      <DialogHeader>
        <DialogTitle>{{ t('modemDetail.tabs.detail') }}</DialogTitle>
      </DialogHeader>
      <ScrollArea class="pr-2 [&_[data-slot=scroll-area-viewport]]:max-h-[70vh]">
        <ModemDetailCard :modem="modem" :euicc="euicc" />
      </ScrollArea>
    </DialogContent>
  </Dialog>

  <DraggableFab
    v-if="modem && isEsimModem"
    :ariaLabel="t('modemDetail.esim.installButton')"
    :title="t('modemDetail.esim.installButton')"
    @click="installDialogOpen = true"
  >
    <Download class="size-5" />
  </DraggableFab>

  <EsimInstallDialog
    ref="installDialogRef"
    v-model:open="installDialogOpen"
    :is-discovering="isDiscoverLoading"
    :allow-transfer="canTransferEsim"
    @confirm="startDownload"
    @discover="openDiscoverDialog"
    @transfer="openTransferDialog"
  />

  <EsimTransferDialog
    v-if="canTransferEsim"
    v-model:open="transferDialogOpen"
    :modem-id="modemId"
    @completed="fetchEsimProfiles(modemId)"
  />

  <EsimDiscoverDialog
    v-model:open="discoverDialogOpen"
    v-model:selected-address="selectedDiscoverAddress"
    :options="discoverOptions"
    :is-loading="isDiscoverLoading"
    :has-options="hasDiscoverOptions"
    :has-selection="hasDiscoverSelection"
    @confirm="confirmDiscoverSelection"
  />

  <EsimDownloadProgressModal
    :open="isProgressModalOpen"
    :title="progressTitle"
    :stage-label="stageLabel"
    :progress="progress"
    :cancel-label="t('modemDetail.actions.cancel')"
    @cancel="cancelDownload"
  />

  <EsimDownloadPreviewModal
    :open="isPreviewModalOpen"
    :title="t('modemDetail.esim.downloadPreviewTitle')"
    :hint="t('modemDetail.esim.downloadPreviewHint')"
    :profile="previewProfile"
    :confirm-label="t('modemDetail.actions.confirm')"
    :cancel-label="t('modemDetail.actions.cancel')"
    @confirm="handlePreviewConfirm"
    @cancel="handlePreviewCancel"
  />

  <EsimDownloadConfirmationModal
    v-model:code="confirmationCode"
    :open="isConfirmationModalOpen"
    :title="confirmationTitle"
    :hint="confirmationHint"
    :placeholder="confirmationPlaceholder"
    :confirm-label="t('modemDetail.actions.confirm')"
    :cancel-label="t('modemDetail.actions.cancel')"
    @submit="handleConfirmationSubmit"
    @cancel="cancelDownload"
  />

  <EsimDownloadResultModal
    :open="isResultModalOpen"
    :title="resultTitle"
    :message="resultMessage"
    :confirm-label="t('modemDetail.actions.confirm')"
    :tone="resultTone"
    @confirm="handleResultConfirm"
  />
</template>
