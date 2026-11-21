<script setup lang="ts">
import {
  Bell,
  CalendarClock,
  ChevronRight,
  CreditCard,
  Download,
  EllipsisVertical,
  Info,
} from 'lucide-vue-next'
import { computed, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { toast } from 'vue-sonner'

import { useModemApi } from '@/apis/modem'
import { useReminderApi } from '@/apis/reminder'
import ReminderDialog from '@/components/ReminderDialog.vue'
import { formatReminderTimestamp } from '@/lib/datetime'
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
import AutomationScheduleBadge from '@/components/modem/AutomationScheduleBadge.vue'
import ModemDetailCard from '@/components/modem/ModemDetailCard.vue'
import ModemDetailHeader from '@/components/modem/ModemDetailHeader.vue'
import ModemLineMsisdnDialog from '@/components/modem/settings/ModemLineMsisdnDialog.vue'
import SimAutomationsDialog from '@/components/modem/SimAutomationsDialog.vue'
import SimApplicationDialog from '@/components/modem/SimApplicationDialog.vue'
import SimPinDialog from '@/components/modem/SimPinDialog.vue'
import { useSimPin } from '@/composables/useSimPin'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
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
import { refreshModemList } from '@/composables/useModems'
import { automationLineKey, useScheduledAutomations } from '@/composables/useScheduledAutomations'
import { useSimApplicationSession } from '@/composables/useSimApplicationSession'
import { useSimSlotSwitch } from '@/composables/useSimSlotSwitch'
import type { AutomationLine } from '@/types/automation'
import type { EsimProfile } from '@/types/esim'
import type { ReminderPayload } from '@/types/reminder'

const route = useRoute()
const { t } = useI18n()
const { hasFeature, fetchCapabilities } = useCapabilities()
const modemApi = useModemApi()
const reminderApi = useReminderApi()

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
  isSIMKindUnknown,
  isPhysicalModem,
  isEsimModem,
  fetchModemDetail,
  fetchEsimProfiles,
} = useModemDetail()

const simPinIdentity = computed(
  () =>
    `${modem.value?.sim.slot ?? ''}:${modem.value?.sim.identifier ?? ''}:${esimProfiles.value
      .filter((profile) => profile.enabled)
      .map((profile) => profile.id)
      .join(',')}`,
)
const simPin = useSimPin(modemId, simPinIdentity)
const needsSIMUnlock = computed(
  () =>
    modem.value?.state === 'locked' && (simPin.legacy.value ? modem.value.unlockSupported : true),
)
const openSIMUnlock = () => (simPin.legacy.value ? openPinUnlockDialog() : simPin.show())

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

const installDialogRef = useTemplateRef<{
  applyDiscoverAddress: (address: string, seId?: string) => void
}>('installDialogRef')

const installDialogOpen = ref(false)
const transferDialogOpen = ref(false)
const transferSEID = ref('')
const remoteTransfer = ref(false)
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
const automationLine = ref<AutomationLine>()
const scheduleModemId = computed(() => {
  if (modem.value?.id !== modemId.value) return
  if (isEsimModem.value || isPhysicalModem.value) return modemId.value
})
const { schedules: automationSchedules, refresh: refreshAutomationSchedules } =
  useScheduledAutomations(scheduleModemId)
const physicalAutomation = computed(() =>
  automationSchedules.value.get(automationLineKey(modem.value?.sim.identifier ?? '')),
)
const openESIMAutomations = (profile: EsimProfile) => {
  automationLine.value = {
    profileId: profile.iccid,
    modemId: modemId.value,
    seId: profile.seId,
    label: profile.name,
  }
}
const openPhysicalAutomations = () => {
  if (!modem.value?.sim.identifier) return
  automationLine.value = {
    profileId: modem.value.sim.identifier,
    modemId: modemId.value,
    label: physicalProfileName.value,
  }
}
watch(modemId, () => {
  automationLine.value = undefined
})
watch(
  () => modem.value?.sim.identifier,
  (profileId) => {
    const selected = automationLine.value
    if (selected && !selected.seId && selected.profileId !== profileId) {
      automationLine.value = undefined
    }
  },
)

