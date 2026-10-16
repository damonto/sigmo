<script setup lang="ts">
import { refDebounced } from '@vueuse/core'
import { Phone, Trash2 } from 'lucide-vue-next'
import { computed, nextTick, onBeforeUnmount, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import { useModemApi } from '@/apis/modem'
import { useUssdApi } from '@/apis/ussd'
import { useVoLTEApi } from '@/apis/volte'
import ContactQuickActions from '@/components/contacts/ContactQuickActions.vue'
import CommunicationActionButton from '@/components/communication/CommunicationActionButton.vue'
import CallHistoryRow from '@/components/communication/CallHistoryRow.vue'
import CallListSkeleton from '@/components/communication/CallListSkeleton.vue'
import CallHistoryFields from '@/components/communication/CallHistoryFields.vue'
import BackButton from '@/components/BackButton.vue'
import DraggableFab from '@/components/fab/DraggableFab.vue'
import ModemDialpad from '@/components/modem/ModemDialpad.vue'
import CommunicationSearchInput from '@/components/communication/CommunicationSearchInput.vue'
import ModemStickyTopBar from '@/components/modem/ModemStickyTopBar.vue'
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Spinner } from '@/components/ui/spinner'
import { FEATURE, useCapabilities } from '@/composables/useCapabilities'
import { useDialpadInput } from '@/composables/useDialpadInput'
import { useDeviceCallHistory } from '@/composables/useDeviceCallHistory'
import { useModemCallSession } from '@/composables/useModemCallSession'
import { useModemPhoneCountry } from '@/composables/useModemPhoneCountry'
import { useStickyTopBar } from '@/composables/useStickyTopBar'
import { dialStringChars, isCallableDialString, isDialServiceCode } from '@/lib/phoneNumberInput'
import type { CallRecord } from '@/types/call'
import type { UssdAction } from '@/types/ussd'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const modemApi = useModemApi()
const volteApi = useVoLTEApi()
const { hasFeature } = useCapabilities()
const volteAvailable = computed(() => hasFeature(FEATURE.volte))
const ussdApi = useUssdApi()
const backButtonRef = useTemplateRef<HTMLElement>('backButtonRef')
const dialpadRef = useTemplateRef<{ focus: () => void }>('dialpadRef')
const { isStickyVisible } = useStickyTopBar(backButtonRef)

const modemId = computed(() => (route.params.id ?? 'unknown') as string)
const { phoneCountry, profileID } = useModemPhoneCountry(modemId)
const searchQuery = ref('')
const normalizedSearchQuery = computed(() => searchQuery.value.trim())
const debouncedSearchQuery = refDebounced(normalizedSearchQuery, 250)

const {
  activeCall,
  isDialing,
  errorMessage: sessionError,
  callAudio,
  routeLabel,
  stateLabel,
  primaryLine,
  callDurationLabel,
  terminalStates,
  dial,
} = useModemCallSession(modemId, phoneCountry)
const {
  recentCalls,
  hasRecentCalls,
  isLoading,
  isDeletingCallID,
  deleteRecord,
  loadCalls,
  setSearchQuery,
  errorMessage: historyError,
} = useDeviceCallHistory(modemId)
const errorMessage = computed(() => historyError.value || sessionError.value)

const dialpadOpen = ref(false)
const expandedCallID = ref('')
const deleteDialogOpen = ref(false)
const deleteTarget = ref<CallRecord | null>(null)
const dialingCallBackID = ref('')
const replacement = ref<{ call: CallRecord; mode: 'message' | 'call' }>()
const ussdDialogOpen = ref(false)
const ussdDraft = ref('')
const ussdReply = ref('')
const ussdAction = ref<UssdAction>('initialize')
const isSendingUssd = ref(false)
const wifiCallingConnected = ref(false)
const volteEnabled = ref(false)

const {
  digits,
  normalizedDigits,
  keys,
  dialInputClass,
  setDigits,
  appendKey,
  backspace,
  startPlusLongPress,
  clearPlusLongPress,
} = useDialpadInput(phoneCountry)
const canDial = computed(() => isCallableDialString(normalizedDigits.value) && !isDialing.value)

const isUssd = (value: string) => isDialServiceCode(value)

const shouldPrepareOutgoingAudio = () => wifiCallingConnected.value || volteEnabled.value

const openUssdDialog = (code: string) => {
  ussdDraft.value = code
  ussdReply.value = ''
  ussdAction.value = 'initialize'
  ussdDialogOpen.value = true
}

