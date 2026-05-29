<script setup lang="ts">
import { refDebounced } from '@vueuse/core'
import { Delete, Phone, PhoneCall, PhoneIncoming, PhoneOutgoing, Trash2 } from 'lucide-vue-next'
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'

import { useModemApi } from '@/apis/modem'
import { useUssdApi } from '@/apis/ussd'
import BackButton from '@/components/BackButton.vue'
import DraggableFab from '@/components/fab/DraggableFab.vue'
import ModemSearchInput from '@/components/modem/ModemSearchInput.vue'
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
import { useModemCallSession } from '@/composables/useModemCallSession'
import { useModemPhoneCountry } from '@/composables/useModemPhoneCountry'
import { useStickyTopBar } from '@/composables/useStickyTopBar'
import { hasBrowserAmrCodec } from '@/lib/browserAmrCodec'
import { formatListTimestamp } from '@/lib/datetime'
import { dialStringChars, formatDialInput, isDialServiceCode } from '@/lib/phoneNumberInput'
import type { CallRecord } from '@/types/call'
import type { UssdAction } from '@/types/ussd'

const route = useRoute()
const { t } = useI18n()
const modemApi = useModemApi()
const ussdApi = useUssdApi()
const backButtonRef = ref<HTMLElement | null>(null)
const dialInputRef = ref<HTMLInputElement | null>(null)
const { isStickyVisible } = useStickyTopBar(backButtonRef)

const modemId = computed(() => (route.params.id ?? 'unknown') as string)
const { phoneCountry } = useModemPhoneCountry(modemId)
const searchQuery = ref('')
const normalizedSearchQuery = computed(() => searchQuery.value.trim())
const debouncedSearchQuery = refDebounced(normalizedSearchQuery, 250)

const {
  recentCalls,
  hasRecentCalls,
  activeCall,
  isLoading,
  isDialing,
  errorMessage,
  callAudio,
  routeLabel,
  stateLabel,
  primaryLine,
  callDurationLabel,
  terminalStates,
  isDeletingCallID,
  dial,
  deleteRecord,
  loadCalls,
  setSearchQuery,
} = useModemCallSession(modemId, phoneCountry)

const dialpadOpen = ref(false)
const expandedCallID = ref('')
const deleteDialogOpen = ref(false)
const deleteTarget = ref<CallRecord | null>(null)
const digits = ref('')
const plusLongPressTimer = ref<number | null>(null)
const suppressNextZeroClick = ref(false)
const ussdDialogOpen = ref(false)
const ussdDraft = ref('')
const ussdReply = ref('')
const ussdAction = ref<UssdAction>('initialize')
const isSendingUssd = ref(false)
const wifiCallingConnected = ref(false)

const keys = [
  { value: '1', letters: '' },
  { value: '2', letters: 'ABC' },
  { value: '3', letters: 'DEF' },
  { value: '4', letters: 'GHI' },
  { value: '5', letters: 'JKL' },
  { value: '6', letters: 'MNO' },
  { value: '7', letters: 'PQRS' },
  { value: '8', letters: 'TUV' },
  { value: '9', letters: 'WXYZ' },
  { value: '*', letters: '' },
  { value: '0', letters: '+' },
  { value: '#', letters: '' },
]

const normalizedDigits = computed(() => dialStringChars(digits.value))
const canDial = computed(() => normalizedDigits.value.length > 0 && !isDialing.value)
const dialInputClass = computed(() => {
  const length = normalizedDigits.value.length
  if (length > 20) return 'text-lg'
  if (length > 15) return 'text-xl'
  if (length > 10) return 'text-2xl'
  return 'text-3xl'
})

const isUssd = (value: string) => isDialServiceCode(value)

const shouldPrepareOutgoingAudio = () => hasBrowserAmrCodec() && wifiCallingConnected.value

const setDigits = (value: string) => {
  digits.value = formatDialInput(value, phoneCountry.value)
}

const appendDigit = (value: string) => {
  setDigits(`${digits.value}${value}`)
}

const appendPlus = () => {
  if (dialStringChars(digits.value)) return
  setDigits('+')
}

const clearPlusLongPress = () => {
  if (plusLongPressTimer.value === null) return
  window.clearTimeout(plusLongPressTimer.value)
  plusLongPressTimer.value = null
}

const startPlusLongPress = (value: string) => {
  if (value !== '0') return
  clearPlusLongPress()
  suppressNextZeroClick.value = false
  plusLongPressTimer.value = window.setTimeout(() => {
    appendPlus()
    suppressNextZeroClick.value = true
    plusLongPressTimer.value = null
  }, 450)
}