const physicalReminderOpen = ref(false)
const physicalReminderSaving = ref(false)
const physicalReminderDeleting = ref(false)

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

const refreshAfterSimSwitch = () => {
  // List refresh errors are reported by the API client independently of the SIM switch.
  void refreshModemList().catch(() => undefined)
  return refreshModem()
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

  const targetId = modemId.value
  const targetSIM = simPinIdentity.value
  isPinUnlocking.value = true
  pinUnlockError.value = ''
  try {
    await modemApi.unlockSim(targetId, pinUnlockInput.value)
    if (targetId !== modemId.value || targetSIM !== simPinIdentity.value) return
    pinUnlockDismissedFor.value = ''
    pinUnlockDialogOpen.value = false
    pinUnlockInput.value = ''
    toast.success(t('modemDetail.unlock.success'))
    await refreshModem()
  } catch (err) {
    if (targetId !== modemId.value || targetSIM !== simPinIdentity.value) return
    pinUnlockError.value = err instanceof Error ? err.message : t('modemDetail.unlock.error')
  } finally {
    if (targetId === modemId.value && targetSIM === simPinIdentity.value) {
      pinUnlockInput.value = ''
      isPinUnlocking.value = false
    }
  }
}

const showSuccess = (message: string) => {
  toast.success(message)
}

const physicalProfileName = computed(() => {
  if (!modem.value) return ''
  return modem.value.sim.operatorName || modem.value.sim.identifier
})

const closePhysicalReminder = () => {
  if (physicalReminderSaving.value || physicalReminderDeleting.value) return
  physicalReminderOpen.value = false
}

const savePhysicalReminder = async (payload: ReminderPayload) => {
  if (!modem.value?.sim.identifier) return
  physicalReminderSaving.value = true
  try {
    const { data } = await reminderApi.savePhysicalReminder(
      modemId.value,
      modem.value.sim.identifier,
      payload,
    )
    modem.value.sim.reminder = data.value ?? {
      nextAt: payload.scheduledAt,
      repeatDays: payload.repeatDays,
      content: payload.content,
    }
    physicalReminderOpen.value = false
    showSuccess(t('modemDetail.reminder.saved'))
  } catch (err) {
    console.error('[ModemDetailView] Failed to save physical SIM reminder:', err)
  } finally {
    physicalReminderSaving.value = false
  }
}

