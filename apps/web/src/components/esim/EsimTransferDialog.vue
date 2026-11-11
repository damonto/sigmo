<script setup lang="ts">
import type { TemplateRef } from 'vue'
import { computed, nextTick, ref, useId, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowLeftRight } from 'lucide-vue-next'

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
import { Field, FieldLabel } from '@/components/ui/field'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  InputGroupText,
} from '@/components/ui/input-group'
import { useEsimTransfer } from '@/composables/useEsimTransfer'
import {
  TRANSFER_CLIENT_ERROR,
  TRANSFER_STATE,
  transferStageLabelKey,
} from '@/constants/esimTransfer'
import type { EsimTransferSource } from '@/types/esim'

const props = defineProps<{
  modemId: string
  targetSeId: string
  remote?: boolean
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
const {
  canStartTransfer,
  submitUserInput,
  confirmSourceDeletion,
  completeWebsheet,
  hasSources,
  sources,
  ccidError,
  sourceImei,
  selectedSource,
  profiles,
  selectedProfile,
  needsSourceImei,
  userInputResponse,
  userInput,
  progress,
  sourceDeletionICCID,
  carrierWebsheet,
} = transfer
const isError = computed(() => transfer.state.value === TRANSFER_STATE.error)

type TransferHeaderFocus = {
  focus: () => void
}

const remoteAddress = ref('')
const remoteAddressId = useId()
const requestRemote = () => {
  profileDialogOpen.value = true
  transferStarted.value = true
  transfer.connectRemote(props.targetSeId, remoteAddress.value)
}
const remoteFormOpen = computed(() => props.remote && !transfer.remote.value)
const profileDialogOpen = ref(false)
const startConfirmOpen = ref(false)
const transferStarted = ref(false)
const canChangeSource = computed(
  () =>
    !props.remote && profileDialogOpen.value && !transferStarted.value && !startConfirmOpen.value,
)
const sourceHeader = useTemplateRef<TransferHeaderFocus>('sourceHeader')
const transferHeader = useTemplateRef<TransferHeaderFocus>('transferHeader')

const sourceDialogOpen = computed(() => open.value && !profileDialogOpen.value)
const promptOpen = computed(() => transfer.state.value === TRANSFER_STATE.userInput)
const transferDialogOpen = computed(
  () => open.value && profileDialogOpen.value && (!transferStarted.value || promptOpen.value),
)
const sourceLoading = computed(() => transfer.state.value === TRANSFER_STATE.loadingSources)
const profileLoading = computed(() => transfer.state.value === TRANSFER_STATE.loadingProfiles)
const ready = computed(() => transfer.state.value === TRANSFER_STATE.ready)
const terminal = computed(
  () =>
    transfer.state.value === TRANSFER_STATE.completed ||
    transfer.state.value === TRANSFER_STATE.error,
)
const sourceDeletionOpen = computed(
  () =>
    open.value && transferStarted.value && transfer.state.value === TRANSFER_STATE.sourceDeletion,
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
  if (transfer.state.value === TRANSFER_STATE.completed)
    return t('modemDetail.esim.transferCompleted')
  return ''
})
const transferResultMessage = computed(() => {
  if (transfer.state.value === TRANSFER_STATE.error) {
    return transferErrorMessage.value
  }
  if (transfer.state.value === TRANSFER_STATE.completed) {
    const name =
      transfer.downloadedName.value || t('modemDetail.esim.downloadCompletedFallbackName')
    return t('modemDetail.esim.downloadCompletedMessage', { name })
  }
  return ''
})
const transferErrorMessage = computed(() => {
  switch (transfer.errorMessage.value) {
    case TRANSFER_CLIENT_ERROR.invalidResponse:
      return t('modemDetail.esim.transferInvalidResponse')
    case TRANSFER_CLIENT_ERROR.connectionClosed:
      return props.remote ? t('esimPeer.uncertain') : t('modemDetail.esim.transferConnectionClosed')
    case 'transfer_result_uncertain':
      return t('esimPeer.uncertain')
    case 'transfer_remote_unsupported':
      return t('esimPeer.unsupported')
    case 'transfer_remote_failed':
      return t('esimPeer.failed')
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

const changeSource = () => {
  if (!canChangeSource.value) return
  profileDialogOpen.value = false
  void transfer.loadSources()
}

const selectProfileID = (id: string) => {
  const profile = transfer.profiles.value.find((item) => item.id === id)
  if (!profile?.supported) return
  transfer.selectedProfile.value = profile
}

const requestStartTransfer = () => {
  if (!props.targetSeId.trim()) return
  if (!transfer.canStartTransfer.value) return
  if (props.remote) {
    return
  }
  startConfirmOpen.value = true
}

const confirmStartTransfer = () => {
  if (!props.targetSeId.trim()) return
  if (!transfer.canStartTransfer.value) return
  startConfirmOpen.value = false
  transferStarted.value = true
  transfer.startTransfer(props.targetSeId)
}

const focusDialogHeader = (target: TemplateRef<TransferHeaderFocus>, event: Event) => {
  event.preventDefault()
  void nextTick(() => target.value?.focus())
}

const focusSourceDialogHeader = (event: Event) => {
  focusDialogHeader(sourceHeader, event)
}

const focusTransferDialogHeader = (event: Event) => {
  focusDialogHeader(transferHeader, event)
}

watch(
  open,
  (value) => {
    startConfirmOpen.value = false
    transferStarted.value = false
    profileDialogOpen.value = false
    if (!value) {
      transfer.cancelTransfer()
      return
    }
    remoteAddress.value = ''
    if (props.remote) transfer.cancelTransfer()
    else void transfer.loadSources()
  },
  { immediate: true },
)
</script>

<template>
  <Dialog
    :open="sourceDialogOpen"
    @update:open="handleDialogOpen"
  >
    <EsimPersistentDialogContent
      class="max-h-[calc(100vh-2rem)] overflow-hidden sm:max-w-md"
      @open-auto-focus="focusSourceDialogHeader"
    >
      <EsimTransferHeader ref="sourceHeader" />

      <form
        v-if="remoteFormOpen"
        class="space-y-4"
        @submit.prevent="requestRemote()"
      >
        <Field class="gap-2">
          <FieldLabel :for="remoteAddressId">{{ t('esimPeer.address') }}</FieldLabel>
          <InputGroup>
            <InputGroupInput
              :id="remoteAddressId"
              v-model="remoteAddress"
              type="url"
              placeholder="https://sigmo.example.com"
              required
            />
            <InputGroupAddon aria-hidden="true">
              <InputGroupText>http(s)://</InputGroupText>
            </InputGroupAddon>
          </InputGroup>
        </Field>
        <Button
          type="submit"
          class="w-full"
          :disabled="!remoteAddress.trim()"
          >{{ t('esimPeer.request') }}</Button
        >
      </form>
      <EsimTransferSourceStep
        v-if="!props.remote"
        :loading="sourceLoading"
        :ready="ready"
        :has-sources="hasSources"
        :sources="sources"
        :ccid-error="ccidError"
        :error-message="isError ? transferErrorMessage : ''"
        @select="selectSource"
      />

      <DialogFooter v-if="props.remote || ready || isError">
        <Button
          variant="ghost"
          type="button"
          class="w-full"
          @click="closeDialog"
        >
          {{ isError ? t('modemDetail.actions.confirm') : t('modemDetail.actions.cancel') }}
        </Button>
      </DialogFooter>
    </EsimPersistentDialogContent>
  </Dialog>

  <Dialog
    :open="transferDialogOpen"
    @update:open="handleDialogOpen"
  >
    <EsimPersistentDialogContent
      class="flex max-h-[70dvh] flex-col overflow-hidden sm:max-w-md"
      @open-auto-focus="focusTransferDialogHeader"
    >
      <EsimTransferHeader
        ref="transferHeader"
        class="shrink-0"
      />

      <EsimTransferProfileStep
        v-if="!promptOpen"
        v-model:source-imei="sourceImei"
        :selected-source="selectedSource"
        :selected-source-detail="selectedSourceDetail"
        :loading="profileLoading"
        :ready="ready"
        :stage-label="stageLabel"
        :profiles="profiles"
        :selected-profile-id="selectedProfile?.id"
        :needs-source-imei="needsSourceImei"
        :error-message="isError ? transferErrorMessage : ''"
        @select-profile="selectProfileID"
      >
        <template
          v-if="canChangeSource"
          #source-actions
        >
          <Button
            variant="ghost"
            size="icon-sm"
            type="button"
            class="shrink-0"
            :aria-label="t('modemDetail.esim.transferChangeSourceLabel')"
            :title="t('modemDetail.esim.transferChangeSourceLabel')"
            @click="changeSource"
          >
            <ArrowLeftRight
              class="size-4"
              aria-hidden="true"
            />
          </Button>
        </template>
      </EsimTransferProfileStep>

      <EsimTransferUserInputPrompt
        v-else
        v-model:response="userInputResponse"
        :input="userInput"
        @submit="submitUserInput"
      />

      <DialogFooter
        v-if="ready || isError"
        class="shrink-0 grid grid-cols-1 gap-3 sm:grid-cols-2"
      >
        <Button
          variant="outline"
          type="button"
          :class="['w-full', ready ? 'order-2 sm:order-1' : 'sm:col-span-2']"
          @click="closeDialog"
        >
          {{ t('modemDetail.actions.cancel') }}
        </Button>
        <Button
          v-if="ready"
          type="button"
          class="order-1 w-full sm:order-2"
          :disabled="!props.targetSeId.trim() || !canStartTransfer"
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
    :progress="progress"
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
    :iccid="sourceDeletionICCID"
    @cancel="confirmSourceDeletion(false)"
    @confirm="confirmSourceDeletion(true)"
  />

  <CarrierWebsheetDialog
    :open="carrierWebsheetOpen"
    :websheet="carrierWebsheet"
    @cancel="closeDialog"
    @done="completeWebsheet"
  />

  <EsimTransferStartConfirmAlert
    :open="startConfirmOpen"
    @cancel="startConfirmOpen = false"
    @confirm="confirmStartTransfer"
  />
</template>
