<script setup lang="ts">
import type { Ref } from 'vue'
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import CarrierWebsheetDialog from '@/components/CarrierWebsheetDialog.vue'
import EsimDownloadProgressModal from '@/components/esim/EsimDownloadProgressModal.vue'
import EsimDownloadResultModal from '@/components/esim/EsimDownloadResultModal.vue'
import EsimPersistentDialogContent from '@/components/esim/EsimPersistentDialogContent.vue'
import EsimSourceDeletionAlert from '@/components/esim/EsimSourceDeletionAlert.vue'
import EsimTransferHeader from '@/components/esim/EsimTransferHeader.vue'
import EsimTransferProfileStep from '@/components/esim/EsimTransferProfileStep.vue'
import EsimTransferSourceStep from '@/components/esim/EsimTransferSourceStep.vue'
import EsimTransferStartConfirmAlert from '@/components/esim/EsimTransferStartConfirmAlert.vue'
import EsimTransferUserInputPrompt from '@/components/esim/EsimTransferUserInputPrompt.vue'
import { Button } from '@/components/ui/button'
import { Dialog, DialogFooter } from '@/components/ui/dialog'
import { useEsimTransfer } from '@/composables/useEsimTransfer'
import {
  TRANSFER_CLIENT_ERROR,
  TRANSFER_STATE,
  transferStageLabelKey,
} from '@/constants/esimTransfer'
import type { EsimTransferSource } from '@/types/esim'

const props = defineProps<{
  modemId: string
}>()

const emit = defineEmits<{
  (event: 'completed'): void
}>()

const open = defineModel<boolean>('open', { required: true })
const { t } = useI18n()

const modemIdRef = computed(() => props.modemId)
const transfer = useEsimTransfer(modemIdRef, {
  onCompleted: () => emit('completed'),
})

type TransferHeaderFocus = {
  focus: () => void
}

const profileDialogOpen = ref(false)
const startConfirmOpen = ref(false)
const transferStarted = ref(false)
const sourceHeader = ref<TransferHeaderFocus | null>(null)
const transferHeader = ref<TransferHeaderFocus | null>(null)

const sourceDialogOpen = computed(() => open.value && !profileDialogOpen.value)
const promptOpen = computed(() => transfer.state.value === TRANSFER_STATE.userInput)
const transferDialogOpen = computed(
  () => open.value && profileDialogOpen.value && (!transferStarted.value || promptOpen.value),
)
const sourceLoading = computed(() => transfer.state.value === TRANSFER_STATE.loadingSources)
const profileLoading = computed(() => transfer.state.value === TRANSFER_STATE.loadingProfiles)
const ready = computed(() => transfer.state.value === TRANSFER_STATE.ready)
const terminal = computed(
  () => transfer.state.value === TRANSFER_STATE.completed || transfer.state.value === TRANSFER_STATE.error,
)
const sourceDeletionOpen = computed(
  () => open.value && transferStarted.value && transfer.state.value === TRANSFER_STATE.sourceDeletion,
)
const carrierWebsheetOpen = computed(
  () => open.value && transferStarted.value && transfer.state.value === TRANSFER_STATE.websheet,
)
const transferProgressOpen = computed(
  () =>
    open.value &&
    transferStarted.value &&
    (transfer.state.value === TRANSFER_STATE.connecting ||
      transfer.state.value === TRANSFER_STATE.progress),
)
const transferResultOpen = computed(() => open.value && transferStarted.value && terminal.value)
const selectedSourceDetail = computed(() => {
  const source = transfer.selectedSource.value
  if (!source) return ''
  if (source.type === 'ccid') return t('modemDetail.esim.transferCcid')
  return source.detail ?? ''
})
const stageLabel = computed(() => {
  const labelKey = transferStageLabelKey[transfer.stage.value]
  if (labelKey) return t(labelKey)
  if (transfer.stage.value) return transfer.stage.value
  return t('modemDetail.esim.transferStagePreparing')
})
const transferResultTone = computed(() =>
  transfer.state.value === TRANSFER_STATE.error ? 'error' : 'success',
)
const transferResultTitle = computed(() => {
  if (transfer.state.value === TRANSFER_STATE.error) return t('modemDetail.esim.transferFailed')
  if (transfer.state.value === TRANSFER_STATE.completed) return t('modemDetail.esim.transferCompleted')
  return ''
})
const transferResultMessage = computed(() => {
  if (transfer.state.value === TRANSFER_STATE.error) {
    return transferErrorMessage.value
  }
  if (transfer.state.value === TRANSFER_STATE.completed) {
    const name = transfer.downloadedName.value || t('modemDetail.esim.downloadCompletedFallbackName')
    return t('modemDetail.esim.downloadCompletedMessage', { name })
  }
  return ''
})
const transferErrorMessage = computed(() => {
  switch (transfer.errorMessage.value) {
    case TRANSFER_CLIENT_ERROR.invalidResponse:
      return t('modemDetail.esim.transferInvalidResponse')
    case TRANSFER_CLIENT_ERROR.connectionClosed:
      return t('modemDetail.esim.transferConnectionClosed')
    default:
      return transfer.errorMessage.value || t('modemDetail.esim.transferFailed')
  }
})

const closeDialog = () => {
  startConfirmOpen.value = false
  transferStarted.value = false
  profileDialogOpen.value = false
  transfer.cancelTransfer()
  open.value = false
}

const handleDialogOpen = (value: boolean) => {
  if (!value) closeDialog()
}

const selectSource = (source: EsimTransferSource) => {
  transfer.selectSource(source)
  profileDialogOpen.value = true
  void transfer.loadProfiles()
}