const appendKey = (value: string) => {
  if (value === '0' && suppressNextZeroClick.value) {
    suppressNextZeroClick.value = false
    return
  }
  appendDigit(value)
}

const backspace = () => {
  setDigits(dialStringChars(digits.value).slice(0, -1))
}

const updateDialInput = (event: Event) => {
  const target = event.target as HTMLInputElement | null
  setDigits(target?.value ?? '')
}

const openUssdDialog = (code: string) => {
  ussdDraft.value = code
  ussdReply.value = ''
  ussdAction.value = 'initialize'
  ussdDialogOpen.value = true
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
  const preparedAudio = shouldPrepareOutgoingAudio()
  if (preparedAudio) {
    const ready = await callAudio.prepare()
    if (!ready) return
  }
  dialpadOpen.value = false
  const call = await dial(target)
  if (call) {
    digits.value = ''
    await loadCalls()
  } else if (preparedAudio) {
    callAudio.stop()
  }
}

const dialNumber = async (number: string) => {
  setDigits(number)
  await startDial()
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

const loadWiFiCallingStatus = async () => {
  const targetId = modemId.value
  if (!targetId || targetId === 'unknown') {
    wifiCallingConnected.value = false
    return
  }
  try {
    const { data } = await modemApi.getWiFiCallingSettings(targetId)
    wifiCallingConnected.value = data.value?.connected ?? false
  } catch (err) {
    console.warn('[ModemPhoneView] load Wi-Fi Calling status:', err)
    wifiCallingConnected.value = false
  }
}

const pageErrorMessage = computed(
  () => errorMessage.value || (!activeCall.value ? callAudio.errorMessage.value : ''),
)
const isSearching = computed(() => debouncedSearchQuery.value.length > 0)
const emptyLabel = computed(() =>
  isSearching.value ? t('modemDetail.phone.noSearchResults') : t('modemDetail.phone.empty'),
)

const formatDetailTimestamp = (value: string) => {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'medium',
  }).format(date)
}

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
    void loadWiFiCallingStatus()
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

watch(phoneCountry, () => {
  setDigits(digits.value)
})

watch(deleteDialogOpen, (open) => {
  if (open) return
  deleteTarget.value = null
})

watch(dialpadOpen, async (open) => {
  if (!open) return
  await nextTick()
  dialInputRef.value?.focus()
})
</script>