const dialTarget = async (target: string, clearDigitsOnSuccess = false) => {
  if (!target) return
  const preparedAudio = shouldPrepareOutgoingAudio()
  const audioReady = preparedAudio ? callAudio.prepare() : Promise.resolve(false)
  dialpadOpen.value = false
  const call = await dial(target, 'auto', profileID.value)
  if (call) {
    if (clearDigitsOnSuccess) {
      digits.value = ''
    }
    await loadCalls()
  } else if (preparedAudio && (await audioReady)) {
    callAudio.stop()
  }
}

const startDial = async () => {
  const target = normalizedDigits.value
  if (!target) return
  if (isUssd(target)) {
    openUssdDialog(target)
    dialpadOpen.value = false
    await sendUssd()
    digits.value = ''
    return
  }
  await dialTarget(target, true)
}

const dialNumber = async (number: string) => {
  const target = dialStringChars(number)
  if (!target) return
  await dialTarget(target)
}

const historyAction = async (call: CallRecord, mode: 'message' | 'call') => {
  if (call.profileId && call.profileId !== profileID.value) {
    replacement.value = { call, mode }
    return
  }
  await performHistoryAction(call, mode)
}
const performHistoryAction = async (call: CallRecord, mode: 'message' | 'call') => {
  if (mode === 'message') {
    await router.push({
      name: 'modem-message-thread',
      params: { id: modemId.value, participant: call.number },
      query: { new: '1' },
    })
  } else await callBack(call)
}
const confirmReplacement = async () => {
  const selected = replacement.value
  replacement.value = undefined
  if (selected) await performHistoryAction(selected.call, selected.mode)
}
const callBack = async (call: CallRecord) => {
  if (!call.number || dialingCallBackID.value) return
  dialingCallBackID.value = call.callID
  try {
    await dialNumber(call.number)
  } finally {
    if (dialingCallBackID.value === call.callID) {
      dialingCallBackID.value = ''
    }
  }
}

const sendUssd = async () => {
  const targetId = modemId.value
  const code = ussdDraft.value.trim()
  if (!targetId || targetId === 'unknown' || !code || isSendingUssd.value) return
  isSendingUssd.value = true
  try {
    const { data } = await ussdApi.executeUssd(targetId, ussdAction.value, code)
    ussdReply.value = data.value?.reply ?? ''
    ussdDraft.value = ''
    ussdAction.value = 'reply'
  } catch {
    // The global API handler already surfaced the error; keep this dialog state intact.
  } finally {
    isSendingUssd.value = false
  }
}

const closeUssd = () => {
  ussdDialogOpen.value = false
  ussdDraft.value = ''
  ussdReply.value = ''
  ussdAction.value = 'initialize'
}

const loadImsRouteStatus = async () => {
  const targetId = modemId.value
  if (!targetId || targetId === 'unknown') {
    wifiCallingConnected.value = false
    volteEnabled.value = false
    return
  }
  const [wifiCallingResult, volte] = await Promise.all([
    Promise.allSettled([modemApi.getWiFiCallingSettings(targetId)]).then(([result]) => result),
    volteAvailable.value
      ? Promise.allSettled([volteApi.settings(targetId)]).then(([result]) => result)
      : Promise.resolve(null),
  ])
  if (wifiCallingResult.status === 'fulfilled') {
    wifiCallingConnected.value = wifiCallingResult.value.data.value?.connected ?? false
  } else {
    console.warn('[ModemCallsView] load Wi-Fi Calling status:', wifiCallingResult.reason)
    wifiCallingConnected.value = false
  }
  if (!volteAvailable.value) {
    volteEnabled.value = false
  } else if (volte?.status === 'fulfilled') {
    volteEnabled.value = volte.value.data.value?.enabled ?? false
  } else if (volte?.status === 'rejected') {
    console.warn('[ModemCallsView] load VoLTE status:', volte.reason)
    volteEnabled.value = false
  }
}

const pageErrorMessage = computed(
  () => errorMessage.value || (!activeCall.value ? callAudio.errorMessage.value : ''),
)
const isSearching = computed(() => debouncedSearchQuery.value.length > 0)
const emptyLabel = computed(() =>
  isSearching.value ? t('modemDetail.calls.noSearchResults') : t('modemDetail.calls.empty'),
)

const toggleCallDetails = (call: CallRecord) => {
  expandedCallID.value = expandedCallID.value === call.callID ? '' : call.callID
}

const openDeleteDialog = (call: CallRecord) => {
  deleteTarget.value = call
  deleteDialogOpen.value = true
}

const confirmDeleteRecord = async () => {
  if (!deleteTarget.value) return
  const callID = deleteTarget.value.callID
  const deleted = await deleteRecord(deleteTarget.value)
  if (!deleted) return
  if (expandedCallID.value === callID) {
    expandedCallID.value = ''
  }
  deleteDialogOpen.value = false
  deleteTarget.value = null
}

