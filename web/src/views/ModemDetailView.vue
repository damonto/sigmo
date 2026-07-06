<script setup lang="ts">
import { CreditCard, Download } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { toast } from 'vue-sonner'

import { useModemApi } from '@/apis/modem'
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
import ModemLineMsisdnDialog from '@/components/modem/settings/ModemLineMsisdnDialog.vue'
import SimApplicationDialog from '@/components/modem/SimApplicationDialog.vue'
import SimPinUnlockDialog from '@/components/modem/SimPinUnlockDialog.vue'
import SimSlotSwitcher from '@/components/modem/SimSlotSwitcher.vue'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { ScrollArea } from '@/components/ui/scroll-area'
import { FEATURE, useCapabilities } from '@/composables/useCapabilities'
import { useEsimDiscover } from '@/composables/useEsimDiscover'
import { useEsimDownload } from '@/composables/useEsimDownload'
import { useEsimProfileQuickActions } from '@/composables/useEsimProfileQuickActions'
import { useModemDetail } from '@/composables/useModemDetail'
import { useSimApplicationSession } from '@/composables/useSimApplicationSession'
import { useSimSlotSwitch } from '@/composables/useSimSlotSwitch'
import type { EsimProfile } from '@/types/esim'

const route = useRoute()
const { t } = useI18n()
const { hasFeature, fetchCapabilities } = useCapabilities()
const modemApi = useModemApi()

const modemId = computed(() => (route.params.id ?? 'unknown') as string)
const canTransferEsim = computed(() => hasFeature(FEATURE.esimTransfer))
const canUseWiFiCalling = computed(() => hasFeature(FEATURE.wifiCalling))
const {
  modem,
  seInfo,
  esimProfiles,
  isLoading,
  isSELoading,
  isEsimProfilesLoading,
  isPhysicalModem,
  isEsimModem,
  fetchModemDetail,
  fetchEsimProfiles,
} = useModemDetail()

const {
  available: simApplicationAvailable,
  profileIccid: simApplicationProfileIccid,
  currentView: simApplicationView,
  dialogOpen: simApplicationDialogOpen,
  errorMessage: simApplicationErrorMessage,
  openRootMenu: openSimApplication,
  selectMenuItem: selectSimApplicationMenuItem,
  submitInput: submitSimApplicationInput,
  submitInkey: submitSimApplicationInkey,
  respondConfirm: respondSimApplicationConfirm,
  back: backSimApplication,
} = useSimApplicationSession(modemId)

const installDialogRef = ref<{
  applyDiscoverAddress: (address: string, seId?: string) => void
} | null>(null)