<template>
  <div class="flex min-h-[calc(100dvh-6.5rem)] flex-col gap-4">
    <header class="space-y-3">
      <ModemStickyTopBar
        :show="isStickyVisible"
        :title="t('modemDetail.phone.title')"
        :back-label="t('modemDetail.back')"
        back-to="/"
      />

      <div class="space-y-1">
        <div ref="backButtonRef" class="inline-flex" :class="{ invisible: isStickyVisible }">
          <BackButton to="/" :label="t('modemDetail.back')" />
        </div>
        <h1 class="text-2xl font-semibold">{{ t('modemDetail.phone.title') }}</h1>
        <p class="text-sm text-muted-foreground">{{ t('modemDetail.phone.subtitle') }}</p>
      </div>
    </header>

    <p
      v-if="pageErrorMessage"
      class="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive"
    >
      {{ pageErrorMessage }}
    </p>

    <ModemSearchInput
      v-model="searchQuery"
      :placeholder="t('modemDetail.phone.searchPlaceholder')"
      :clear-label="t('modemDetail.phone.clearSearch')"
    />

    <div v-if="isLoading" class="flex flex-1 items-center justify-center">
      <Spinner class="size-6" />
    </div>

    <div
      v-else-if="!hasRecentCalls"
      class="flex flex-1 flex-col items-center justify-center rounded-lg border border-dashed px-6 py-12 text-center"
    >
      <Phone class="mb-3 size-10 text-muted-foreground" />
      <p class="font-medium">{{ emptyLabel }}</p>
    </div>

    <section v-else class="space-y-3">
      <div
        v-for="call in recentCalls"
        :key="call.callID"
        class="group rounded-lg bg-card px-4 py-3 shadow-sm transition hover:shadow-md"
        role="button"
        tabindex="0"
        :aria-expanded="expandedCallID === call.callID"
        @click="toggleCallDetails(call)"
        @keydown.enter.prevent="toggleCallDetails(call)"
        @keydown.space.prevent="toggleCallDetails(call)"
      >
        <div class="flex items-center gap-3">
          <span
            class="flex size-11 shrink-0 items-center justify-center rounded-full text-base font-semibold shadow-sm ring-1"
            :class="
              call.direction === 'incoming'
                ? 'bg-emerald-100 text-emerald-700 ring-emerald-200/70 dark:bg-emerald-500/15 dark:text-emerald-200 dark:ring-emerald-400/20'
                : 'bg-sky-100 text-sky-700 ring-sky-200/70 dark:bg-sky-500/15 dark:text-sky-200 dark:ring-sky-400/20'
            "
            aria-hidden="true"
          >
            <PhoneIncoming v-if="call.direction === 'incoming'" class="size-5" />
            <PhoneOutgoing v-else class="size-5" />
          </span>

          <span class="min-w-0 flex-1 space-y-1">
            <span class="flex min-w-0 items-center justify-between gap-3">
              <span class="truncate text-sm font-semibold text-foreground">
                {{ primaryLine(call) }}
              </span>
              <span class="shrink-0 text-xs font-medium text-muted-foreground">
                {{ formatListTimestamp(call.updatedAt) }}
              </span>
            </span>
            <span class="flex min-w-0 items-center justify-between gap-3">
              <span class="block truncate text-xs text-muted-foreground">
                {{ stateLabel(call.state) }} · {{ routeLabel(call.route) }}
              </span>
              <span v-if="callDurationLabel(call)" class="shrink-0 text-xs text-muted-foreground">{{
                callDurationLabel(call)
              }}</span>
            </span>
          </span>

          <Button
            size="icon"
            variant="ghost"
            class="size-8 shrink-0 rounded-full opacity-100 transition"
            :disabled="!call.number"
            :aria-label="t('modemDetail.phone.callBack')"
            @click.stop="dialNumber(call.number)"
          >
            <PhoneCall class="size-4" />
          </Button>
        </div>

        <div v-if="expandedCallID === call.callID" class="mt-4 border-t pt-4" @click.stop>
          <dl class="grid grid-cols-2 gap-3 text-sm">
            <div class="space-y-1">
              <dt class="text-xs font-medium text-muted-foreground">
                {{ t('modemDetail.phone.details.direction') }}
              </dt>
              <dd>{{ t(`modemDetail.phone.${call.direction}`) }}</dd>
            </div>
            <div class="space-y-1">
              <dt class="text-xs font-medium text-muted-foreground">
                {{ t('modemDetail.phone.details.state') }}
              </dt>
              <dd>{{ stateLabel(call.state) }}</dd>
            </div>
            <div class="space-y-1">
              <dt class="text-xs font-medium text-muted-foreground">
                {{ t('modemDetail.phone.details.route') }}
              </dt>
              <dd>{{ routeLabel(call.route) }}</dd>
            </div>
            <div class="space-y-1">
              <dt class="text-xs font-medium text-muted-foreground">
                {{ t('modemDetail.phone.details.duration') }}
              </dt>
              <dd>{{ callDurationLabel(call) || t('modemDetail.phone.durationEmpty') }}</dd>
            </div>
            <div class="space-y-1">
              <dt class="text-xs font-medium text-muted-foreground">
                {{ t('modemDetail.phone.details.startedAt') }}
              </dt>
              <dd>{{ formatDetailTimestamp(call.startedAt) }}</dd>
            </div>
            <div class="space-y-1">
              <dt class="text-xs font-medium text-muted-foreground">
                {{ t('modemDetail.phone.details.answeredAt') }}
              </dt>
              <dd>
                {{
                  call.answeredAt
                    ? formatDetailTimestamp(call.answeredAt)
                    : t('modemDetail.phone.details.notAnswered')
                }}
              </dd>
            </div>
            <div class="space-y-1">
              <dt class="text-xs font-medium text-muted-foreground">
                {{ t('modemDetail.phone.details.endedAt') }}
              </dt>
              <dd>
                {{
                  call.endedAt
                    ? formatDetailTimestamp(call.endedAt)
                    : t('modemDetail.phone.details.inProgress')
                }}
              </dd>
            </div>
            <div v-if="call.reason" class="space-y-1">
              <dt class="text-xs font-medium text-muted-foreground">
                {{ t('modemDetail.phone.details.reason') }}
              </dt>
              <dd>{{ call.reason }}</dd>
            </div>
          </dl>

          <div v-if="terminalStates.has(call.state)" class="mt-4 flex justify-end">
            <Button
              variant="destructive"
              size="sm"
              class="w-full gap-2 sm:w-auto"
              :disabled="isDeletingCallID === call.callID"
              @click="openDeleteDialog(call)"
            >
              <Spinner v-if="isDeletingCallID === call.callID" class="size-4" />
              <Trash2 v-else class="size-4" />
              {{ t('modemDetail.phone.deleteRecord') }}
            </Button>
          </div>
        </div>
      </div>
    </section>

    <DraggableFab :ariaLabel="t('modemDetail.phone.openDialpad')" @click="dialpadOpen = true">
      <Phone class="size-6" />
    </DraggableFab>

    <Dialog v-model:open="dialpadOpen">
      <DialogContent
        class="min-h-[min(82dvh,42rem)] w-[min(calc(100%-3rem),20rem)] max-w-none grid-rows-[auto_1fr] rounded-2xl p-5 sm:max-w-none"
      >
        <DialogHeader>
          <DialogTitle>{{ t('modemDetail.phone.dialpad') }}</DialogTitle>
          <DialogDescription>
            {{ t('modemDetail.phone.dialpadDescription') }}
          </DialogDescription>
        </DialogHeader>

        <div class="flex min-h-0 flex-col justify-between gap-6">
          <div class="relative flex min-h-24 items-center">
            <input
              ref="dialInputRef"
              :value="digits"
              type="tel"
              inputmode="tel"
              autocomplete="tel"
              class="h-20 w-full bg-transparent text-center font-semibold tracking-normal outline-none"
              :class="dialInputClass"
              :aria-label="t('modemDetail.phone.numberPlaceholder')"
              @input="updateDialInput"
              @keydown.enter.prevent="startDial"
            />
            <Button
              v-if="digits"
              size="icon"
              variant="ghost"
              class="absolute top-1/2 right-0 -translate-y-1/2 touch-manipulation"
              :aria-label="t('modemDetail.phone.backspace')"
              @click="backspace"
            >
              <Delete class="size-5" />
            </Button>
          </div>

          <div class="mx-auto grid w-full max-w-60 grid-cols-3 gap-4">
            <button
              v-for="key in keys"
              :key="key.value"
              type="button"
              class="flex aspect-square min-h-0 touch-manipulation select-none flex-col items-center justify-center rounded-full bg-muted text-lg font-semibold transition hover:bg-muted/70 active:scale-95"
              @click="appendKey(key.value)"
              @pointerdown="startPlusLongPress(key.value)"
              @pointerup="clearPlusLongPress"
              @pointercancel="clearPlusLongPress"
              @pointerleave="clearPlusLongPress"
            >
              <span>{{ key.value }}</span>
              <span class="h-4 text-[0.65rem] font-medium text-muted-foreground">{{
                key.letters
              }}</span>
            </button>
          </div>

          <div class="mx-auto grid w-full max-w-60 grid-cols-3 gap-4">
            <button
              type="button"
              class="col-start-2 flex aspect-square min-h-0 touch-manipulation items-center justify-center rounded-full bg-emerald-600 text-white transition hover:bg-emerald-700 active:scale-95 disabled:pointer-events-none disabled:opacity-50"
              :disabled="!canDial"
              :aria-label="t('modemDetail.phone.call')"
              @click="startDial"
            >
              <PhoneCall v-if="!isDialing" class="size-5" />
              <Spinner v-else class="size-6" />
            </button>
          </div>
        </div>
      </DialogContent>
    </Dialog>

    <Dialog v-model:open="ussdDialogOpen">
      <DialogContent class="max-w-sm rounded-2xl">
        <DialogHeader>
          <DialogTitle>{{ t('modemDetail.phone.ussdTitle') }}</DialogTitle>
          <DialogDescription>
            {{ t('modemDetail.phone.ussdDescription') }}
          </DialogDescription>
        </DialogHeader>
        <div class="space-y-4">
          <div v-if="ussdReply" class="rounded-lg bg-muted px-4 py-3 text-sm whitespace-pre-wrap">
            {{ ussdReply }}
          </div>
          <input
            v-model="ussdDraft"
            class="h-11 w-full rounded-md border bg-background px-3 text-base outline-none focus-visible:ring-2 focus-visible:ring-ring"
            :placeholder="t('modemDetail.phone.ussdPlaceholder')"
            @keyup.enter="sendUssd"
          />
          <div class="grid grid-cols-2 gap-2">
            <Button variant="outline" @click="closeUssd">{{
              t('modemDetail.actions.cancel')
            }}</Button>
            <Button :disabled="isSendingUssd || !ussdDraft.trim()" @click="sendUssd">
              <Spinner v-if="isSendingUssd" class="mr-2 size-4" />
              {{ t('modemDetail.ussd.send') }}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>

    <AlertDialog v-model:open="deleteDialogOpen">
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{{ t('modemDetail.phone.deleteTitle') }}</AlertDialogTitle>
          <AlertDialogDescription>
            {{ t('modemDetail.phone.deleteDescription') }}
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
            <span v-if="isDeletingCallID" class="inline-flex items-center gap-2">
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