const selectProfileID = (id: string) => {
  const profile = transfer.profiles.value.find((item) => item.id === id)
  if (!profile?.supported) return
  transfer.selectedProfile.value = profile
}

const requestStartTransfer = () => {
  if (!transfer.canStartTransfer.value) return
  startConfirmOpen.value = true
}

const confirmStartTransfer = () => {
  if (!transfer.canStartTransfer.value) return
  startConfirmOpen.value = false
  transferStarted.value = true
  transfer.startTransfer()
}

const focusDialogHeader = (target: Ref<TransferHeaderFocus | null>, event: Event) => {
  event.preventDefault()
  void nextTick(() => target.value?.focus())
}

const focusSourceDialogHeader = (event: Event) => {
  focusDialogHeader(sourceHeader, event)
}

const focusTransferDialogHeader = (event: Event) => {
  focusDialogHeader(transferHeader, event)
}

watch(open, (value) => {
  if (value) {
    startConfirmOpen.value = false
    transferStarted.value = false
    profileDialogOpen.value = false
    void transfer.loadSources()
    return
  }
  startConfirmOpen.value = false
  transferStarted.value = false
  profileDialogOpen.value = false
  transfer.cancelTransfer()
})
</script>

<template>
  <Dialog :open="sourceDialogOpen" @update:open="handleDialogOpen">
    <EsimPersistentDialogContent
      class="max-h-[calc(100vh-2rem)] overflow-hidden sm:max-w-md"
      @open-auto-focus="focusSourceDialogHeader"
    >
      <EsimTransferHeader ref="sourceHeader" />

      <EsimTransferSourceStep
        :loading="sourceLoading"
        :ready="ready"
        :has-sources="transfer.hasSources.value"
        :sources="transfer.sources.value"
        :ccid-error="transfer.ccidError.value"
        :error-message="transfer.state.value === TRANSFER_STATE.error ? transferErrorMessage : ''"
        @select="selectSource"
      />

      <DialogFooter v-if="ready || transfer.state.value === TRANSFER_STATE.error">
        <Button variant="ghost" type="button" class="w-full" @click="closeDialog">
          {{ transfer.state.value === TRANSFER_STATE.error ? t('modemDetail.actions.confirm') : t('modemDetail.actions.cancel') }}
        </Button>
      </DialogFooter>
    </EsimPersistentDialogContent>
  </Dialog>

  <Dialog :open="transferDialogOpen" @update:open="handleDialogOpen">
    <EsimPersistentDialogContent
      class="flex max-h-[70dvh] flex-col overflow-hidden sm:max-w-md"
      @open-auto-focus="focusTransferDialogHeader"
    >
      <EsimTransferHeader ref="transferHeader" class="shrink-0" />

      <EsimTransferProfileStep
        v-if="transfer.state.value !== TRANSFER_STATE.userInput"
        v-model:source-imei="transfer.sourceImei.value"
        :selected-source="transfer.selectedSource.value"
        :selected-source-detail="selectedSourceDetail"
        :loading="profileLoading"
        :ready="ready"
        :stage-label="stageLabel"
        :profiles="transfer.profiles.value"
        :selected-profile-id="transfer.selectedProfile.value?.id"
        :needs-source-imei="transfer.needsSourceImei.value"
        :error-message="transfer.state.value === TRANSFER_STATE.error ? transferErrorMessage : ''"
        @select-profile="selectProfileID"
      />

      <EsimTransferUserInputPrompt
        v-else
        v-model:response="transfer.userInputResponse.value"
        :input="transfer.userInput.value"
        @submit="transfer.submitUserInput"
      />

      <DialogFooter
        v-if="ready || transfer.state.value === TRANSFER_STATE.error"
        class="shrink-0 grid grid-cols-1 gap-3 sm:grid-cols-2"
      >
        <Button
          variant="outline"
          type="button"
          :class="[
            'w-full',
            ready ? 'order-2 sm:order-1' : 'sm:col-span-2',
          ]"
          @click="closeDialog"
        >
          {{ t('modemDetail.actions.cancel') }}
        </Button>
        <Button
          v-if="ready"
          type="button"
          class="order-1 w-full sm:order-2"
          :disabled="!transfer.canStartTransfer.value"
          @click="requestStartTransfer"
        >
          {{ t('modemDetail.esim.transferStart') }}
        </Button>
      </DialogFooter>
    </EsimPersistentDialogContent>
  </Dialog>

  <EsimDownloadProgressModal
    :open="transferProgressOpen"
    :title="t('modemDetail.esim.downloadTitle')"
    :stage-label="stageLabel"
    :progress="transfer.progress.value"
    :cancel-label="t('modemDetail.actions.cancel')"
    @cancel="closeDialog"
  />

  <EsimDownloadResultModal
    :open="transferResultOpen"
    :title="transferResultTitle"
    :message="transferResultMessage"
    :confirm-label="t('modemDetail.actions.confirm')"
    :tone="transferResultTone"
    @confirm="closeDialog"
  />

  <EsimSourceDeletionAlert
    :open="sourceDeletionOpen"
    :iccid="transfer.sourceDeletionICCID.value"
    @cancel="transfer.confirmSourceDeletion(false)"
    @confirm="transfer.confirmSourceDeletion(true)"
  />

  <CarrierWebsheetDialog
    :open="carrierWebsheetOpen"
    :websheet="transfer.carrierWebsheet.value"
    @cancel="closeDialog"
    @done="transfer.completeWebsheet"
  />

  <EsimTransferStartConfirmAlert
    :open="startConfirmOpen"
    @cancel="startConfirmOpen = false"
    @confirm="confirmStartTransfer"
  />
</template>
