<script setup lang="ts">
import { Delete, Phone, PhoneCall, PhoneIncoming, PhoneOutgoing } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'

import { useModemApi } from '@/apis/modem'
import { useUssdApi } from '@/apis/ussd'
import BackButton from '@/components/BackButton.vue'
import DraggableFab from '@/components/fab/DraggableFab.vue'
import ModemStickyTopBar from '@/components/modem/ModemStickyTopBar.vue'
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
import { useStickyTopBar } from '@/composables/useStickyTopBar'
import { hasBrowserAmrCodec } from '@/lib/browserAmrCodec'
import type { CallRecord } from '@/types/call'
import type { UssdAction } from '@/types/ussd'

const route = useRoute()
const { t } = useI18n()
const modemApi = useModemApi()
const ussdApi = useUssdApi()
const backButtonRef = ref<HTMLElement | null>(null)
const { isStickyVisible } = useStickyTopBar(backButtonRef)

const modemId = computed(() => (route.params.id ?? 'unknown') as string)

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
  dial,
  loadCalls,
} = useModemCallSession(modemId)

const dialpadOpen = ref(false)
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

const normalizedDigits = computed(() => digits.value.trim())
const canDial = computed(() => normalizedDigits.value.length > 0 && !isDialing.value)

const isUssd = (value: string) => value.startsWith('*') || value.startsWith('#')

const shouldPrepareOutgoingAudio = () => hasBrowserAmrCodec() && wifiCallingConnected.value

const timestampLabel = (value: string) => {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleString()
}

const directionLabel = (call: CallRecord) =>
  call.direction === 'incoming' ? t('modemDetail.phone.incoming') : t('modemDetail.phone.outgoing')

const appendDigit = (value: string) => {
  digits.value += value
}

const appendPlus = () => {
  if (digits.value.includes('+')) return
  digits.value = digits.value.length === 0 ? '+' : `${digits.value}+`
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
  digits.value = digits.value.slice(0, -1)
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
  digits.value = number
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

watch(
  modemId,
  () => {
    void loadWiFiCallingStatus()
  },
  { immediate: true },
)
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

    <div v-if="isLoading" class="flex flex-1 items-center justify-center">
      <Spinner class="size-6" />
    </div>

    <div
      v-else-if="!hasRecentCalls"
      class="flex flex-1 flex-col items-center justify-center rounded-lg border border-dashed px-6 py-12 text-center"
    >
      <Phone class="mb-3 size-10 text-muted-foreground" />
      <p class="font-medium">{{ t('modemDetail.phone.empty') }}</p>
    </div>

    <section v-else class="space-y-3">
      <div
        v-for="call in recentCalls"
        :key="call.callID"
        class="group rounded-lg bg-card px-4 py-3 shadow-sm transition hover:shadow-md"
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
                {{ timestampLabel(call.updatedAt) }}
              </span>
            </span>
            <span class="flex min-w-0 items-center justify-between gap-3">
              <span class="block truncate text-xs text-muted-foreground">
                {{ directionLabel(call) }} · {{ stateLabel(call.state) }} ·
                {{ routeLabel(call.route) }}
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
            @click="dialNumber(call.number)"
          >
            <PhoneCall class="size-4" />
          </Button>
        </div>
      </div>
    </section>

    <DraggableFab :ariaLabel="t('modemDetail.phone.openDialpad')" @click="dialpadOpen = true">
      <Phone class="size-6" />
    </DraggableFab>

    <Dialog v-model:open="dialpadOpen">
      <DialogContent class="rounded-2xl sm:max-w-xs">
        <DialogHeader>
          <DialogTitle>{{ t('modemDetail.phone.dialpad') }}</DialogTitle>
          <DialogDescription>
            {{ t('modemDetail.phone.dialpadDescription') }}
          </DialogDescription>
        </DialogHeader>

        <div class="space-y-5">
          <div
            class="flex min-h-12 items-center justify-center gap-2 text-center text-3xl font-semibold tracking-normal"
          >
            <span class="break-all">{{ digits || t('modemDetail.phone.numberPlaceholder') }}</span>
            <Button
              v-if="digits"
              size="icon"
              variant="ghost"
              :aria-label="t('modemDetail.phone.backspace')"
              @click="backspace"
            >
              <Delete class="size-5" />
            </Button>
          </div>

          <div class="mx-auto grid max-w-56 grid-cols-3 gap-2">
            <button
              v-for="key in keys"
              :key="key.value"
              type="button"
              class="flex aspect-square min-h-0 flex-col items-center justify-center rounded-full bg-muted text-xl font-semibold transition hover:bg-muted/70 active:scale-95"
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

          <div class="flex items-center justify-center">
            <Button
              size="icon-lg"
              class="size-16 rounded-full bg-emerald-600 hover:bg-emerald-700"
              :disabled="!canDial"
              :aria-label="t('modemDetail.phone.call')"
              @click="startDial"
            >
              <PhoneCall v-if="!isDialing" class="size-8" />
              <Spinner v-else class="size-6" />
            </Button>
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
  </div>
</template>