watch(
  modemId,
  () => {
    expandedCallID.value = ''
    deleteDialogOpen.value = false
    deleteTarget.value = null
    void loadImsRouteStatus()
  },
  { immediate: true },
)

watch(
  debouncedSearchQuery,
  (query) => {
    setSearchQuery(query)
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  setSearchQuery('')
})

watch(deleteDialogOpen, (open) => {
  if (open) return
  deleteTarget.value = null
})

watch(dialpadOpen, async (open) => {
  if (!open) return
  await nextTick()
  dialpadRef.value?.focus()
})
</script>

<template>
  <Dialog
    :open="!!replacement"
    @update:open="
      (open) => {
        if (!open) replacement = undefined
      }
    "
  >
    <DialogContent>
      <DialogHeader
        ><DialogTitle>{{ t('communication.useCurrentLine') }}</DialogTitle
        ><DialogDescription>{{ t('communication.currentModem') }}</DialogDescription></DialogHeader
      >
      <Button @click="confirmReplacement">{{ t('communication.useCurrentLine') }}</Button>
    </DialogContent>
  </Dialog>
  <div
    class="flex min-h-[calc(100dvh-6.5rem)] flex-col gap-4 lg:h-(--modem-desktop-content-height) lg:min-h-0 lg:overflow-hidden"
  >
    <header class="space-y-3">
      <ModemStickyTopBar
        :show="isStickyVisible"
        :title="t('modemDetail.calls.title')"
        :back-label="t('modemDetail.back')"
        back-to="/"
      />

      <div class="space-y-1">
        <div
          ref="backButtonRef"
          class="inline-flex lg:hidden"
          :class="{ invisible: isStickyVisible }"
        >
          <BackButton
            to="/"
            :label="t('modemDetail.back')"
          />
        </div>
        <h1 class="text-2xl font-semibold">{{ t('modemDetail.calls.title') }}</h1>
        <p class="text-sm text-muted-foreground">{{ t('modemDetail.calls.subtitle') }}</p>
      </div>
    </header>

    <p
      v-if="pageErrorMessage"
      class="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive"
    >
      {{ pageErrorMessage }}
    </p>

    <div class="grid flex-1 gap-6 lg:min-h-0 lg:grid-cols-[minmax(0,1fr)_18rem] lg:items-stretch">
      <div class="min-w-0 space-y-4 lg:flex lg:min-h-0 lg:flex-col lg:gap-4 lg:space-y-0">
        <CommunicationSearchInput
          v-model="searchQuery"
          :placeholder="t('modemDetail.calls.searchPlaceholder')"
          :clear-label="t('modemDetail.calls.clearSearch')"
        />

        <CallListSkeleton v-if="isLoading && !hasRecentCalls" />

        <div
          v-else-if="!hasRecentCalls"
          class="flex min-h-80 flex-col items-center justify-center rounded-lg border border-dashed px-6 py-12 text-center lg:min-h-0 lg:flex-1"
        >
          <Phone class="mb-3 size-10 text-muted-foreground" />
          <p class="font-medium">{{ emptyLabel }}</p>
        </div>

        <section
          v-else
          class="scrollbar-none space-y-3 lg:min-h-0 lg:flex-1 lg:overflow-y-auto"
        >
          <CallHistoryRow
            v-for="call in recentCalls"
            :key="call.callID"
            :call="call"
            :title="primaryLine(call)"
            :source="call.lineLabel"
            :state="stateLabel(call.state)"
            :route="routeLabel(call.route)"
            :duration="callDurationLabel(call)"
            :expanded="expandedCallID === call.callID"
            :calling="dialingCallBackID === call.callID"
            :callback-disabled="!call.number || isDialing || !!dialingCallBackID"
            @select="toggleCallDetails(call)"
            @callback="historyAction(call, 'call')"
          >
            <div
              v-if="expandedCallID === call.callID"
              class="mt-4 border-t pt-4"
              @click.stop
            >
              <CallHistoryFields
                :call="call"
                :state="stateLabel(call.state)"
                :route="routeLabel(call.route)"
                :duration="callDurationLabel(call)"
              />

              <div class="mt-4 flex flex-col gap-2 lg:flex-row lg:justify-end">
                <ContactQuickActions
                  class="flex-col lg:flex-row lg:flex-nowrap"
                  :numbers="call.number ? [{ number: call.number, label: '' }] : []"
                  :call-disabled="isDialing || !terminalStates.has(call.state)"
                  @action="(_, mode) => historyAction(call, mode)"
                />
                <CommunicationActionButton
                  v-if="terminalStates.has(call.state)"
                  destructive
                  :disabled="isDeletingCallID === call.callID"
                  @click="openDeleteDialog(call)"
                >
                  <Spinner
                    v-if="isDeletingCallID === call.callID"
                    class="size-4"
                  />
                  <Trash2
                    v-else
                    class="size-4"
                  />
                  {{ t('modemDetail.calls.deleteRecord') }}
                </CommunicationActionButton>
              </div>
            </div>
          </CallHistoryRow>
        </section>
      </div>

      <aside class="scrollbar-none hidden min-h-0 lg:block lg:h-full lg:overflow-y-auto">
        <div class="rounded-xl border bg-card/60 p-4 shadow-sm">
          <ModemDialpad
            :digits="digits"
            :keys="keys"
            :input-class="dialInputClass"
            :can-dial="canDial"
            :is-dialing="isDialing"
            density="compact"
            show-header
            @update:digits="setDigits"
            @backspace="backspace"
            @append-key="appendKey"
            @start-plus="startPlusLongPress"
            @clear-plus="clearPlusLongPress"
            @dial="startDial"
          />
        </div>
      </aside>
    </div>

    <DraggableFab
      class="lg:hidden"
      :ariaLabel="t('modemDetail.calls.openDialpad')"
      @click="dialpadOpen = true"
    >
      <Phone class="size-6" />
    </DraggableFab>

    <Dialog v-model:open="dialpadOpen">
      <DialogContent
        class="min-h-[min(82dvh,42rem)] w-[min(calc(100%-3rem),20rem)] max-w-none grid-rows-[auto_1fr] rounded-2xl p-5 sm:max-w-none"
      >
        <DialogHeader>
          <DialogTitle>{{ t('modemDetail.calls.dialpad') }}</DialogTitle>
          <DialogDescription>
            {{ t('modemDetail.calls.dialpadDescription') }}
          </DialogDescription>
        </DialogHeader>

        <ModemDialpad
          ref="dialpadRef"
          :digits="digits"
          :keys="keys"
          :input-class="dialInputClass"
          :can-dial="canDial"
          :is-dialing="isDialing"
          @update:digits="setDigits"
          @backspace="backspace"
          @append-key="appendKey"
          @start-plus="startPlusLongPress"
          @clear-plus="clearPlusLongPress"
          @dial="startDial"
        />
      </DialogContent>
    </Dialog>

    <Dialog v-model:open="ussdDialogOpen">
      <DialogContent class="max-w-sm rounded-2xl">
        <DialogHeader>
          <DialogTitle>{{ t('modemDetail.calls.ussdTitle') }}</DialogTitle>
          <DialogDescription>
            {{ t('modemDetail.calls.ussdDescription') }}
          </DialogDescription>
        </DialogHeader>
        <div class="space-y-4">
          <div
            v-if="ussdReply"
            class="rounded-lg bg-muted px-4 py-3 text-sm whitespace-pre-wrap"
          >
            {{ ussdReply }}
          </div>
          <input
            v-model="ussdDraft"
            class="h-11 w-full rounded-md border bg-background px-3 text-base outline-none focus-visible:ring-2 focus-visible:ring-ring"
            :placeholder="t('modemDetail.calls.ussdPlaceholder')"
            @keyup.enter="sendUssd"
          />
          <div class="grid grid-cols-2 gap-2">
            <Button
              variant="outline"
              @click="closeUssd"
              >{{ t('modemDetail.actions.cancel') }}</Button
            >
            <Button
              :disabled="isSendingUssd || !ussdDraft.trim()"
              @click="sendUssd"
            >
              <Spinner
                v-if="isSendingUssd"
                class="mr-2 size-4"
              />
              {{ t('modemDetail.ussd.send') }}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>

    <AlertDialog v-model:open="deleteDialogOpen">
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{{ t('modemDetail.calls.deleteTitle') }}</AlertDialogTitle>
          <AlertDialogDescription>
            {{ t('modemDetail.calls.deleteDescription') }}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel :disabled="!!isDeletingCallID">
            {{ t('modemDetail.actions.cancel') }}
          </AlertDialogCancel>
          <Button
            variant="destructive"
            type="button"
            :disabled="!!isDeletingCallID"
            @click="confirmDeleteRecord"
          >
            <span
              v-if="isDeletingCallID"
              class="inline-flex items-center gap-2"
            >
              <Spinner class="size-4" />
              {{ t('modemDetail.actions.delete') }}
            </span>
            <span v-else>{{ t('modemDetail.actions.delete') }}</span>
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </div>
</template>