const installDialogOpen = ref(false)
const transferDialogOpen = ref(false)
const transferSEID = ref('')
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
const activeProfileActionsProfileId = ref<string | null>(null)

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
const isInstallDisabled = computed(
  () => isSELoading.value || seInfo.value === null || seInfo.value.ses.length === 0,
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

const refreshModem = async (targetId = modemId.value) => {
  if (!targetId || targetId === 'unknown') return
  await fetchModemDetail(targetId)
}

const openInstallDialog = () => {
  if (isInstallDisabled.value) return
  installDialogOpen.value = true
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

const shouldLoadProfileQuickActions = computed(() => activeProfileActionsProfileId.value !== null)
const shouldLoadWiFiCallingQuickActions = computed(
  () => canUseWiFiCalling.value && activeProfileActionsProfileId.value !== null,
)

const {
  msisdnDialogOpen,
  msisdnInput,
  isMsisdnUpdating,
  isMsisdnValid,
  isInternetConnected,
  isInternetBusy,
  settingsWiFiCallingEnabled,
  settingsWiFiCallingConnected,
  isWiFiCallingBusy,
  openMsisdnDialog,
  saveMsisdn,
  handleNetworkQuickToggle,
  handleWiFiCallingQuickToggle,
} = useEsimProfileQuickActions({
  modemId,
  modem,
  canUseWiFiCalling,
  loadInternetConnection: shouldLoadProfileQuickActions,
  loadWiFiCallingSettings: shouldLoadWiFiCallingQuickActions,
  refreshModem,
  onSuccess: showSuccess,
})

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
  applyDiscoverAddress: (address: string, seId: string) => {
    installDialogRef.value?.applyDiscoverAddress(address, seId)
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

watch(modemId, () => {
  activeProfileActionsProfileId.value = null
})

watch(simApplicationErrorMessage, (message) => {
  if (!message) return
  toast.error(t('modemDetail.simApplication.errorTitle'), {
    description: message,
  })
})

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

const openTransferDialog = (seId: string) => {
  if (!canTransferEsim.value) return
  if (!seId.trim()) return

  transferSEID.value = seId.trim()
  installDialogOpen.value = false
  transferDialogOpen.value = true
}

const handleProfileActionsOpenChange = (profile: EsimProfile, open: boolean) => {
  if (!profile.enabled) return
  if (open) {
    activeProfileActionsProfileId.value = profile.id
    return
  }
  if (activeProfileActionsProfileId.value === profile.id) {
    activeProfileActionsProfileId.value = null
  }
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
    :airplane-mode="modem.airplaneMode"
    :on-switch="handleSimSwitch"
  />

  <!-- eSIM modem: show original layout -->
  <div v-if="modem && isEsimModem" class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_22rem]">
    <div class="min-w-0 space-y-3">
      <EsimSummaryCard :modem="modem" :se-info="seInfo" />
      <EsimProfileSection
        v-model:profiles="esimProfiles"
        :loading="isEsimProfilesLoading"
        :modem-id="modemId"
        :refresh-modem="refreshModem"
        :internet-connected="isInternetConnected"
        :internet-busy="isInternetBusy"
        :wifi-calling-available="canUseWiFiCalling"
        :wifi-calling-enabled="settingsWiFiCallingEnabled"
        :wifi-calling-connected="settingsWiFiCallingConnected"
        :wifi-calling-busy="isWiFiCallingBusy"
        :sim-application-available="simApplicationAvailable"
        :sim-application-profile-iccid="simApplicationProfileIccid"
        @success="showSuccess"
        @toggle-network="handleNetworkQuickToggle"
        @toggle-wifi-calling="handleWiFiCallingQuickToggle"
        @edit-phone-number="openMsisdnDialog"
        @profile-actions-open-change="handleProfileActionsOpenChange"
        @open-sim-application="openSimApplication"
      />
    </div>

    <aside class="hidden xl:block">
      <div class="sticky top-(--modem-desktop-sticky-top)">
        <ModemDetailCard :modem="modem" :se-info="seInfo" />
      </div>
    </aside>
  </div>

  <!-- Physical modem: show detail card -->
  <div v-if="modem && isPhysicalModem" class="space-y-3">
    <ModemDetailCard :modem="modem" :se-info="null" />
    <Button
      v-if="simApplicationAvailable"
      type="button"
      variant="outline"
      class="w-full justify-start gap-2"
      @click="openSimApplication"
    >
      <CreditCard class="size-4" />
      {{ t('modemDetail.simApplication.title') }}
    </Button>
  </div>

  <Dialog v-model:open="detailDialogOpen">
    <DialogContent v-if="modem && isEsimModem" class="sm:max-w-lg">
      <DialogHeader>
        <DialogTitle>{{ t('modemDetail.tabs.detail') }}</DialogTitle>
        <DialogDescription class="sr-only">
          {{ t('modemDetail.tabs.detail') }}
        </DialogDescription>
      </DialogHeader>
      <ScrollArea class="pr-2 **:data-[slot=scroll-area-viewport]:max-h-[70vh]">
        <ModemDetailCard :modem="modem" :se-info="seInfo" />
      </ScrollArea>
    </DialogContent>
  </Dialog>

  <ModemLineMsisdnDialog
    v-model:open="msisdnDialogOpen"
    v-model:msisdn="msisdnInput"
    :is-updating="isMsisdnUpdating"
    :is-valid="isMsisdnValid"
    @save="saveMsisdn"
  />

  <SimApplicationDialog
    v-model:open="simApplicationDialogOpen"
    :view="simApplicationView"
    @select-menu-item="selectSimApplicationMenuItem"
    @submit-input="submitSimApplicationInput"
    @submit-inkey="submitSimApplicationInkey"
    @respond-confirm="respondSimApplicationConfirm"
    @back="backSimApplication"
  />

  <DraggableFab
    v-if="modem && isEsimModem"
    :ariaLabel="t('modemDetail.esim.installButton')"
    :title="t('modemDetail.esim.installButton')"
    :disabled="isInstallDisabled"
    @click="openInstallDialog"
  >
    <Download class="size-5" />
  </DraggableFab>

  <EsimInstallDialog
    ref="installDialogRef"
    v-model:open="installDialogOpen"
    :is-discovering="isDiscoverLoading"
    :allow-transfer="canTransferEsim"
    :ses="seInfo?.ses ?? []"
    @confirm="startDownload"
    @discover="(seId) => openDiscoverDialog(seId)"
    @transfer="openTransferDialog"
  />

  <EsimTransferDialog
    v-if="canTransferEsim"
    v-model:open="transferDialogOpen"
    :modem-id="modemId"
    :target-se-id="transferSEID"
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