const clearPhysicalReminder = async () => {
  if (!modem.value?.sim.identifier) return
  physicalReminderDeleting.value = true
  try {
    await reminderApi.deletePhysicalReminder(modemId.value, modem.value.sim.identifier)
    modem.value.sim.reminder = undefined
    physicalReminderOpen.value = false
    showSuccess(t('modemDetail.reminder.cleared'))
  } catch (err) {
    console.error('[ModemDetailView] Failed to clear physical SIM reminder:', err)
  } finally {
    physicalReminderDeleting.value = false
  }
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

const { currentSimSlot, simSlots, handleSimSwitch } = useSimSlotSwitch({
  modemId,
  modem,
  refreshModem: refreshAfterSimSwitch,
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
  [needsPinUnlock, modemId, simPin.legacy],
  ([needsUnlock, id]) => {
    if (!needsUnlock) {
      pinUnlockDialogOpen.value = false
      pinUnlockDismissedFor.value = ''
      pinUnlockError.value = ''
      return
    }
    if (simPin.legacy.value && pinUnlockDismissedFor.value !== id) {
      pinUnlockDialogOpen.value = true
    }
  },
  { immediate: true },
)

watch([modemId, simPinIdentity], () => {
  activeProfileActionsProfileId.value = null
  pinUnlockInput.value = ''
  pinUnlockError.value = ''
  pinUnlockDialogOpen.value = false
  isPinUnlocking.value = false
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
  >
    <template #actions>
      <Button
        v-if="isEsimModem"
        variant="ghost"
        size="icon"
        type="button"
        class="@modem-detail/modem:hidden"
        aria-haspopup="dialog"
        :aria-label="t('modemDetail.tabs.detail')"
        :title="t('modemDetail.tabs.detail')"
        @click="detailDialogOpen = true"
      >
        <Info
          class="size-4 text-muted-foreground"
          aria-hidden="true"
        />
      </Button>
    </template>
  </ModemDetailHeader>

  <SimPinDialog
    :controller="simPin"
    @updated="refreshModem"
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

  <Alert v-if="needsSIMUnlock && !pinUnlockDialogOpen && !simPin.open.value">
    <AlertTitle>{{ t('modemDetail.unlock.lockedTitle') }}</AlertTitle>
    <AlertDescription class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <span>{{
        simPin.legacy.value ? t('modemDetail.unlock.lockedDescription') : t('simPin.description')
      }}</span>
      <Button
        type="button"
        size="sm"
        class="w-full sm:w-auto"
        @click="openSIMUnlock"
      >
        {{ t('modemDetail.unlock.submit') }}
      </Button>
    </AlertDescription>
  </Alert>

  <!-- SIM Slot Switcher -->
  <SimSlotSwitcher
    v-if="modem && !needsPinUnlock"
    v-model="currentSimSlot"
    :slots="simSlots"
    :signal-quality="modem.signalQuality"
    :registration-state="modem.registrationState"
    :access-technology="modem.accessTechnology"
    :registered-operator-name="modem.registeredOperator.name"
    :wifi-calling-connected="modem.wifiCallingConnected"
    :airplane-mode="modem.airplaneMode"
    :on-switch="handleSimSwitch"
  />

  <div
    v-if="modem && isEsimModem"
    class="grid gap-4 @modem-detail/modem:grid-cols-[minmax(22rem,1fr)_clamp(16rem,27vw,26rem)]"
  >
    <div class="min-w-0 space-y-3">
      <EsimSummaryCard
        :modem="modem"
        :se-info="seInfo"
      />
      <EsimProfileSection
        :automation-schedules="automationSchedules"
        @open-automations="openESIMAutomations"
        :sim-pin-available="simPin.available.value"
        :sim-pin-profile-iccid="modem.sim.identifier"
        @open-sim-pin="simPin.show"
        v-model:profiles="esimProfiles"
        :loading="isEsimProfilesLoading"
        :modem-id="modemId"
        :refresh-modem="refreshAfterSimSwitch"
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

    <aside
      class="hidden min-w-0 self-start rounded-xl border bg-card p-4 @modem-detail/modem:block lg:block"
    >
      <ModemDetailCard
        :modem="modem"
        :se-info="seInfo"
      />
    </aside>
  </div>

  <!-- Physical modem: show detail card -->
  <div
    v-else-if="modem && isPhysicalModem"
    class="space-y-3"
  >
    <div class="flex flex-wrap items-center gap-2">
      <DropdownMenu v-if="simPin.available.value">
        <DropdownMenuTrigger as-child>
          <Button
            variant="outline"
            size="icon"
            :aria-label="t('simPin.simActions')"
            ><EllipsisVertical class="size-4"
          /></Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent
          align="start"
          class="w-56"
        >
          <DropdownMenuItem @click="simPin.show">{{ t('simPin.title') }}</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <Button
        v-if="modem.sim.identifier"
        type="button"
        variant="outline"
        class="justify-start gap-2"
        @click="physicalReminderOpen = true"
      >
        <Bell class="size-4" />
        {{ t('modemDetail.reminder.title') }}
      </Button>
      <Button
        v-if="modem.sim.identifier"
        type="button"
        variant="outline"
        class="justify-start gap-2"
        @click="openPhysicalAutomations"
      >
        <CalendarClock
          class="size-4"
          aria-hidden="true"
        />
        {{ t('automation.title') }}
      </Button>
      <Button
        v-if="simApplicationAvailable"
        type="button"
        variant="outline"
        class="justify-start gap-2"
        @click="openSimApplication"
      >
        <CreditCard class="size-4" />
        {{ t('modemDetail.simApplication.title') }}
      </Button>
    </div>
    <button
      v-if="modem.sim.reminder"
      type="button"
      aria-haspopup="dialog"
      class="flex w-full items-start gap-3 rounded-lg border bg-card px-4 py-3 text-left text-sm text-card-foreground transition-colors hover:bg-accent/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      @click="physicalReminderOpen = true"
    >
      <Bell
        class="mt-0.5 size-4 shrink-0 text-primary"
        aria-hidden="true"
      />
      <span class="min-w-0 flex-1">
        <span class="block font-medium">{{ t('modemDetail.reminder.title') }}</span>
        <span class="mt-0.5 block space-y-1 leading-relaxed text-muted-foreground">
          <span class="block">
            {{ t('modemDetail.reminder.nextAt') }}:
            <time :datetime="modem.sim.reminder.nextAt">{{
              formatReminderTimestamp(modem.sim.reminder.nextAt)
            }}</time>
          </span>
          <span class="block">
            {{
              modem.sim.reminder.repeatDays
                ? t('modemDetail.reminder.everyDays', { days: modem.sim.reminder.repeatDays })
                : t('modemDetail.reminder.once')
            }}
          </span>
          <span class="block whitespace-pre-wrap wrap-break-word">{{
            modem.sim.reminder.content
          }}</span>
        </span>
      </span>
      <ChevronRight
        class="size-4 shrink-0 self-center text-muted-foreground"
        aria-hidden="true"
      />
    </button>
    <div
      v-if="physicalAutomation"
      class="rounded-lg border bg-card px-4 py-3"
    >
      <AutomationScheduleBadge
        :automation="physicalAutomation"
        @edit="openPhysicalAutomations"
      />
    </div>
    <ModemDetailCard
      :modem="modem"
      :se-info="null"
      class="lg:rounded-xl lg:border lg:bg-card lg:p-4"
    />
  </div>

  <div v-else-if="modem && isSIMKindUnknown">
    <ModemDetailCard
      :modem="modem"
      :se-info="null"
      class="lg:rounded-xl lg:border lg:bg-card lg:p-4"
    />
  </div>

  <Dialog v-model:open="detailDialogOpen">
    <DialogContent
      v-if="modem && isEsimModem"
      class="sm:max-w-lg"
    >
      <DialogHeader>
        <DialogTitle>{{ t('modemDetail.tabs.detail') }}</DialogTitle>
        <DialogDescription class="sr-only">
          {{ t('modemDetail.tabs.detail') }}
        </DialogDescription>
      </DialogHeader>
      <ScrollArea class="pr-2 **:data-[slot=scroll-area-viewport]:max-h-[70vh]">
        <ModemDetailCard
          :modem="modem"
          :se-info="seInfo"
        />
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

  <SimAutomationsDialog
    v-model="automationLine"
    @changed="refreshAutomationSchedules"
  />

  <ReminderDialog
    v-if="modem && isPhysicalModem && modem.sim.identifier"
    v-model:open="physicalReminderOpen"
    :profile-name="physicalProfileName"
    :reminder="modem.sim.reminder"
    :saving="physicalReminderSaving"
    :deleting="physicalReminderDeleting"
    @save="savePhysicalReminder"
    @clear="clearPhysicalReminder"
    @update:open="(value) => !value && closePhysicalReminder()"
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
    @transfer="
      (seId) => {
        remoteTransfer = false
        openTransferDialog(seId)
      }
    "
    @remote-transfer="
      (seId) => {
        remoteTransfer = true
        openTransferDialog(seId)
      }
    "
  />

  <EsimTransferDialog
    v-if="canTransferEsim"
    v-model:open="transferDialogOpen"
    :modem-id="modemId"
    :target-se-id="transferSEID"
    :remote="remoteTransfer"
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
